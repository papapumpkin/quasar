// Package gum provides a thin wrapper around the charmbracelet/gum CLI tool.
// It enables TUI-quality interactive prompts (choose, write, confirm, filter,
// pager, format) by shelling out to the gum binary. When gum is not installed,
// callers should fall back to built-in BubbleTea overlays.
package gum

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrNotFound is returned when the gum binary cannot be located in PATH.
var ErrNotFound = errors.New("gum binary not found in PATH")

// Quasar galactic palette — passed as gum styling flags.
const (
	colorBlueshift = "#7aa2f7"
	colorAccent    = "#ff9e64"
	colorSuccess   = "#9ece6a"
	colorDanger    = "#ff7b72"
	colorMuted     = "#7f8490"
)

// Gum wraps the gum CLI binary for interactive prompts.
type Gum struct {
	// BinPath is the resolved path to the gum binary.
	BinPath string
}

// New auto-detects the gum binary in PATH and returns a ready-to-use Gum
// instance. Returns ErrNotFound if gum is not installed.
func New() (*Gum, error) {
	path, err := exec.LookPath("gum")
	if err != nil {
		return nil, ErrNotFound
	}
	return &Gum{BinPath: path}, nil
}

// Available reports whether the gum binary was found.
func Available() bool {
	_, err := exec.LookPath("gum")
	return err == nil
}

// Validate checks that the gum binary exists and is executable.
func (g *Gum) Validate() error {
	if g.BinPath == "" {
		return ErrNotFound
	}
	_, err := os.Stat(g.BinPath)
	if err != nil {
		return fmt.Errorf("gum binary not accessible: %w", err)
	}
	return nil
}

// Choose presents an interactive choice menu and returns the selected option.
// The prompt is shown as a header above the choices.
func (g *Gum) Choose(ctx context.Context, prompt string, options []string) (string, error) {
	if len(options) == 0 {
		return "", fmt.Errorf("gum choose: no options provided")
	}
	args := []string{"choose",
		"--header", prompt,
		"--cursor.foreground", colorBlueshift,
		"--header.foreground", colorAccent,
		"--selected.foreground", colorSuccess,
	}
	args = append(args, options...)
	return g.runInteractive(ctx, args)
}

// Write presents a multi-line text editor and returns the entered text.
// The placeholder is shown as ghost text in the empty editor.
func (g *Gum) Write(ctx context.Context, placeholder string) (string, error) {
	args := []string{"write",
		"--placeholder", placeholder,
		"--header.foreground", colorAccent,
		"--cursor.foreground", colorBlueshift,
	}
	return g.runInteractive(ctx, args)
}

// Confirm presents a yes/no confirmation prompt. Returns true for yes.
func (g *Gum) Confirm(ctx context.Context, prompt string) (bool, error) {
	args := []string{"confirm", prompt,
		"--affirmative", "Yes",
		"--negative", "No",
		"--selected.foreground", colorSuccess,
	}
	_, err := g.runInteractive(ctx, args)
	if err != nil {
		// gum confirm exits 1 for "No" — distinguish from real errors.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Filter presents a fuzzy filter over the given items and returns the selection.
func (g *Gum) Filter(ctx context.Context, items []string) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("gum filter: no items provided")
	}
	args := []string{"filter",
		"--cursor.foreground", colorBlueshift,
		"--indicator.foreground", colorAccent,
		"--match.foreground", colorSuccess,
	}

	// Feed items via stdin.
	input := strings.Join(items, "\n")
	return g.runInteractiveWithStdin(ctx, args, input)
}

// Pager displays content in a scrollable pager.
func (g *Gum) Pager(ctx context.Context, content string) error {
	args := []string{"pager"}
	cmd := exec.CommandContext(ctx, g.BinPath, args...)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Format renders markdown content to the terminal via gum format.
func (g *Gum) Format(ctx context.Context, markdown string) (string, error) {
	args := []string{"format", "--type", "markdown"}
	return g.runCapture(ctx, args, markdown)
}

// runInteractive runs a gum command with stdin/stdout/stderr wired to the
// terminal for interactive use. Captures stdout for the result.
func (g *Gum) runInteractive(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, g.BinPath, args...)
	var stdout bytes.Buffer
	cmd.Stdin = os.Stdin
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

// runInteractiveWithStdin runs a gum command with custom stdin content piped
// in, but stderr wired to the terminal. Captures stdout for the result.
func (g *Gum) runInteractiveWithStdin(ctx context.Context, args []string, input string) (string, error) {
	cmd := exec.CommandContext(ctx, g.BinPath, args...)
	var stdout bytes.Buffer
	cmd.Stdin = strings.NewReader(input)
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

// runCapture runs a gum command with stdin piped in, capturing stdout.
// Neither stdin nor stdout is the terminal — used for non-interactive
// formatting commands.
func (g *Gum) runCapture(ctx context.Context, args []string, input string) (string, error) {
	cmd := exec.CommandContext(ctx, g.BinPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdin = strings.NewReader(input)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gum %s: %w (stderr: %s)", args[0], err, stderr.String())
	}
	return stdout.String(), nil
}
