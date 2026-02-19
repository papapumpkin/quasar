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
	// The summary is a short human-readable description included in the commit message.
	// Returns the HEAD SHA after the commit. If the working tree is clean,
	// no commit is created and the current HEAD SHA is returned.
	CommitCycle(ctx context.Context, label string, cycle int, summary string) (sha string, err error)
	// HeadSHA returns the current HEAD commit SHA.
	HeadSHA(ctx context.Context) (string, error)
	// DiffRange returns the full diff between two commits (base..head).
	DiffRange(ctx context.Context, base, head string) (string, error)
	// ResetTo performs a hard reset to the given SHA, restoring the working
	// tree to that commit's state. The SHA must be a valid, reachable commit.
	// If branch enforcement is active, the current branch is verified first.
	ResetTo(ctx context.Context, sha string) error
}

// gitCycleCommitter implements CycleCommitter using the git CLI.
type gitCycleCommitter struct {
	dir    string // working directory for git commands
	branch string // expected branch; empty = no enforcement
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

// NewCycleCommitterWithBranch returns a CycleCommitter that verifies the
// working directory is on the expected branch before every commit.
// If branch is empty, no enforcement is applied.
func NewCycleCommitterWithBranch(ctx context.Context, dir, branch string) CycleCommitter {
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--git-dir")
	if err := cmd.Run(); err != nil {
		return nil
	}
	return &gitCycleCommitter{dir: dir, branch: branch}
}

// CommitCycle stages all changes and creates a commit for the given cycle.
// If the working tree is clean, no commit is created and the current HEAD SHA
// is returned.
func (g *gitCycleCommitter) CommitCycle(ctx context.Context, label string, cycle int, summary string) (string, error) {
	if g == nil {
		return "", nil
	}

	if err := g.ensureBranch(ctx); err != nil {
		return "", err
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
	msg := fmt.Sprintf("%s/cycle-%d: %s", label, cycle, summary)
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

// DiffRange returns the full diff between two commits (base..head).
// If g is nil, it returns an empty string.
func (g *gitCycleCommitter) DiffRange(ctx context.Context, base, head string) (string, error) {
	if g == nil {
		return "", nil
	}
	ref := base + ".." + head
	cmd := exec.CommandContext(ctx, "git", "-C", g.dir, "diff", ref)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git diff %s: %w: %s", ref, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// ResetTo performs a hard reset to the given SHA, restoring the working tree
// to that commit's state. It verifies the SHA is a valid, reachable commit
// and checks the current branch if branch enforcement is active.
// If g is nil, this is a no-op.
func (g *gitCycleCommitter) ResetTo(ctx context.Context, sha string) error {
	if g == nil {
		return nil
	}

	if err := g.ensureBranch(ctx); err != nil {
		return err
	}

	// Verify the SHA exists and is a reachable commit.
	verifyCmd := exec.CommandContext(ctx, "git", "-C", g.dir, "merge-base", "--is-ancestor", sha, "HEAD")
	var verifyStderr bytes.Buffer
	verifyCmd.Stderr = &verifyStderr
	if err := verifyCmd.Run(); err != nil {
		return fmt.Errorf("sha %s is not a valid ancestor of HEAD: %w: %s", sha, err, strings.TrimSpace(verifyStderr.String()))
	}

	// Perform the hard reset.
	resetCmd := exec.CommandContext(ctx, "git", "-C", g.dir, "reset", "--hard", sha)
	var resetStderr bytes.Buffer
	resetCmd.Stderr = &resetStderr
	if err := resetCmd.Run(); err != nil {
		return fmt.Errorf("git reset --hard %s: %w: %s", sha, err, strings.TrimSpace(resetStderr.String()))
	}
	return nil
}

// ensureBranch verifies the working directory is on the expected branch.
// If branch is empty, this is a no-op.
func (g *gitCycleCommitter) ensureBranch(ctx context.Context) error {
	if g.branch == "" {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "-C", g.dir, "rev-parse", "--abbrev-ref", "HEAD")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("checking current branch: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	current := strings.TrimSpace(stdout.String())
	if current != g.branch {
		return fmt.Errorf("branch mismatch: expected %q, on %q", g.branch, current)
	}
	return nil
}
