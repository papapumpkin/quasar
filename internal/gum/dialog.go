package gum

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/papapumpkin/quasar/internal/dialog"
)

// DialogOpener implements dialog.Opener using gum subprocess calls.
// When a dialog is opened, it runs gum commands to display context and
// collect human input, rather than using BubbleTea overlays.
//
// Because gum is interactive, the caller must suspend the TUI (via
// tea.ExecProcess) before invoking Open. The GumDialogCmd helper
// handles this coordination.
type DialogOpener struct {
	gum *Gum
}

// NewDialogOpener creates a dialog opener backed by the given gum instance.
func NewDialogOpener(g *Gum) *DialogOpener {
	return &DialogOpener{gum: g}
}

// Open creates a dialog session, runs the gum interaction synchronously,
// and returns the session with the response already recorded. The caller
// should check session.Transcript() for the human's response.
func (d *DialogOpener) Open(ctx context.Context, req dialog.Request) (dialog.Session, error) {
	sess := dialog.NewSession(req)

	response, err := d.runDialog(ctx, req)
	if err != nil {
		sess.Close()
		return sess, fmt.Errorf("gum dialog: %w", err)
	}

	// Record the human response in the session.
	if response != "" {
		msg := dialog.Message{
			Role:    dialog.RoleHuman,
			Content: response,
		}
		select {
		case sess.FromHuman() <- msg:
		default:
		}
	}
	sess.Close()
	return sess, nil
}

// runDialog executes the gum interaction flow for a dialog request:
// 1. Show rendered context via gum format (if context provided)
// 2. If options: gum choose among them
// 3. Else: gum write for free-text input
func (d *DialogOpener) runDialog(ctx context.Context, req dialog.Request) (string, error) {
	// Display header.
	header := fmt.Sprintf("# DIALOG: %s", req.Title)
	if req.Kind != "" {
		header += fmt.Sprintf(" [%s]", req.Kind)
	}
	if req.PhaseID != "" {
		header += fmt.Sprintf("\n\n**Phase:** %s", req.PhaseID)
	}

	// Show context via gum format (non-interactive, writes to stdout).
	if req.Context != "" {
		fullContext := header + "\n\n---\n\n" + req.Context
		formatted, err := d.gum.Format(ctx, fullContext)
		if err == nil {
			fmt.Fprint(os.Stderr, formatted)
		} else {
			// Fallback: print raw context.
			fmt.Fprintln(os.Stderr, fullContext)
		}
	} else {
		formatted, err := d.gum.Format(ctx, header)
		if err == nil {
			fmt.Fprint(os.Stderr, formatted)
		} else {
			fmt.Fprintln(os.Stderr, header)
		}
	}

	// Collect response.
	if len(req.Options) > 0 {
		return d.gum.Choose(ctx, "Select an option:", req.Options)
	}
	return d.gum.Write(ctx, "Type your response...")
}

// GumDialogCmd builds an *exec.Cmd that runs the gum dialog flow in a
// subprocess-friendly way. This is designed to be used with
// tea.ExecProcess(cmd, callback) to suspend the TUI during interaction.
//
// The command runs a helper script that:
// 1. Displays context via gum format
// 2. Collects a response via gum choose or gum write
// 3. Writes the response to stdout (captured by the callback)
func GumDialogCmd(ctx context.Context, gumPath string, req dialog.Request) *exec.Cmd {
	// Build a shell script that sequences gum calls.
	var script strings.Builder

	// Show context.
	header := fmt.Sprintf("# DIALOG: %s", req.Title)
	if req.Kind != "" {
		header += fmt.Sprintf(" [%s]", req.Kind)
	}
	if req.PhaseID != "" {
		header += fmt.Sprintf("\nPhase: %s", req.PhaseID)
	}

	fullContent := header
	if req.Context != "" {
		fullContent += "\n\n---\n\n" + req.Context
	}

	// Escape single quotes for shell.
	escaped := strings.ReplaceAll(fullContent, "'", "'\\''")
	script.WriteString(fmt.Sprintf("echo '%s' | %s format --type markdown >&2\n", escaped, gumPath))

	if len(req.Options) > 0 {
		// Use gum choose.
		script.WriteString(fmt.Sprintf("%s choose --header 'Select an option:' --cursor.foreground '%s' --header.foreground '%s' --selected.foreground '%s'",
			gumPath, colorBlueshift, colorAccent, colorSuccess))
		for _, opt := range req.Options {
			optEscaped := strings.ReplaceAll(opt, "'", "'\\''")
			script.WriteString(fmt.Sprintf(" '%s'", optEscaped))
		}
		script.WriteString("\n")
	} else {
		// Use gum write.
		script.WriteString(fmt.Sprintf("%s write --placeholder 'Type your response...' --header.foreground '%s' --cursor.foreground '%s'\n",
			gumPath, colorAccent, colorBlueshift))
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", script.String())
	return cmd
}
