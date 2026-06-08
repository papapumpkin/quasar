package constellations

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/papapumpkin/quasar/internal/gitops"
)

// opMergeAttemptName / opFulfillEntanglementsName are the registered builtin
// names the merge-gate constellation routes through.
const (
	opMergeAttemptName         = "merge_attempt"
	opFulfillEntanglementsName = "fulfill_entanglements"
)

// merger attempts a branch merge in a throwaway worktree and classifies the
// outcome. gitops.MergeAttempt satisfies it; the interface is defined here,
// where opMergeAttempt consumes it, so the operator is testable with a fake and
// dispatchBuiltin never imports git machinery directly.
type merger interface {
	Try(ctx context.Context, opts gitops.TryOpts) (gitops.MergeOutcome, error)
	Cleanup(ctx context.Context, worktree string)
}

// mergeAttempter returns the runtime's merger: an injected test seam when set,
// otherwise a gitops-backed merger bound to the repo path. A runtime with no
// repo path cannot merge, so it errors rather than constructing a useless
// client.
func (r *Runtime) mergeAttempter() (merger, error) {
	if r.merger != nil {
		return r.merger, nil
	}
	if strings.TrimSpace(r.repoPath) == "" {
		return nil, fmt.Errorf("merge_attempt: no repo path configured")
	}
	return &gitops.MergeAttempt{Client: gitops.New(r.repoPath)}, nil
}

// opMergeAttempt merges the run's branch into its parent in a throwaway
// worktree and reports the classified outcome for the merge-gate constellation
// to route on. The worktree is kept only for a markers or build_failure
// outcome, where a downstream conflict resolver inherits it; a clean or
// merge_error outcome cleans it up immediately. The optional verify_command and
// verify_timeout inputs override the Go-centric defaults (the config → input
// plumbing for these lands with the merge-gate-firing supervisor). Output:
//
//	{result, conflicted_files, build_output, merged_sha, worktree_path}
func opMergeAttempt(ctx context.Context, rt *Runtime, _ *State, args map[string]any) (map[string]any, error) {
	src, _ := args["src_branch"].(string)
	dst, _ := args["dst_branch"].(string)
	if strings.TrimSpace(src) == "" || strings.TrimSpace(dst) == "" {
		return nil, fmt.Errorf("merge_attempt: src_branch and dst_branch are required")
	}
	verify, _ := args["verify_command"].(string)
	runID, _ := args["run_id"].(string)
	timeout, err := parseTimeout(args["verify_timeout"])
	if err != nil {
		return nil, err
	}

	m, err := rt.mergeAttempter()
	if err != nil {
		return nil, err
	}
	// Keep the worktree up front; the resolver-hand-off cases (markers,
	// build_failure) need it, and we remove it ourselves for the others.
	outcome, err := m.Try(ctx, gitops.TryOpts{
		SrcBranch:     src,
		DstBranch:     dst,
		VerifyCommand: verify,
		Timeout:       timeout,
		RunID:         runID,
		KeepWorktree:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("merge_attempt: %w", err)
	}
	if outcome.Result == gitops.MergeClean || outcome.Result == gitops.MergeError {
		m.Cleanup(ctx, outcome.Worktree)
	}

	conflicted := outcome.ConflictedFiles
	if conflicted == nil {
		conflicted = []string{}
	}
	return map[string]any{
		"result":           string(outcome.Result),
		"conflicted_files": conflicted,
		"build_output":     outcome.BuildOutput,
		"merged_sha":       outcome.MergedSHA,
		"worktree_path":    outcome.Worktree,
	}, nil
}

// parseTimeout coerces the verify_timeout input into a duration. It accepts a
// Go duration string (e.g. "5m", matching the .quasar.yaml merge_gate format)
// or nil/empty for no cap. A malformed value is an error so a typo surfaces
// rather than silently disabling the runaway-verify guard.
func parseTimeout(v any) (time.Duration, error) {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("merge_attempt: invalid verify_timeout %q: %w", s, err)
	}
	return d, nil
}

// opFulfillEntanglements transitions a producing run's in_flight entanglements
// to fulfilled once the merge gate confirms the merge landed (cleanly or via
// the conflict resolver). The producing run's id is supplied as args["run_id"];
// it is distinct from the merge-gate run, so the supervisor that fires the gate
// threads it through. The merged SHA is passed through unchanged so a downstream
// node (the supervisor's parent-branch commit) can read it.
//
// Best-effort and tolerant: a runtime without entanglement tracking, or a call
// with no run_id (the wiring that supplies it lands with the supervisor), is a
// no-op reporting fulfilled=false rather than an error, so the gate never fails
// a merge purely over advisory coordination bookkeeping. Output:
//
//	{fulfilled: bool, merged_sha: <passthrough>}
func opFulfillEntanglements(ctx context.Context, rt *Runtime, _ *State, args map[string]any) (map[string]any, error) {
	mergedSHA, _ := args["merged_sha"].(string)
	out := map[string]any{"fulfilled": false, "merged_sha": mergedSHA}

	runID, _ := args["run_id"].(string)
	if rt.entanglements == nil || strings.TrimSpace(runID) == "" {
		return out, nil
	}
	if err := rt.entanglements.Fulfill(ctx, runID); err != nil {
		return nil, fmt.Errorf("fulfill_entanglements: %w", err)
	}
	out["fulfilled"] = true
	return out, nil
}
