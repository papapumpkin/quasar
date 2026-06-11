package gitops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// BranchFor returns the deterministic quasar/* branch a nebula's build commits
// to. Always under the quasar/ namespace so the perimeter permits the push.
func (c *Client) BranchFor(nebulaID string) string {
	return "quasar/nebula-" + sanitizeName(nebulaID)
}

// EnsureWorktree creates — idempotently — an isolated git worktree for a
// nebula's build, on its quasar/<nebula> branch, so the build never writes to
// the operator's working tree or current branch. Returns the worktree path.
// Reused if it already exists (a resumed run).
func (c *Client) EnsureWorktree(ctx context.Context, nebulaID string) (string, error) {
	branch := c.BranchFor(nebulaID)
	if !IsQuasarBranch(branch) {
		return "", fmt.Errorf("%w: %q is not a quasar/* branch", ErrUnsafeRef, branch)
	}
	path := filepath.Join(c.workDir, ".quasar", "worktrees", sanitizeName(nebulaID))
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("gitops: create worktree dir: %w", err)
	}
	// Create the branch + worktree from HEAD. If the branch already exists (a
	// prior run for this nebula), add the worktree onto it without -b.
	if _, stderr, err := c.runGit(ctx, "worktree", "add", "-b", branch, path); err != nil {
		if _, stderr2, err2 := c.runGit(ctx, "worktree", "add", path, branch); err2 != nil {
			return "", fmt.Errorf("gitops: worktree add %q on %q: %s / %s", path, branch, stderr, stderr2)
		}
	}
	return path, nil
}

// RemoveWorktree removes a build worktree (best-effort). It does not force:
// a clean worktree (commits made) removes fine, and a dirty one is left for the
// worktree reaper rather than force-discarding a coder's uncommitted work. The
// branch ref and any pushed commits are unaffected.
func (c *Client) RemoveWorktree(ctx context.Context, path string) error {
	_, _, err := c.runGit(ctx, "worktree", "remove", path)
	return err
}
