package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HomeView renders the landing page list of discovered nebulas.
type HomeView struct {
	Nebulae []NebulaChoice
	Cursor  int
	Width   int
}

// View renders the home landing page with a scrollable list of nebulas.
// Each row shows an indicator, the nebula name, phase count, and status.
// An empty state is shown when no nebulas are found.
func (hv HomeView) View() string {
	if len(hv.Nebulae) == 0 {
		return hv.renderEmpty()
	}

	var b strings.Builder
	for i, nc := range hv.Nebulae {
		b.WriteString(hv.renderNebulaRow(i, nc))
		b.WriteString("\n")
	}
	return b.String()
}

// renderEmpty renders the empty state when no nebulas are discovered.
func (hv HomeView) renderEmpty() string {
	msg := "No nebulas found in .nebulas/"
	return "  " + styleDetailDim.Render(msg) + "\n"
}

// renderNebulaRow renders a single nebula row with selection indicator,
// name, phase count, status, and description.
func (hv HomeView) renderNebulaRow(i int, nc NebulaChoice) string {
	selected := i == hv.Cursor

	// Selection indicator — matches the pattern from nebulaview.go.
	indicator := "  "
	if selected {
		indicator = styleSelectionIndicator.Render(selectionIndicator) + " "
	}

	// Status icon and label with color coding.
	statusIcon, statusStyle := homeStatusIconAndStyle(nc.Status)

	// Nebula name — bold/bright when selected, white otherwise.
	nameWidth := 24
	if hv.Width < CompactWidth && hv.Width > 0 {
		nameWidth = hv.Width / 3
		if nameWidth < 8 {
			nameWidth = 8
		}
	}
	name := TruncateWithEllipsis(nc.Name, nameWidth)
	paddedName := fmt.Sprintf("%-*s", nameWidth, name)

	var styledName string
	if selected {
		styledName = styleRowSelected.Render(paddedName)
	} else {
		styledName = stylePhaseID.Render(paddedName)
	}

	// Phase count and status detail.
	phaseLabel := fmt.Sprintf("%d phases", nc.Phases)
	if nc.Phases == 1 {
		phaseLabel = "1 phase"
	}
	statusLabel := homeStatusLabel(nc)
	detail := phaseLabel + "  " + statusStyle.Render(statusIcon+" "+statusLabel)

	styledDetail := "  " + stylePhaseDetail.Render(detail)

	// First line: indicator + name + detail.
	line := fmt.Sprintf("%s%s%s", indicator, styledName, styledDetail)

	// Second line: description (indented, only if non-empty).
	if nc.Description != "" {
		descIndent := "    "
		maxDescWidth := hv.Width - len(descIndent) - 2
		if maxDescWidth < 10 {
			maxDescWidth = 40
		}
		desc := TruncateWithEllipsis(nc.Description, maxDescWidth)
		line += "\n" + descIndent + styleDetailDim.Render(desc)
	}

	return line
}

// homeStatusIconAndStyle returns the icon and style for a nebula status string.
func homeStatusIconAndStyle(status string) (string, lipgloss.Style) {
	switch status {
	case "done":
		return iconDone, styleRowDone
	case "in_progress":
		return iconWorking, styleRowWorking
	case "partial":
		return iconFailed, lipgloss.NewStyle().Foreground(colorAccent)
	default: // "ready"
		return iconWaiting, styleRowWaiting
	}
}

// homeStatusLabel returns a human-readable status label for a nebula choice.
func homeStatusLabel(nc NebulaChoice) string {
	switch nc.Status {
	case "done":
		return "done"
	case "in_progress":
		return fmt.Sprintf("%d/%d done", nc.Done, nc.Phases)
	case "partial":
		return fmt.Sprintf("%d/%d partial", nc.Done, nc.Phases)
	default:
		return "ready"
	}
}

// SelectedNebula returns the nebula choice at the cursor, or nil if the list is empty.
func (hv HomeView) SelectedNebula() *NebulaChoice {
	if hv.Cursor < 0 || hv.Cursor >= len(hv.Nebulae) {
		return nil
	}
	return &hv.Nebulae[hv.Cursor]
}

// MoveUp moves the cursor up by one position.
func (hv *HomeView) MoveUp() {
	if hv.Cursor > 0 {
		hv.Cursor--
	}
}

// MoveDown moves the cursor down by one position.
func (hv *HomeView) MoveDown() {
	max := len(hv.Nebulae) - 1
	if max < 0 {
		max = 0
	}
	if hv.Cursor < max {
		hv.Cursor++
	}
}
