package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runGHFunc is the signature of the gh shell-out hook. It is stored as a field
// on Source so tests can substitute a fake without invoking the real binary.
// Implementations return gh's stdout and, on a non-zero exit, a *ghError
// carrying the exit code and stderr so callers can classify the failure.
type runGHFunc func(ctx context.Context, args ...string) ([]byte, error)

// ghError reports a non-zero `gh` exit. It preserves the exit code and stderr
// so classifyGHError can map failures onto typed adapter errors via errors.As
// rather than brittle string matching at the call site.
type ghError struct {
	Args     []string
	ExitCode int
	Stderr   string
	Err      error
}

// Error implements the error interface.
func (e *ghError) Error() string {
	return fmt.Sprintf("gh %s: exit %d: %s", strings.Join(e.Args, " "), e.ExitCode, strings.TrimSpace(e.Stderr))
}

// Unwrap exposes the underlying exec error for errors.Is/As traversal.
func (e *ghError) Unwrap() error { return e.Err }

// defaultRunGH returns a runGHFunc that executes the real `gh` binary. When
// token is non-empty it is injected as GH_TOKEN; when empty, gh is left to
// resolve its own credential chain (gh config, keychain, ambient GH_TOKEN).
func defaultRunGH(token string) runGHFunc {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "gh", args...)
		cmd.Env = os.Environ()
		if token != "" {
			cmd.Env = append(cmd.Env, "GH_TOKEN="+token)
		}

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			ge := &ghError{Args: args, Stderr: stderr.String(), Err: err, ExitCode: -1}
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				ge.ExitCode = exitErr.ExitCode()
			}
			return stdout.Bytes(), ge
		}
		return stdout.Bytes(), nil
	}
}
