package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Bead status icons.
const (
	beadIconOpen       = "●"
	beadIconInProgress = "◎"
	beadIconClosed     = "✓"
)

// BeadView renders a tree of beads (parent task + child issues) grouped by cycle.
type BeadView struct {
	Root    BeadInfo
	HasData bool
	Width   int
}

// NewBeadView creates an empty bead view.
func NewBeadView() BeadView {
	return BeadView{}
}

// SetRoot updates the bead hierarchy to display.
func (v *BeadView) SetRoot(root BeadInfo) {
	v.Root = root
	v.HasData = true
}

// View renders the bead hierarchy as a compact graph with progress bar and status icons.
func (v BeadView) View() string {
	if !v.HasData {
		return styleDetailDim.Render("  (no bead data yet)")
	}

	var b strings.Builder

	total := len(v.Root.Children)
	closed := 0
	for _, c := range v.Root.Children {
		if c.Status == "closed" {
			closed++
		}
	}

	// Title + progress fraction.
	b.WriteString("  ")
	b.WriteString(styleBeadTitle.Render(v.Root.Title))
	if total == 0 {
		b.WriteString("  ")
		b.WriteString(styleDetailDim.Render("(no child issues)"))
		return b.String()
	}
	b.WriteString("  ")
	progress := fmt.Sprintf("[%d/%d resolved]", closed, total)
	if closed == total {
		b.WriteString(styleBeadClosed.Render(progress))
	} else {
		b.WriteString(styleBeadOpen.Render(progress))
	}
	b.WriteString("\n")

	// Progress bar.
	barWidth := min(v.Width-4, 32)
	if barWidth < 8 {
		barWidth = 8
	}
	filled := (closed * barWidth) / total
	b.WriteString("  ")
	b.WriteString(styleBeadClosed.Render(strings.Repeat("█", filled)))
	b.WriteString(styleDetailDim.Render(strings.Repeat("░", barWidth-filled)))
	b.WriteString("\n\n")

	// Per-cycle compact rows.
	groups := groupByCycle(v.Root.Children)
	for _, g := range groups {
		b.WriteString(renderCompactCycle(g))
	}

	return b.String()
}

// cycleGroup holds children for a single cycle.
type cycleGroup struct {
	Cycle    int
	Children []BeadInfo
}

// groupByCycle organizes children by their Cycle field.
// Children with Cycle == 0 are grouped under cycle 1.
func groupByCycle(children []BeadInfo) []cycleGroup {
	m := make(map[int][]BeadInfo)
	var order []int
	for _, c := range children {
		cy := c.Cycle
		if cy <= 0 {
			cy = 1
		}
		if _, seen := m[cy]; !seen {
			order = append(order, cy)
		}
		m[cy] = append(m[cy], c)
	}
	groups := make([]cycleGroup, 0, len(order))
	for _, cy := range order {
		groups = append(groups, cycleGroup{Cycle: cy, Children: m[cy]})
	}
	return groups
}

// renderCompactCycle renders a single cycle as a compact label + status icons.
func renderCompactCycle(g cycleGroup) string {
	var b strings.Builder
	label := fmt.Sprintf("  Cycle %d  ", g.Cycle)
	b.WriteString(styleBeadCycleHeader.Render(label))
	for _, c := range g.Children {
		icon, iconStyle := beadStatusIcon(c.Status)
		b.WriteString(iconStyle.Render(icon))
	}
	b.WriteString("\n")
	return b.String()
}

// beadStatusIcon returns the icon and style for a bead status.
func beadStatusIcon(status string) (string, lipgloss.Style) {
	switch status {
	case "closed":
		return beadIconClosed, styleBeadClosed
	case "in_progress":
		return beadIconInProgress, styleBeadInProgress
	default:
		return beadIconOpen, styleBeadOpen
	}
}
