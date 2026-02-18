package nebula

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// BranchManager manages git branches for nebula execution.
// It ensures that all work for a nebula happens on a dedicated branch
// (nebula/<name>), providing isolation from main and other nebulas.
type BranchManager struct {
	dir    string // working directory
	branch string // target branch name (e.g., "nebula/statusbar-regression")
}

// NewBranchManager creates a BranchManager with branch name nebula/<nebulaName>.
// Returns an error if the directory is not in a git repo or git is unavailable.
// Does NOT create or checkout the branch yet — call CreateOrCheckout for that.
// slugifyBranch converts a human-readable name into a valid git branch segment.
// Spaces become hyphens, disallowed characters are stripped, and runs of hyphens
// are collapsed. The result is lowercased and trimmed of leading/trailing hyphens.
func slugifyBranch(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-':
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '/':
			b.WriteRune('-')
		default:
			// drop disallowed characters (&, ~, ^, :, ?, *, [, etc.)
		}
	}
	// Collapse runs of hyphens and trim edges.
	s := b.String()
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-.")
}

func NewBranchManager(ctx context.Context, dir, nebulaName string) (*BranchManager, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("git not available: %w", err)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--git-dir")
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("not a git repository: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return &BranchManager{
		dir:    dir,
		branch: "nebula/" + slugifyBranch(nebulaName),
	}, nil
}

// CreateOrCheckout checks out the target branch if it already exists,
// or creates it from the current HEAD and checks it out. This handles
// both fresh starts and resumptions of previous nebula runs.
func (b *BranchManager) CreateOrCheckout(ctx context.Context) error {
	if b == nil {
		return nil
	}

	if b.branchExists(ctx) {
		// Branch exists — check it out.
		cmd := exec.CommandContext(ctx, "git", "-C", b.dir, "checkout", b.branch)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git checkout %s: %w: %s", b.branch, err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	// Branch does not exist — create from current HEAD.
	cmd := exec.CommandContext(ctx, "git", "-C", b.dir, "checkout", "-b", b.branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout -b %s: %w: %s", b.branch, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// EnsureBranch verifies the current branch matches the expected branch name.
// Returns a descriptive error if the working directory is on the wrong branch.
func (b *BranchManager) EnsureBranch(ctx context.Context) error {
	if b == nil {
		return nil
	}

	current, err := b.currentBranch(ctx)
	if err != nil {
		return fmt.Errorf("checking current branch: %w", err)
	}
	if current != b.branch {
		return fmt.Errorf("expected branch %q but on %q", b.branch, current)
	}
	return nil
}

// Branch returns the target branch name (e.g., "nebula/statusbar-regression").
func (b *BranchManager) Branch() string {
	if b == nil {
		return ""
	}
	return b.branch
}

// currentBranch returns the name of the currently checked-out branch.
func (b *BranchManager) currentBranch(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", b.dir, "rev-parse", "--abbrev-ref", "HEAD")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git rev-parse --abbrev-ref HEAD: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// branchExists checks whether the target branch already exists.
func (b *BranchManager) branchExists(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", b.dir, "branch", "--list", b.branch)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return false
	}
	return strings.TrimSpace(stdout.String()) != ""
}
