package loop

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CycleCommitter creates git commits at coder-cycle boundaries.
type CycleCommitter interface {
	// CommitCycle stages all changes and creates a commit for the given cycle.
	// Returns the HEAD SHA after the commit. If the working tree is clean,
	// no commit is created and the current HEAD SHA is returned.
	CommitCycle(ctx context.Context, label string, cycle int) (sha string, err error)
	// HeadSHA returns the current HEAD commit SHA.
	HeadSHA(ctx context.Context) (string, error)
}

// gitCycleCommitter implements CycleCommitter using the git CLI.
type gitCycleCommitter struct {
	dir string // working directory for git commands
}

// NewCycleCommitter returns a CycleCommitter if the working directory is a git
// repo, or nil otherwise. A nil return is not an error — callers should treat
// a nil CycleCommitter as a no-op.
func NewCycleCommitter(ctx context.Context, dir string) CycleCommitter {
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--git-dir")
	if err := cmd.Run(); err != nil {
		return nil
	}
	return &gitCycleCommitter{dir: dir}
}

// CommitCycle stages all changes and creates a commit for the given cycle.
// If the working tree is clean, no commit is created and the current HEAD SHA
// is returned.
func (g *gitCycleCommitter) CommitCycle(ctx context.Context, label string, cycle int) (string, error) {
	if g == nil {
		return "", nil
	}

	// Stage all changes.
	addCmd := exec.CommandContext(ctx, "git", "-C", g.dir, "add", "-A")
	if err := addCmd.Run(); err != nil {
		return "", fmt.Errorf("git add: %w", err)
	}

	// Check for staged changes.
	statusCmd := exec.CommandContext(ctx, "git", "-C", g.dir, "diff", "--cached", "--quiet")
	if statusCmd.Run() == nil {
		// Nothing staged — return current HEAD.
		return g.HeadSHA(ctx)
	}

	// Create commit with descriptive message.
	msg := fmt.Sprintf("quasar: %s cycle-%d", label, cycle)
	commitCmd := exec.CommandContext(ctx, "git", "-C", g.dir, "commit", "-m", msg)
	if err := commitCmd.Run(); err != nil {
		return "", fmt.Errorf("git commit: %w", err)
	}

	return g.HeadSHA(ctx)
}

// HeadSHA returns the current HEAD commit SHA.
func (g *gitCycleCommitter) HeadSHA(ctx context.Context) (string, error) {
	if g == nil {
		return "", nil
	}

	cmd := exec.CommandContext(ctx, "git", "-C", g.dir, "rev-parse", "HEAD")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
