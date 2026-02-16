package tui

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// DetailPanel wraps a viewport for scrollable content display.
type DetailPanel struct {
	viewport viewport.Model
	title    string
	ready    bool
}

// NewDetailPanel creates a detail panel with the given dimensions.
func NewDetailPanel(width, height int) DetailPanel {
	vp := viewport.New(width, height)
	vp.SetContent("")
	return DetailPanel{
		viewport: vp,
		ready:    true,
	}
}

// SetSize updates the viewport dimensions.
func (d *DetailPanel) SetSize(width, height int) {
	d.viewport.Width = width
	d.viewport.Height = height
}

// SetContent updates the displayed text and title.
func (d *DetailPanel) SetContent(title, content string) {
	d.title = title
	d.viewport.SetContent(content)
	d.viewport.GotoTop()
}

// Update handles viewport scroll messages.
func (d *DetailPanel) Update(msg tea.Msg) {
	d.viewport, _ = d.viewport.Update(msg)
}

// View renders the detail panel with a rounded border.
func (d DetailPanel) View() string {
	header := ""
	if d.title != "" {
		header = styleDetailTitle.Render(d.title) + "\n"
	}
	content := header + d.viewport.View()
	return styleDetailBorder.Render(content)
}
