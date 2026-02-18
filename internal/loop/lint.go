package loop

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// DefaultMaxLintRetries is the maximum number of times the coder is asked to
// fix lint issues before handing off to the reviewer regardless.
const DefaultMaxLintRetries = 2

// Linter runs lint commands against the working directory and returns any
// issues found. A nil Linter (or empty command list) is treated as a no-op.
type Linter interface {
	// Run executes all configured lint commands and returns their combined
	// output. An empty string means no issues were found.
	Run(ctx context.Context) (output string, err error)
}

// CommandLinter runs shell commands as lint checks.
type CommandLinter struct {
	Commands []string // e.g. ["go vet ./...", "go fmt ./..."]
	Dir      string   // working directory
}

// NewLinter returns a Linter for the given commands and directory.
// Returns nil if commands is empty, which callers treat as a no-op.
func NewLinter(commands []string, dir string) Linter {
	if len(commands) == 0 {
		return nil
	}
	return &CommandLinter{Commands: commands, Dir: dir}
}

// Run executes each lint command in sequence and collects output from commands
// that fail (non-zero exit). Commands that succeed (exit 0) are treated as
// auto-fixers — any stdout they produce (e.g. `go fmt` printing reformatted
// filenames) is informational and not reported as lint findings.
// This implementation never returns a fatal error; all command failures
// (including start failures) are captured as output text.
func (l *CommandLinter) Run(ctx context.Context) (string, error) {
	if l == nil || len(l.Commands) == 0 {
		return "", nil
	}

	var results strings.Builder
	for _, cmdStr := range l.Commands {
		parts := strings.Fields(cmdStr)
		if len(parts) == 0 {
			continue
		}

		cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
		cmd.Dir = l.Dir

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()

		if err == nil {
			// Command succeeded (exit 0). Any stdout output is informational
			// (e.g. `go fmt` prints filenames it reformatted but already fixed
			// them). We don't treat this as lint findings.
			continue
		}

		// Command failed (non-zero exit) — collect output as lint findings.
		combined := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
		combined = strings.TrimSpace(combined)

		if combined == "" {
			// Command failed but produced no output — include the error itself.
			combined = fmt.Sprintf("%s: %v", cmdStr, err)
		}

		if results.Len() > 0 {
			results.WriteString("\n\n")
		}
		fmt.Fprintf(&results, "$ %s\n%s", cmdStr, combined)
	}

	return results.String(), nil
}
