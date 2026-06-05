package gitops

import (
	"context"
	"errors"
)

// ErrNothingToCommit indicates the index was empty after pre-commit hooks ran,
// so no commit was created. This is not necessarily a failure — the agent may
// have already committed, or a pre-commit command may have normalized a change
// back to a no-op.
var ErrNothingToCommit = errors.New("nothing to commit: index is empty after pre-commit")

// CommitOpts configures Commit.
type CommitOpts struct {
	// PreCommit declares formatter/linter commands to run before staging and
	// committing. The zero value runs no commands.
	PreCommit PreCommitConfig
	// Author overrides the commit author (e.g. "Quasar <quasar@noreply.local>").
	// Empty uses the repository's configured identity.
	Author string
}

// AddTracked stages modifications to already-tracked files (git add -u). New
// files are not staged here; they are introduced via the agent's own tooling
// and tracked separately by the loop.
func (c *Client) AddTracked(ctx context.Context) error {
	_, err := c.run(ctx, "add", "-u")
	return err
}

// Commit runs pre-commit hooks, re-stages tracked modifications, and creates a
// commit. The sequence is:
//
//  1. Run opts.PreCommit. If a command fails and FailOnError is set, the commit
//     is aborted and the pre-commit error is returned (git commit is not run).
//  2. Re-stage tracked modifications introduced by the hooks (git add -u).
//  3. If the index is empty, return ErrNothingToCommit.
//  4. git commit (with --author when opts.Author is set).
//
// It returns the new HEAD SHA on success.
func (c *Client) Commit(ctx context.Context, message string, opts CommitOpts) (sha string, err error) {
	if _, err := c.RunPreCommit(ctx, opts.PreCommit); err != nil {
		return "", err
	}

	if err := c.AddTracked(ctx); err != nil {
		return "", err
	}

	// `git diff --cached --quiet` exits 0 when the index is empty and non-zero
	// when there are staged changes. A nil error here therefore means nothing
	// is staged.
	if _, _, diffErr := c.runGit(ctx, "diff", "--cached", "--quiet"); diffErr == nil {
		return "", ErrNothingToCommit
	}

	args := []string{"commit", "-m", message}
	if opts.Author != "" {
		args = append(args, "--author", opts.Author)
	}
	if _, err := c.run(ctx, args...); err != nil {
		return "", err
	}

	return c.HeadSHA(ctx)
}
