package tui

import (
	"github.com/charmbracelet/glamour"
)

// newRenderer creates a glamour renderer for the given wrap width.
// Returns nil on creation error.
func newRenderer(width int) *glamour.TermRenderer {
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil
	}
	return r
}

// RenderMarkdown renders markdown content with ANSI styling via glamour.
// Falls back to raw text if the renderer cannot be created or rendering
// fails (graceful degradation).
func RenderMarkdown(content string, width int) string {
	if width <= 0 {
		width = 80
	}
	r := newRenderer(width)
	if r == nil {
		return content
	}
	rendered, err := r.Render(content)
	if err != nil {
		return content
	}
	// glamour appends a trailing newline; trim it so callers control spacing.
	if len(rendered) > 0 && rendered[len(rendered)-1] == '\n' {
		rendered = rendered[:len(rendered)-1]
	}
	return rendered
}
