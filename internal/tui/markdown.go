package tui

import (
	"sync"

	"github.com/charmbracelet/glamour"
)

// mdRenderer caches a glamour renderer per word-wrap width so we don't
// recreate it on every frame. A sync.Map avoids locking on the hot path
// once a width has been seen.
var mdRenderers sync.Map // map[int]*glamour.TermRenderer

// getRenderer returns (or lazily creates) a glamour renderer for the
// given wrap width. Returns nil on creation error.
func getRenderer(width int) *glamour.TermRenderer {
	if v, ok := mdRenderers.Load(width); ok {
		return v.(*glamour.TermRenderer)
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil
	}
	mdRenderers.Store(width, r)
	return r
}

// RenderMarkdown renders markdown content with ANSI styling via glamour.
// Falls back to raw text if the renderer cannot be created or rendering
// fails (graceful degradation).
func RenderMarkdown(content string, width int) string {
	if width <= 0 {
		width = 80
	}
	r := getRenderer(width)
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
