// Package gitops is Quasar's vanilla-git output safety perimeter. Every git
// write Quasar performs (push, branch, commit, fetch) goes through a *Client
// here, never through scattered exec.Command("git", …) calls. The package uses
// the git binary directly — never `gh` — so Quasar's output side stays
// compatible with any forge. Pushes are confined to the quasar/* ref namespace
// and destructive operations against base branches are not exposed at all.
//
// An architecture test (internal/arch_test) forbids direct git exec outside
// this package; see docs/safety.md for the full perimeter design.
package gitops

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// runner executes a git subcommand and returns its stdout, stderr, and the
// process error. It is a function field on Client so tests can inject a fake
// without spawning real git.
type runner func(ctx context.Context, args ...string) (stdout, stderr []byte, err error)

// shellRunner executes a single shell command (via `sh -c`) with dir as the
// working directory and returns its stdout, stderr, exit code, and run error.
// It is the test seam for the pre-commit runner.
type shellRunner func(ctx context.Context, dir, command string) (stdout, stderr []byte, exitCode int, err error)

// Client is the single entry point for all git writes Quasar performs against a
// worktree. Construct one with New (production) or NewWithRunner (tests).
type Client struct {
	workDir  string      // worktree root; passed to git via -C
	runGit   runner      // injectable git executor
	runShell shellRunner // injectable shell executor for pre-commit commands
}

// New returns a Client that shells out to the real git binary in workDir.
func New(workDir string) *Client {
	return &Client{
		workDir:  workDir,
		runGit:   defaultRunGit(workDir),
		runShell: defaultRunShell,
	}
}

// NewWithRunner returns a Client whose git invocations are handled by the given
// runner instead of the real git binary. The pre-commit shell runner still
// defaults to the real shell; tests that exercise pre-commit may override the
// unexported runShell field directly.
func NewWithRunner(workDir string, r runner) *Client {
	return &Client{
		workDir:  workDir,
		runGit:   r,
		runShell: defaultRunShell,
	}
}

// defaultRunGit returns a runner that executes `git -C <workDir> <args…>`.
func defaultRunGit(workDir string) runner {
	return func(ctx context.Context, args ...string) ([]byte, []byte, error) {
		full := append([]string{"-C", workDir}, args...)
		cmd := exec.CommandContext(ctx, "git", full...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		return stdout.Bytes(), stderr.Bytes(), err
	}
}

// run executes a git subcommand and returns its trimmed stdout. On failure it
// wraps the error with the subcommand and trimmed stderr for context.
func (c *Client) run(ctx context.Context, args ...string) (string, error) {
	stdout, stderr, err := c.runGit(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(stderr)))
	}
	return strings.TrimSpace(string(stdout)), nil
}

// Status reports whether the worktree is clean (no staged, unstaged, or
// untracked changes).
func (c *Client) Status(ctx context.Context) (clean bool, err error) {
	out, err := c.run(ctx, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out == "", nil
}

// HeadSHA returns the full commit SHA of HEAD.
func (c *Client) HeadSHA(ctx context.Context) (string, error) {
	return c.run(ctx, "rev-parse", "HEAD")
}

// CurrentBranch returns the short name of the currently checked-out branch.
func (c *Client) CurrentBranch(ctx context.Context) (string, error) {
	return c.run(ctx, "rev-parse", "--abbrev-ref", "HEAD")
}

// CreateBranch creates a new branch named name. The name must be in the
// quasar/* namespace; any other name returns ErrUnsafeRef without invoking git.
func (c *Client) CreateBranch(ctx context.Context, name string) error {
	if !IsQuasarBranch(name) {
		return fmt.Errorf("%w: %q is not a quasar/* branch", ErrUnsafeRef, name)
	}
	_, err := c.run(ctx, "branch", name)
	return err
}

// CheckoutBranch checks out an existing branch named name. The branch must
// already exist; checking out a missing branch returns the underlying git error.
func (c *Client) CheckoutBranch(ctx context.Context, name string) error {
	if _, _, err := c.runGit(ctx, "rev-parse", "--verify", "--quiet", "refs/heads/"+name); err != nil {
		return fmt.Errorf("checkout %q: branch does not exist: %w", name, err)
	}
	_, err := c.run(ctx, "checkout", name)
	return err
}

// Diff returns the unified diff between baseRef and headRef.
func (c *Client) Diff(ctx context.Context, baseRef, headRef string) (string, error) {
	stdout, stderr, err := c.runGit(ctx, "diff", baseRef, headRef)
	if err != nil {
		return "", fmt.Errorf("git diff %s %s: %w: %s", baseRef, headRef, err, strings.TrimSpace(string(stderr)))
	}
	return string(stdout), nil
}

// CommitInfo is a single commit's metadata as returned by Log.
type CommitInfo struct {
	SHA     string
	Author  string
	Subject string
}

// LogOpts configures Log. A zero value lists the full history of HEAD.
type LogOpts struct {
	// MaxCount caps the number of commits returned (git log -n). Zero means no
	// limit.
	MaxCount int
	// Range restricts the log to a revision range (e.g. "base..head"). Empty
	// means HEAD's full history.
	Range string
}

// logFieldSep and logRecordSep are ASCII unit/record separators used to make
// `git log --pretty` output unambiguous to parse (subjects may contain any
// printable character but not these control bytes).
const (
	logFieldSep  = "\x1f"
	logRecordSep = "\x1e"
)

// Log returns commit metadata for the history selected by opts, newest first.
func (c *Client) Log(ctx context.Context, opts LogOpts) ([]CommitInfo, error) {
	args := []string{"log", "--pretty=format:%H" + logFieldSep + "%an" + logFieldSep + "%s" + logRecordSep}
	if opts.MaxCount > 0 {
		args = append(args, fmt.Sprintf("-n%d", opts.MaxCount))
	}
	if opts.Range != "" {
		args = append(args, opts.Range)
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}

// parseLog splits Log's separator-delimited output into CommitInfo records.
func parseLog(out string) []CommitInfo {
	var commits []CommitInfo
	for _, record := range strings.Split(out, logRecordSep) {
		record = strings.Trim(record, "\n")
		if record == "" {
			continue
		}
		fields := strings.Split(record, logFieldSep)
		if len(fields) != 3 {
			continue
		}
		commits = append(commits, CommitInfo{
			SHA:     fields[0],
			Author:  fields[1],
			Subject: fields[2],
		})
	}
	return commits
}
