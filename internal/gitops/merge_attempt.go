package gitops

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultVerifyCommand is the verification command MergeAttempt runs after a
// clean merge when TryOpts.VerifyCommand is empty. It is Go-centric because the
// project is Go; a repo overrides it via .quasar.yaml's merge_gate block.
const DefaultVerifyCommand = "go build ./... && go test -short ./..."

// MergeResult classifies the outcome of a merge attempt.
type MergeResult string

const (
	// MergeClean means the merge produced no conflict markers and the verify
	// command passed.
	MergeClean MergeResult = "clean"
	// MergeMarkers means git merge left one or more files with conflict markers.
	MergeMarkers MergeResult = "markers"
	// MergeBuildFailure means the merge was marker-free but the verify command
	// exited non-zero — the two branches collided semantically.
	MergeBuildFailure MergeResult = "build_failure"
	// MergeError means git merge itself failed (e.g. a missing ref or corrupt
	// object), distinct from an ordinary conflict.
	MergeError MergeResult = "merge_error"
)

// MergeOutcome is the classified result of a merge attempt. Only the fields
// relevant to Result are populated: MergedSHA for clean, ConflictedFiles for
// markers, BuildOutput for build_failure (and the git error text for
// merge_error). Worktree is the merge worktree path, meaningful to a downstream
// conflict resolver only when the attempt was made with KeepWorktree=true.
type MergeOutcome struct {
	Result          MergeResult
	Worktree        string
	ConflictedFiles []string
	BuildOutput     string
	MergedSHA       string
}

// MergeAttempt tries to merge one branch into another in a throwaway worktree
// and classifies the outcome. The repo's main working tree is never modified:
// the attempt happens in a sibling worktree under the repo's git common dir
// (<git-common-dir>/quasar-merge/merge-<name>/), removed at return unless
// TryOpts.KeepWorktree is set.
type MergeAttempt struct {
	// Client is the gitops Client bound to the repo whose branches are merged.
	Client *Client
}

// TryOpts configures MergeAttempt.Try.
type TryOpts struct {
	// SrcBranch is the branch merged in (e.g. "quasar/cycle-3-of-run-xyz").
	SrcBranch string
	// DstBranch is the branch merged into (e.g. "main" or "quasar/integration").
	DstBranch string
	// VerifyCommand runs in the merge worktree after a clean merge. Empty uses
	// DefaultVerifyCommand.
	VerifyCommand string
	// Timeout caps the verify command so a runaway build cannot block the gate
	// forever. Non-positive means no cap (the parent ctx still applies). A
	// timed-out verify is reported as a build_failure, not a Go error.
	Timeout time.Duration
	// KeepWorktree leaves the merge worktree in place on return so a conflict
	// resolver can inherit the merge-state worktree. Default false removes it.
	KeepWorktree bool
	// RunID names the deterministic worktree directory (merge-<RunID>) so the
	// resolver can locate it. Empty derives a name from SrcBranch.
	RunID string
}

// Try merges SrcBranch into DstBranch in a temporary worktree, runs the verify
// command on a clean merge, and returns the classified outcome. A git-level
// failure (missing ref, corrupt object) is reported as a MergeError outcome
// with a nil error so the caller can route on Result; a real (non-nil) error is
// reserved for setup faults the caller cannot route on (invalid arguments, an
// undiscoverable git dir).
func (m *MergeAttempt) Try(ctx context.Context, opts TryOpts) (MergeOutcome, error) {
	if m == nil || m.Client == nil {
		return MergeOutcome{}, fmt.Errorf("gitops: MergeAttempt has no Client")
	}
	if strings.TrimSpace(opts.SrcBranch) == "" || strings.TrimSpace(opts.DstBranch) == "" {
		return MergeOutcome{}, fmt.Errorf("gitops: merge attempt requires src and dst branches")
	}

	worktree, err := m.worktreePath(ctx, opts)
	if err != nil {
		return MergeOutcome{}, err
	}
	outcome := MergeOutcome{Worktree: worktree}

	// A stale worktree from a prior crashed attempt at the same path would make
	// `worktree add` fail; clear it first so the attempt is idempotent.
	m.cleanup(ctx, worktree)

	if _, stderr, addErr := m.Client.runGit(ctx, "worktree", "add", "--detach", worktree, opts.DstBranch); addErr != nil {
		// Could not even check out the destination — treat as a git-level error
		// the constellation routes to _failed, not a setup fault.
		outcome.Result = MergeError
		outcome.BuildOutput = strings.TrimSpace(string(stderr))
		return outcome, nil
	}
	if !opts.KeepWorktree {
		defer m.cleanup(ctx, worktree)
	}

	wtGit := defaultRunGit(worktree)

	if _, stderr, mergeErr := wtGit(ctx, "merge", "--no-edit", opts.SrcBranch); mergeErr != nil {
		// A failed merge is either a conflict (unmerged paths present) or a
		// git-level error (missing ref, corrupt object) with no unmerged paths.
		conflicted := conflictedFiles(ctx, wtGit)
		if len(conflicted) > 0 {
			outcome.Result = MergeMarkers
			outcome.ConflictedFiles = conflicted
			return outcome, nil
		}
		outcome.Result = MergeError
		outcome.BuildOutput = strings.TrimSpace(string(stderr))
		return outcome, nil
	}

	// Clean merge: capture the merge SHA, then run verify against the merged tree.
	// CAVEAT: this commit is reachable ONLY from the throwaway worktree's detached
	// HEAD. When KeepWorktree is false (or the caller cleans up afterward) the SHA
	// is unanchored and GC-eligible. A consumer that needs merged_sha to outlive
	// the worktree (e.g. the supervisor committing it to the parent branch) must
	// anchor it first — keep the worktree until the parent commit lands, or
	// `git update-ref refs/quasar/merge-<run-id> <sha>`.
	stdout, _, headErr := wtGit(ctx, "rev-parse", "HEAD")
	if headErr != nil {
		outcome.Result = MergeError
		return outcome, nil
	}
	mergedSHA := strings.TrimSpace(string(stdout))

	verify := opts.VerifyCommand
	if strings.TrimSpace(verify) == "" {
		verify = DefaultVerifyCommand
	}
	if out, ok := runVerify(ctx, worktree, verify, opts.Timeout); !ok {
		outcome.Result = MergeBuildFailure
		outcome.BuildOutput = out
		outcome.MergedSHA = mergedSHA
		return outcome, nil
	}

	outcome.Result = MergeClean
	outcome.MergedSHA = mergedSHA
	return outcome, nil
}

// worktreePath returns the deterministic merge worktree path for opts. It lives
// under the repo's git common dir so it is invisible to the main working tree
// and shared across linked worktrees.
func (m *MergeAttempt) worktreePath(ctx context.Context, opts TryOpts) (string, error) {
	stdout, stderr, err := m.Client.runGit(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("gitops: locate git dir: %w: %s", err, strings.TrimSpace(string(stderr)))
	}
	gitDir := strings.TrimSpace(string(stdout))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(m.Client.workDir, gitDir)
	}
	// The path is deterministic per run-id by design. The SrcBranch fallback keeps
	// a stand-alone Try (no RunID) working, but it is NOT collision-safe: two
	// concurrent attempts merging the same source branch would target the same
	// directory and clobber each other. Safe today because gate execution is
	// single-flight; when the firing supervisor introduces concurrency it must
	// thread a unique RunID (the operator already plumbs run_id end-to-end — only
	// the merge-gate.toml `attempt` input binding is missing).
	name := opts.RunID
	if strings.TrimSpace(name) == "" {
		name = opts.SrcBranch
	}
	return filepath.Join(gitDir, "quasar-merge", "merge-"+sanitizeName(name)), nil
}

// Cleanup removes a merge worktree left in place by a KeepWorktree attempt.
// The merge gate calls it once it knows no conflict resolver will consume the
// worktree (a clean or merge_error outcome); the resolver itself calls it after
// consuming a kept worktree.
func (m *MergeAttempt) Cleanup(ctx context.Context, worktree string) {
	if m == nil || m.Client == nil {
		return
	}
	m.cleanup(ctx, worktree)
}

// cleanup removes the merge worktree directory and prunes git's administrative
// record of it. Best-effort: it mirrors the worktree reaper (remove the dir,
// then `git worktree prune`) so it never needs the forbidden `--force` flag.
func (m *MergeAttempt) cleanup(ctx context.Context, worktree string) {
	if worktree == "" {
		return
	}
	if err := os.RemoveAll(worktree); err != nil {
		fmt.Fprintf(os.Stderr, "gitops: remove merge worktree %s: %v\n", worktree, err)
	}
	if _, stderr, err := m.Client.runGit(ctx, "worktree", "prune"); err != nil {
		fmt.Fprintf(os.Stderr, "gitops: prune merge worktrees: %v: %s\n", err, strings.TrimSpace(string(stderr)))
	}
}

// conflictedFiles returns the worktree's unmerged paths — exactly the files git
// left with <<<<<<< conflict markers. An error listing them yields nil, so the
// caller falls back to classifying a failed merge as a git-level error.
func conflictedFiles(ctx context.Context, wtGit runner) []string {
	stdout, _, err := wtGit(ctx, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(stdout)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files
}

// runVerify runs command via `sh -c` in dir and reports its combined output and
// whether it succeeded. A non-zero exit (or any run error, including a timeout
// kill) is a verification failure, not a Go error: the merge gate routes on the
// boolean. A positive timeout derives a deadline from ctx so a runaway verify is
// killed and reported as a failure rather than blocking the gate forever.
func runVerify(ctx context.Context, dir, command string, timeout time.Duration) (output string, ok bool) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// sanitizeName makes a branch name safe for a single path segment by replacing
// the path separators a ref may contain.
func sanitizeName(name string) string {
	r := strings.NewReplacer("/", "-", string(filepath.Separator), "-", " ", "-")
	return r.Replace(strings.TrimSpace(name))
}
