package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// PaneFocus tracks which pane currently has keyboard focus in the split layout.
type PaneFocus int

const (
	// PaneFocusSidebar means the sidebar phase list has focus.
	PaneFocusSidebar PaneFocus = iota
	// PaneFocusMain means the right-side main/detail area has focus.
	PaneFocusMain
)

// Sidebar renders a persistent vertical list of all phases with status icons.
// It replaces the need for the board view to show full phase names, letting
// the board become a compact visual status display.
type Sidebar struct {
	Phases []PhaseEntry
	Cursor int  // currently highlighted phase index
	Offset int  // scroll offset for phases that exceed viewport height
	Width  int  // available width for the sidebar
	Height int  // available height for the sidebar content area
	Focus  bool // whether the sidebar currently has keyboard focus
}

// NewSidebar creates an empty sidebar.
func NewSidebar() Sidebar {
	return Sidebar{}
}

// SelectedPhase returns the phase entry at the cursor position, or nil.
func (s Sidebar) SelectedPhase() *PhaseEntry {
	if s.Cursor < 0 || s.Cursor >= len(s.Phases) {
		return nil
	}
	return &s.Phases[s.Cursor]
}

// MoveUp moves the cursor up by one, clamping at 0.
func (s *Sidebar) MoveUp() {
	if s.Cursor > 0 {
		s.Cursor--
	}
	s.ensureVisible()
}

// MoveDown moves the cursor down by one, clamping at the last phase.
func (s *Sidebar) MoveDown() {
	max := len(s.Phases) - 1
	if max < 0 {
		max = 0
	}
	if s.Cursor < max {
		s.Cursor++
	}
	s.ensureVisible()
}

// ensureVisible adjusts the scroll offset so the cursor is within the viewport.
func (s *Sidebar) ensureVisible() {
	if s.Height <= 0 {
		return
	}
	if s.Cursor < s.Offset {
		s.Offset = s.Cursor
	}
	if s.Cursor >= s.Offset+s.Height {
		s.Offset = s.Cursor - s.Height + 1
	}
}

// SyncPhases updates the sidebar's phase list from the canonical source.
func (s *Sidebar) SyncPhases(phases []PhaseEntry) {
	s.Phases = phases
	// Clamp cursor if phases were removed.
	if max := len(s.Phases) - 1; max >= 0 {
		if s.Cursor > max {
			s.Cursor = max
		}
	} else {
		s.Cursor = 0
	}
	s.ensureVisible()
}

// phaseStatusIcon returns a status icon for a phase.
func phaseStatusIcon(status PhaseStatus) string {
	switch status {
	case PhaseDone:
		return iconDone
	case PhaseFailed:
		return iconFailed
	case PhaseWorking:
		return iconWorking
	case PhaseGate:
		return iconGate
	case PhaseSkipped:
		return iconSkipped
	default:
		return iconWaiting
	}
}

// phaseStatusStyle returns the lipgloss style for a phase status icon.
func phaseStatusStyle(status PhaseStatus) lipgloss.Style {
	switch status {
	case PhaseDone:
		return styleRowDone
	case PhaseFailed:
		return styleRowFailed
	case PhaseWorking:
		return styleRowWorking
	case PhaseGate:
		return styleRowGate
	case PhaseSkipped:
		return styleRowWaiting
	default:
		return styleRowWaiting
	}
}

// View renders the sidebar as a bordered panel with phase list.
func (s Sidebar) View() string {
	if s.Width < 4 || s.Height < 1 {
		return ""
	}

	// Title.
	titleStyle := lipgloss.NewStyle().
		Foreground(colorPrimary).
		Bold(true)
	title := titleStyle.Render("PHASES")

	// Inner width excludes border (2) and padding (2).
	innerWidth := s.Width - 4
	if innerWidth < 4 {
		innerWidth = 4
	}

	// Render visible phase rows.
	var rows []string
	rows = append(rows, title)
	rows = append(rows, "") // spacing after title

	if len(s.Phases) == 0 {
		empty := lipgloss.NewStyle().Foreground(colorMuted).Render("  (no phases)")
		rows = append(rows, empty)
	} else {
		// Available lines for phase entries (height minus title and spacing).
		visibleLines := s.Height - 2
		if visibleLines < 1 {
			visibleLines = 1
		}

		// Scroll indicator at top.
		if s.Offset > 0 {
			indicator := lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
				Render(fmt.Sprintf("  ↑ %d more", s.Offset))
			rows = append(rows, indicator)
			visibleLines--
		}

		// Reserve a line for bottom scroll indicator if needed.
		remaining := len(s.Phases) - s.Offset
		hasBottomIndicator := remaining > visibleLines
		if hasBottomIndicator {
			visibleLines-- // reserve one line for "↓ N more"
		}

		for i := s.Offset; i < len(s.Phases) && i < s.Offset+visibleLines; i++ {
			p := s.Phases[i]
			row := s.renderPhaseRow(p, i == s.Cursor, innerWidth)
			rows = append(rows, row)
		}

		// Bottom scroll indicator.
		if hasBottomIndicator {
			belowCount := len(s.Phases) - (s.Offset + visibleLines)
			indicator := lipgloss.NewStyle().Foreground(colorMuted).Italic(true).
				Render(fmt.Sprintf("  ↓ %d more", belowCount))
			rows = append(rows, indicator)
		}
	}

	content := strings.Join(rows, "\n")

	// Border color changes based on focus state.
	borderColor := colorMuted
	if s.Focus {
		borderColor = colorNebulaDeep
	}

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(s.Width - 2). // subtract border width
		Height(s.Height)

	return borderStyle.Render(content)
}

// renderPhaseRow renders a single phase entry in the sidebar.
func (s Sidebar) renderPhaseRow(p PhaseEntry, selected bool, maxWidth int) string {
	icon := phaseStatusIcon(p.Status)
	iconStr := phaseStatusStyle(p.Status).Render(icon)

	// Use title if available, otherwise ID.
	label := p.Title
	if label == "" {
		label = p.ID
	}

	// Reserve space: indicator (2) + icon (2) + spacing.
	labelWidth := maxWidth - 5
	if labelWidth < 4 {
		labelWidth = 4
	}
	label = TruncateWithEllipsis(label, labelWidth)

	var row string
	if selected {
		indicator := styleSelectionIndicator.Render(selectionIndicator)
		labelStr := styleRowSelected.Render(label)
		row = indicator + " " + iconStr + " " + labelStr
	} else {
		labelStr := styleRowNormal.Render(label)
		row = "  " + iconStr + " " + labelStr
	}

	// Pad and highlight selected row.
	if selected {
		visible := lipgloss.Width(row)
		if visible < maxWidth {
			row += strings.Repeat(" ", maxWidth-visible)
		}
		row = lipgloss.NewStyle().Background(colorSelectionBg).Render(row)
	}

	return row
}
