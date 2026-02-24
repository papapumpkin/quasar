package nebula

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
//
// If checkout fails because untracked working tree files would be
// overwritten (e.g. a state file left over from a previous run on
// another branch), the conflicting files are removed and the checkout
// is retried. The files exist on the target branch and are restored
// by the checkout.
func (b *BranchManager) CreateOrCheckout(ctx context.Context) error {
	if b == nil {
		return nil
	}

	if b.branchExists(ctx) {
		out, err := b.runCheckout(ctx, b.branch)
		if err == nil {
			return nil
		}
		// If checkout fails due to untracked files that would be
		// overwritten, remove them and retry once.
		if conflicts := parseUntrackedConflicts(string(out)); len(conflicts) > 0 {
			for _, f := range conflicts {
				os.Remove(filepath.Join(b.dir, f))
			}
			if _, retryErr := b.runCheckout(ctx, b.branch); retryErr != nil {
				return fmt.Errorf("git checkout %s: %w: %s", b.branch, err, strings.TrimSpace(string(out)))
			}
			return nil
		}
		return fmt.Errorf("git checkout %s: %w: %s", b.branch, err, strings.TrimSpace(string(out)))
	}

	// Branch does not exist — create from current HEAD.
	if out, err := b.runCheckout(ctx, "-b", b.branch); err != nil {
		return fmt.Errorf("git checkout -b %s: %w: %s", b.branch, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runCheckout executes git checkout with the given arguments.
func (b *BranchManager) runCheckout(ctx context.Context, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"-C", b.dir, "checkout"}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	return cmd.CombinedOutput()
}

// parseUntrackedConflicts extracts file paths from a git checkout error
// reporting that untracked working tree files would be overwritten.
// Returns nil if the output does not match this error pattern.
func parseUntrackedConflicts(output string) []string {
	if !strings.Contains(output, "untracked working tree files would be overwritten") {
		return nil
	}
	var files []string
	inFileList := false
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(line, "would be overwritten") {
			inFileList = true
			continue
		}
		if inFileList {
			if trimmed == "" || strings.HasPrefix(trimmed, "Please") || strings.HasPrefix(trimmed, "Aborting") {
				break
			}
			files = append(files, trimmed)
		}
	}
	return files
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
