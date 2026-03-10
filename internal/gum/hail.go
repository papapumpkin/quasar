package gum

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/papapumpkin/quasar/internal/ui"
)

// HailResolver handles hail resolution using gum interactive prompts.
// It replaces the BubbleTea hail overlay and hail list with gum choose/write
// subprocesses, providing auto-advance through multiple pending hails.
type HailResolver struct {
	gum *Gum
}

// NewHailResolver creates a hail resolver backed by the given gum instance.
func NewHailResolver(g *Gum) *HailResolver {
	return &HailResolver{gum: g}
}

// HailResolution holds the result of resolving a single hail.
type HailResolution struct {
	HailID     string
	Response   string
	Skipped    bool // true if the user pressed Esc/cancelled
}

// ResolveOne resolves a single hail using gum prompts. It:
// 1. Shows hail context via gum format
// 2. If the hail has options: gum choose
// 3. Otherwise: gum write for free-text
func (r *HailResolver) ResolveOne(ctx context.Context, hail ui.HailInfo) (HailResolution, error) {
	// Build and display context.
	var md strings.Builder
	md.WriteString(fmt.Sprintf("# ⚠ HAIL: %s\n\n", hail.Summary))

	if hail.Kind != "" {
		md.WriteString(fmt.Sprintf("**Kind:** %s\n", hail.Kind))
	}
	if hail.SourceRole != "" {
		md.WriteString(fmt.Sprintf("**From:** %s", hail.SourceRole))
		if hail.Cycle > 0 {
			md.WriteString(fmt.Sprintf(" · cycle %d", hail.Cycle))
		}
		md.WriteString("\n")
	}
	md.WriteString("\n")

	if hail.Detail != "" {
		md.WriteString(hail.Detail)
		md.WriteString("\n")
	}

	// Render context to stderr.
	formatted, err := r.gum.Format(ctx, md.String())
	if err == nil {
		fmt.Fprint(os.Stderr, formatted)
	} else {
		fmt.Fprint(os.Stderr, md.String())
	}

	// Collect response.
	var response string
	if len(hail.Options) > 0 {
		response, err = r.gum.Choose(ctx, "Choose a resolution:", hail.Options)
	} else {
		response, err = r.gum.Write(ctx, "Type your response (or Esc to skip)...")
	}

	if err != nil {
		// User cancelled (Ctrl+C or Esc) — treat as skip.
		var exitErr *exec.ExitError
		if isExitError(err, &exitErr) {
			return HailResolution{HailID: hail.ID, Skipped: true}, nil
		}
		return HailResolution{}, fmt.Errorf("gum hail resolve: %w", err)
	}

	return HailResolution{
		HailID:   hail.ID,
		Response: response,
	}, nil
}

// ResolveAll resolves multiple hails in sequence, auto-advancing through
// each one. If only one hail is pending, it goes straight to resolution.
// If multiple, it first presents a filter/choose to pick which to address.
func (r *HailResolver) ResolveAll(ctx context.Context, hails []ui.HailInfo) ([]HailResolution, error) {
	if len(hails) == 0 {
		return nil, nil
	}

	var results []HailResolution
	remaining := make([]ui.HailInfo, len(hails))
	copy(remaining, hails)

	for len(remaining) > 0 {
		var selected ui.HailInfo
		var selectedIdx int

		if len(remaining) == 1 {
			selected = remaining[0]
			selectedIdx = 0
		} else {
			// Present a filter to pick which hail to address.
			items := make([]string, len(remaining))
			for i, h := range remaining {
				items[i] = fmt.Sprintf("[%s] %s", h.Kind, h.Summary)
			}

			choice, err := r.gum.Filter(ctx, items)
			if err != nil {
				// User cancelled — return what we have so far.
				break
			}

			// Find the selected hail.
			found := false
			for i, item := range items {
				if item == choice {
					selected = remaining[i]
					selectedIdx = i
					found = true
					break
				}
			}
			if !found {
				break
			}
		}

		res, err := r.ResolveOne(ctx, selected)
		if err != nil {
			return results, err
		}
		results = append(results, res)

		// Remove the resolved hail from remaining.
		remaining = append(remaining[:selectedIdx], remaining[selectedIdx+1:]...)

		// If there are more hails and the user didn't skip, auto-advance.
		if res.Skipped || len(remaining) == 0 {
			break
		}

		// Ask if the user wants to continue to the next hail.
		if len(remaining) > 0 {
			more, err := r.gum.Confirm(ctx, fmt.Sprintf("%d more hail(s) pending. Continue?", len(remaining)))
			if err != nil || !more {
				break
			}
		}
	}

	return results, nil
}

// GumHailCmd builds an *exec.Cmd for resolving a single hail via gum.
// Designed for use with tea.ExecProcess to suspend/resume the TUI.
func GumHailCmd(ctx context.Context, gumPath string, hail ui.HailInfo) *exec.Cmd {
	var script strings.Builder

	// Show context.
	var md strings.Builder
	md.WriteString(fmt.Sprintf("# ⚠ HAIL: %s\n\n", hail.Summary))
	if hail.Kind != "" {
		md.WriteString(fmt.Sprintf("**Kind:** %s\n", hail.Kind))
	}
	if hail.SourceRole != "" {
		md.WriteString(fmt.Sprintf("**From:** %s", hail.SourceRole))
		if hail.Cycle > 0 {
			md.WriteString(fmt.Sprintf(" · cycle %d", hail.Cycle))
		}
		md.WriteString("\n")
	}
	if hail.Detail != "" {
		md.WriteString("\n")
		md.WriteString(hail.Detail)
	}

	escaped := strings.ReplaceAll(md.String(), "'", "'\\''")
	script.WriteString(fmt.Sprintf("echo '%s' | %s format --type markdown >&2\n", escaped, gumPath))

	if len(hail.Options) > 0 {
		script.WriteString(fmt.Sprintf("%s choose --header 'Choose a resolution:' --cursor.foreground '%s' --header.foreground '%s' --selected.foreground '%s'",
			gumPath, colorBlueshift, colorAccent, colorSuccess))
		for _, opt := range hail.Options {
			optEscaped := strings.ReplaceAll(opt, "'", "'\\''")
			script.WriteString(fmt.Sprintf(" '%s'", optEscaped))
		}
		script.WriteString("\n")
	} else {
		script.WriteString(fmt.Sprintf("%s write --placeholder 'Type your response...' --header.foreground '%s' --cursor.foreground '%s'\n",
			gumPath, colorAccent, colorBlueshift))
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", script.String())
	return cmd
}

// isExitError is a helper that checks whether an error is an exec.ExitError.
func isExitError(err error, target **exec.ExitError) bool {
	if err == nil {
		return false
	}
	var exitErr *exec.ExitError
	if ok := strings.Contains(err.Error(), "exit status"); ok {
		_ = exitErr
	}
	// Use type assertion instead of errors.As to keep dependency minimal.
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
		return true
	}
	return false
}
