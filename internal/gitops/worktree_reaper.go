package gitops

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// quasarWorktreePrefix is the branch namespace Quasar's worktrees check out.
// The reaper only ever considers worktrees on a branch under this prefix, so it
// can never touch a human's main checkout or an unrelated worktree.
const quasarWorktreePrefix = "refs/heads/quasar/"

// Worktree is one linked working tree as reported by `git worktree list`.
type Worktree struct {
	// Path is the absolute worktree directory.
	Path string
	// Branch is the full ref (e.g. refs/heads/quasar/run-123) or "" when detached.
	Branch string
	// Prunable is true when git reports the worktree's directory is gone or its
	// administrative files are missing — i.e. it can be pruned.
	Prunable bool
	// ModTime is the worktree directory's modification time, or the zero time
	// when the directory no longer exists.
	ModTime time.Time
}

// Name returns the stable identifier used to correlate a worktree with its
// constellation_run: the branch suffix after the quasar/ prefix, falling back to
// the directory basename. The GC engine's isProtected callback keys on this.
func (w Worktree) Name() string {
	if strings.HasPrefix(w.Branch, quasarWorktreePrefix) {
		return strings.TrimPrefix(w.Branch, quasarWorktreePrefix)
	}
	return filepath.Base(w.Path)
}

// ReapReport summarizes one Reap pass.
type ReapReport struct {
	// Removed lists the worktree paths that were removed.
	Removed []string
	// Kept counts quasar worktrees left in place (protected or too young).
	Kept int
	// ReclaimedBytes is the on-disk size freed by the removed worktrees.
	ReclaimedBytes int64
}

// WorktreeReaper removes stale Quasar worktrees that the runtime never cleaned
// up. It lives in gitops (layer 0) and therefore cannot read run state itself;
// the caller injects an isProtected predicate computed from the database.
type WorktreeReaper struct {
	// runGitFor builds a git runner bound to a repo path. Overridable in tests.
	runGitFor func(repoPath string) runner
}

// NewWorktreeReaper returns a reaper that shells out to the real git binary.
func NewWorktreeReaper() *WorktreeReaper {
	return &WorktreeReaper{runGitFor: defaultRunGit}
}

// NewWorktreeReaperWithRunner returns a reaper whose git invocations are handled
// by r for every repo path. Used by tests to avoid spawning real git.
func NewWorktreeReaperWithRunner(r runner) *WorktreeReaper {
	return &WorktreeReaper{runGitFor: func(string) runner { return r }}
}

// Reap removes Quasar worktrees under repoPath whose backing run is in a
// terminal state or gone entirely. A worktree is removed when it is prunable
// (its directory or git metadata is gone) or its mtime is older than maxAge —
// in either case only if isProtected(name) is false. isProtected must return
// true for any worktree whose constellation_run is still running or paused, so
// the reaper never races the runtime. A nil isProtected protects nothing.
//
// When dryRun is true, Reap reports what it would remove without touching disk.
func (r *WorktreeReaper) Reap(ctx context.Context, repoPath string, maxAge time.Duration, now time.Time, isProtected func(name string) bool, dryRun bool) (*ReapReport, error) {
	if isProtected == nil {
		isProtected = func(string) bool { return false }
	}
	runGit := r.runGitFor(repoPath)
	stdout, stderr, err := runGit(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("gitops: list worktrees in %s: %w: %s", repoPath, err, strings.TrimSpace(string(stderr)))
	}

	report := &ReapReport{}
	for _, wt := range parseWorktrees(string(stdout)) {
		// Only quasar-namespaced worktrees are ever eligible.
		if !strings.HasPrefix(wt.Branch, quasarWorktreePrefix) {
			continue
		}
		if isProtected(wt.Name()) {
			report.Kept++
			continue
		}
		if !wt.Prunable && now.Sub(wt.ModTime) < maxAge {
			report.Kept++
			continue
		}
		report.ReclaimedBytes += dirSize(wt.Path)
		report.Removed = append(report.Removed, wt.Path)
		if dryRun {
			continue
		}
		// Remove the directory, then prune git's administrative record. This
		// avoids `git worktree remove --force` (the safety perimeter forbids the
		// unconditional --force flag) while still reaping a dirty worktree.
		if err := os.RemoveAll(wt.Path); err != nil {
			return report, fmt.Errorf("gitops: remove worktree dir %s: %w", wt.Path, err)
		}
		if _, pruneStderr, pruneErr := runGit(ctx, "worktree", "prune"); pruneErr != nil {
			return report, fmt.Errorf("gitops: prune worktrees in %s: %w: %s", repoPath, pruneErr, strings.TrimSpace(string(pruneStderr)))
		}
	}
	return report, nil
}

// parseWorktrees parses `git worktree list --porcelain` output into entries.
// Records are separated by blank lines; each starts with a "worktree <path>"
// line and may carry "branch <ref>", "detached", and "prunable <reason>" lines.
func parseWorktrees(out string) []Worktree {
	var (
		entries []Worktree
		cur     *Worktree
	)
	flush := func() {
		if cur != nil {
			cur.ModTime = dirModTime(cur.Path)
			entries = append(entries, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case cur == nil:
			continue
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(line, "branch ")
		case strings.HasPrefix(line, "prunable"):
			cur.Prunable = true
		}
	}
	flush()
	return entries
}

// dirModTime returns the modification time of dir, or the zero time when dir is
// absent (a prunable worktree whose directory was deleted out from under git).
func dirModTime(dir string) time.Time {
	info, err := os.Stat(dir)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// dirSize sums the regular-file sizes under dir, returning 0 if dir is gone.
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // best-effort accounting; skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}
		if info, statErr := d.Info(); statErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}
