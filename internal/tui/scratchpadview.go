package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// maxPhaseIDWidth is the column width reserved for the phase ID label in each
// scratchpad entry. Longer IDs are truncated with an ellipsis.
const maxPhaseIDWidth = 12

// ScratchpadView renders a scrollable viewport of timestamped telemetry entries.
// It consumes MsgScratchpadEntry messages from the telemetry bridge and
// auto-scrolls to the bottom unless the user has scrolled up.
type ScratchpadView struct {
	entries    []MsgScratchpadEntry
	viewport   viewport.Model
	width      int
	height     int
	totalLines int  // total content lines (for bottom detection)
	ready      bool // whether the viewport has been initialized with dimensions
}

// NewScratchpadView creates an empty scratchpad view.
func NewScratchpadView() ScratchpadView {
	return ScratchpadView{}
}

// SetSize updates the viewport dimensions and re-renders content.
func (sv *ScratchpadView) SetSize(width, height int) {
	sv.width = width
	sv.height = height
	if !sv.ready {
		sv.viewport = viewport.New(width, height)
		sv.ready = true
	} else {
		sv.viewport.Width = width
		sv.viewport.Height = height
	}
	sv.refreshContent()
}

// AddEntry appends a new entry and refreshes the viewport content.
// If the viewport was at the bottom before the addition, it auto-scrolls
// to keep the latest entry visible.
func (sv *ScratchpadView) AddEntry(entry MsgScratchpadEntry) {
	atBottom := sv.isAtBottom()
	sv.entries = append(sv.entries, entry)
	sv.refreshContent()
	if atBottom {
		sv.viewport.GotoBottom()
	}
}

// Update handles viewport scroll key events.
// Home/g and End/G are handled explicitly because the viewport's built-in
// KeyMap does not bind those keys.
func (sv *ScratchpadView) Update(msg tea.Msg) {
	if !sv.ready {
		return
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "home", "g":
			sv.viewport.GotoTop()
			return
		case "end", "G":
			sv.viewport.GotoBottom()
			return
		}
	}
	sv.viewport, _ = sv.viewport.Update(msg)
}

// View renders the scratchpad viewport or an empty placeholder.
func (sv ScratchpadView) View() string {
	if len(sv.entries) == 0 {
		return lipgloss.NewStyle().
			Foreground(colorMuted).
			PaddingLeft(2).
			Render("No events yet")
	}
	if !sv.ready {
		return ""
	}
	return sv.viewport.View()
}

// isAtBottom returns true if the viewport is scrolled to (or past) the bottom,
// or if the content fits entirely within the viewport.
func (sv *ScratchpadView) isAtBottom() bool {
	if !sv.ready || sv.viewport.Height <= 0 {
		return true
	}
	maxOffset := sv.totalLines - sv.viewport.Height
	if maxOffset <= 0 {
		return true
	}
	return sv.viewport.YOffset >= maxOffset
}

// refreshContent re-renders all entries into the viewport. It preserves the
// current scroll position unless explicitly overridden by the caller.
func (sv *ScratchpadView) refreshContent() {
	if !sv.ready {
		return
	}
	content := sv.renderContent()
	sv.totalLines = strings.Count(content, "\n") + 1
	sv.viewport.SetContent(content)
}

// renderContent formats all entries into a single string for the viewport.
func (sv ScratchpadView) renderContent() string {
	if len(sv.entries) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, entry := range sv.entries {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(sv.formatEntry(entry))
	}
	return sb.String()
}

// formatEntry renders a single scratchpad entry with timestamp, phase ID, and
// wrapped text. The format is:
//
//	[12:34:05] phase-id  Note text here, potentially spanning
//	                     multiple lines with wrapping.
func (sv ScratchpadView) formatEntry(entry MsgScratchpadEntry) string {
	tsStyle := lipgloss.NewStyle().Foreground(colorMuted)
	textStyle := lipgloss.NewStyle().Foreground(colorWhite)

	// Timestamp column: [HH:MM:SS]
	ts := tsStyle.Render(fmt.Sprintf("[%s]", entry.Timestamp.Format("15:04:05")))

	// Phase ID column — use accent color for phase entries, muted for system.
	phaseID := entry.PhaseID
	if phaseID == "" {
		phaseID = "system"
	}
	if len(phaseID) > maxPhaseIDWidth {
		phaseID = phaseID[:maxPhaseIDWidth-1] + "…"
	}
	phaseID = fmt.Sprintf("%-*s", maxPhaseIDWidth, phaseID)

	var pidStyle lipgloss.Style
	if entry.PhaseID == "" {
		pidStyle = lipgloss.NewStyle().Foreground(colorMuted)
	} else {
		pidStyle = lipgloss.NewStyle().Foreground(colorAccent)
	}
	pid := pidStyle.Render(phaseID)

	// Prefix width for wrapping: "[HH:MM:SS] " + phaseID + "  " (visual chars).
	// [HH:MM:SS] = 10 chars, space = 1, phaseID = maxPhaseIDWidth, gap = 2.
	prefixWidth := 10 + 1 + maxPhaseIDWidth + 2

	// Wrap text to fit the available width.
	textWidth := sv.width - prefixWidth
	if textWidth < 20 {
		textWidth = 20
	}
	wrapped := wrapText(entry.Text, textWidth)
	lines := strings.Split(wrapped, "\n")

	var sb strings.Builder
	for i, line := range lines {
		if i == 0 {
			sb.WriteString(fmt.Sprintf("%s %s  %s", ts, pid, textStyle.Render(line)))
		} else {
			indent := strings.Repeat(" ", prefixWidth)
			sb.WriteString(fmt.Sprintf("\n%s%s", indent, textStyle.Render(line)))
		}
	}
	return sb.String()
}

// wrapText breaks text into lines of at most width characters, splitting on
// word boundaries where possible.
func wrapText(text string, width int) string {
	if width <= 0 || len(text) <= width {
		return text
	}

	var sb strings.Builder
	remaining := text

	for len(remaining) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}

		if len(remaining) <= width {
			sb.WriteString(remaining)
			break
		}

		// Find the last space within the width limit for word wrapping.
		breakAt := width
		if idx := strings.LastIndex(remaining[:width], " "); idx > 0 {
			breakAt = idx
		}

		sb.WriteString(remaining[:breakAt])
		remaining = strings.TrimLeft(remaining[breakAt:], " ")
	}
	return sb.String()
}
