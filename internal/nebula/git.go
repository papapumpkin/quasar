package nebula

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// GitCommitter creates commits at phase boundaries.
type GitCommitter interface {
	// CommitPhase stages all changes and creates a commit for the completed phase.
	// If the working tree is clean, this is a no-op.
	CommitPhase(ctx context.Context, nebulaName, phaseID, phaseTitle string) error
	// Diff returns the diff of unstaged/staged changes since the last commit.
	Diff(ctx context.Context) (string, error)
	// DiffLastCommit returns the diff of the most recent commit (HEAD~1..HEAD).
	DiffLastCommit(ctx context.Context) (string, error)
	// DiffStatLastCommit returns the --stat output for the most recent commit.
	DiffStatLastCommit(ctx context.Context) (string, error)
}

// gitCommitter implements GitCommitter using the git CLI.
type gitCommitter struct {
	dir    string // working directory for git commands
	branch string // expected branch; empty = no enforcement
}

// NewGitCommitter creates a GitCommitter for the given directory.
// If git is not available or the directory is not a git repository,
// it returns nil (not an error). The caller should skip committing
// when the committer is nil, following the same pattern as Watcher.
func NewGitCommitter(ctx context.Context, dir string) GitCommitter {
	// Check that git is available.
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	// Check that dir is inside a git repository.
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--git-dir")
	if err := cmd.Run(); err != nil {
		return nil
	}
	return &gitCommitter{dir: dir}
}

// NewGitCommitterWithBranch creates a GitCommitter that verifies the working
// directory is on the expected branch before every commit. If branch is empty,
// no enforcement is applied.
func NewGitCommitterWithBranch(ctx context.Context, dir, branch string) GitCommitter {
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--git-dir")
	if err := cmd.Run(); err != nil {
		return nil
	}
	return &gitCommitter{dir: dir, branch: branch}
}

// CommitPhase stages all changes and creates a commit for the completed phase.
// If the working tree is clean (nothing to commit), this is a no-op.
func (g *gitCommitter) CommitPhase(ctx context.Context, nebulaName, phaseID, phaseTitle string) error {
	if err := g.ensureBranch(ctx); err != nil {
		return err
	}

	// Check for changes first.
	statusCmd := exec.CommandContext(ctx, "git", "-C", g.dir, "status", "--porcelain")
	out, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil // clean working tree, nothing to commit
	}

	// Stage all changes.
	addCmd := exec.CommandContext(ctx, "git", "-C", g.dir, "add", "-A")
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	// Create commit with descriptive message.
	// Truncate phaseTitle to keep the commit message under ~80 chars.
	prefix := fmt.Sprintf("%s/%s: ", nebulaName, phaseID)
	maxTitle := 80 - len(prefix)
	title := phaseTitle
	if maxTitle > 3 && len(title) > maxTitle {
		title = title[:maxTitle-3] + "..."
	}
	msg := prefix + title
	commitCmd := exec.CommandContext(ctx, "git", "-C", g.dir, "commit", "-m", msg)
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

// Diff returns the diff of changes since the last commit.
func (g *gitCommitter) Diff(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", g.dir, "diff", "HEAD")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git diff: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// DiffLastCommit returns the diff of the most recent commit (HEAD~1..HEAD).
func (g *gitCommitter) DiffLastCommit(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", g.dir, "diff", "HEAD~1..HEAD")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git diff HEAD~1..HEAD: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// DiffStatLastCommit returns the --stat output for the most recent commit.
func (g *gitCommitter) DiffStatLastCommit(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", g.dir, "diff", "--stat", "HEAD~1..HEAD")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git diff --stat HEAD~1..HEAD: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// ensureBranch verifies the working directory is on the expected branch.
// If branch is empty, this is a no-op.
func (g *gitCommitter) ensureBranch(ctx context.Context) error {
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

// InterventionFileNames returns the filenames that should be excluded from
// git commits in a nebula directory. These are the PAUSE and STOP intervention
// files used for human control of nebula execution.
func InterventionFileNames() []string {
	names := make([]string, 0, len(interventionFiles))
	for name := range interventionFiles {
		names = append(names, name)
	}
	return names
}

// GitExcludePatterns returns gitignore-style patterns for intervention files.
// Callers can use these patterns with git add --exclude or .gitignore entries.
func GitExcludePatterns() []string {
	names := InterventionFileNames()
	patterns := make([]string, len(names))
	copy(patterns, names)
	return patterns
}

// PostCompletionResult holds the outcomes of the post-completion git workflow
// (commit remaining changes, push to origin, checkout main).
type PostCompletionResult struct {
	// PushBranch is the branch that was pushed (e.g., "nebula/my-nebula").
	PushBranch string
	// PushErr is non-nil if the push failed.
	PushErr error
	// CheckoutErr is non-nil if the checkout to main failed.
	CheckoutErr error
}

// Summary returns a human-readable summary of the git workflow results.
func (r *PostCompletionResult) Summary() string {
	var b strings.Builder
	if r.PushErr != nil {
		b.WriteString(fmt.Sprintf("Push failed: %v", r.PushErr))
	} else {
		b.WriteString(fmt.Sprintf("Pushed to origin/%s", r.PushBranch))
	}
	b.WriteString("\n")
	if r.CheckoutErr != nil {
		b.WriteString(fmt.Sprintf("Checkout main failed: %v", r.CheckoutErr))
	} else {
		b.WriteString("Checked out main")
	}
	return b.String()
}

// PostCompletion runs the post-nebula git workflow: commit any remaining
// changes, push the branch to origin with --set-upstream, and checkout main.
// Errors are captured in the result, not returned, so the caller can display
// them without aborting.
func PostCompletion(ctx context.Context, dir, branch string) *PostCompletionResult {
	result := &PostCompletionResult{PushBranch: branch}

	// Stage and commit any remaining uncommitted changes.
	// Non-fatal: we still try to push whatever commits exist.
	_ = commitRemaining(ctx, dir, branch)

	// Push with --set-upstream to handle branches with no upstream.
	pushCmd := exec.CommandContext(ctx, "git", "-C", dir, "push", "--set-upstream", "origin", branch)
	var pushStderr bytes.Buffer
	pushCmd.Stderr = &pushStderr
	if err := pushCmd.Run(); err != nil {
		result.PushErr = fmt.Errorf("%w: %s", err, strings.TrimSpace(pushStderr.String()))
	}

	// Checkout main.
	checkoutCmd := exec.CommandContext(ctx, "git", "-C", dir, "checkout", "main")
	var checkoutStderr bytes.Buffer
	checkoutCmd.Stderr = &checkoutStderr
	if err := checkoutCmd.Run(); err != nil {
		result.CheckoutErr = fmt.Errorf("%w: %s", err, strings.TrimSpace(checkoutStderr.String()))
	}

	return result
}

// commitRemaining stages and commits any uncommitted changes. If the working
// tree is clean, this is a no-op. Returns nil on success or clean tree.
func commitRemaining(ctx context.Context, dir, branch string) error {
	statusCmd := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain")
	out, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil // clean working tree
	}

	addCmd := exec.CommandContext(ctx, "git", "-C", dir, "add", "-A")
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	msg := fmt.Sprintf("nebula: final changes on %s", branch)
	commitCmd := exec.CommandContext(ctx, "git", "-C", dir, "commit", "-m", msg)
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}
