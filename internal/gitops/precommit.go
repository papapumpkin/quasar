package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrPreCommitFailed indicates a pre-commit command exited non-zero while
// FailOnError was set. The wrapped message names the offending command, its
// exit code, and its stderr.
var ErrPreCommitFailed = errors.New("pre-commit command failed")

// PreCommitConfig declares the formatter/linter commands run before a commit.
// It mirrors the [pre_commit] block in .quasar.yaml. The zero value runs no
// commands.
type PreCommitConfig struct {
	// Commands are shell command strings, each executed as `sh -c <cmd>` with
	// the worktree as CWD, in order.
	Commands []string
	// FailOnError aborts the commit when any command exits non-zero.
	FailOnError bool
}

// PreCommitResult captures the outcome of a single pre-commit command.
type PreCommitResult struct {
	Command  string
	ExitCode int
	Stdout   string
	Stderr   string
	// Err is the underlying run error (e.g. command not found), or nil if the
	// command ran to completion regardless of its exit code.
	Err error
}

// defaultRunShell executes command via `sh -c` in dir, capturing output.
func defaultRunShell(ctx context.Context, dir, command string) (stdout, stderr []byte, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	var o, e bytes.Buffer
	cmd.Stdout = &o
	cmd.Stderr = &e
	err = cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return o.Bytes(), e.Bytes(), exitCode, err
}

// RunPreCommit executes each command in cfg.Commands in order with the worktree
// as CWD, capturing stdout/stderr per command. When cfg.FailOnError is true,
// the first non-zero exit aborts the run and returns ErrPreCommitFailed; the
// results slice still includes every command attempted up to and including the
// failure. When FailOnError is false, all commands run and any failures are
// reported only via the returned results (no error).
func (c *Client) RunPreCommit(ctx context.Context, cfg PreCommitConfig) ([]PreCommitResult, error) {
	results := make([]PreCommitResult, 0, len(cfg.Commands))
	for _, command := range cfg.Commands {
		stdout, stderr, code, runErr := c.runShell(ctx, c.workDir, command)
		results = append(results, PreCommitResult{
			Command:  command,
			ExitCode: code,
			Stdout:   string(stdout),
			Stderr:   string(stderr),
			Err:      runErr,
		})
		if runErr != nil && cfg.FailOnError {
			return results, fmt.Errorf("%w: %q exited %d: %s",
				ErrPreCommitFailed, command, code, strings.TrimSpace(string(stderr)))
		}
	}
	return results, nil
}
