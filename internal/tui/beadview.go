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

// View renders the bead hierarchy as a styled tree.
func (v BeadView) View() string {
	if !v.HasData {
		return styleDetailDim.Render("  (no bead data yet)")
	}

	var b strings.Builder

	// Root bead line.
	icon, iconStyle := beadStatusIcon(v.Root.Status)
	b.WriteString("  ")
	b.WriteString(iconStyle.Render(icon))
	b.WriteString(" ")
	b.WriteString(styleBeadID.Render(v.Root.ID))
	b.WriteString("  ")
	b.WriteString(styleBeadTitle.Render(v.Root.Title))
	b.WriteString("  ")
	b.WriteString(beadStatusLabel(v.Root.Status))
	b.WriteString("\n")

	if len(v.Root.Children) == 0 {
		b.WriteString(styleDetailDim.Render("  (no child issues)"))
		return b.String()
	}

	// Group children by cycle.
	groups := groupByCycle(v.Root.Children)
	for _, g := range groups {
		b.WriteString(renderCycleGroup(g))
	}

	// Per-cycle summary.
	b.WriteString("\n")
	b.WriteString(renderBeadSummary(v.Root.Children))

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

// renderCycleGroup renders a cycle header and its children.
func renderCycleGroup(g cycleGroup) string {
	var b strings.Builder

	header := fmt.Sprintf("  Cycle %d — %d %s", g.Cycle, len(g.Children), pluralIssue(len(g.Children)))
	b.WriteString(styleBeadCycleHeader.Render(header))
	b.WriteString("\n")

	for i, child := range g.Children {
		connector := "├── "
		if i == len(g.Children)-1 {
			connector = "└── "
		}

		icon, iconStyle := beadStatusIcon(child.Status)

		b.WriteString("  ")
		b.WriteString(styleTreeConnector.Render(connector))
		b.WriteString(iconStyle.Render(icon))
		b.WriteString(" ")
		b.WriteString(styleBeadID.Render(child.ID))
		b.WriteString("  ")
		b.WriteString(styleBeadTitle.Render(child.Title))
		b.WriteString("  ")
		b.WriteString(beadStatusLabel(child.Status))
		if child.Severity != "" {
			b.WriteString("  ")
			b.WriteString(beadSeverityTag(child.Severity))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// renderBeadSummary renders a summary of issue counts across cycles.
func renderBeadSummary(children []BeadInfo) string {
	total := len(children)
	closed := 0
	for _, c := range children {
		if c.Status == "closed" {
			closed++
		}
	}
	open := total - closed
	summary := fmt.Sprintf("  %d %s found, %d resolved, %d remaining",
		total, pluralIssue(total), closed, open)
	return styleBeadSummary.Render(summary)
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

// beadStatusLabel returns a styled status label.
func beadStatusLabel(status string) string {
	switch status {
	case "closed":
		return styleBeadClosed.Render("closed")
	case "in_progress":
		return styleBeadInProgress.Render("in_progress")
	default:
		return styleBeadOpen.Render("open")
	}
}

// beadSeverityTag returns a styled severity label.
func beadSeverityTag(severity string) string {
	switch severity {
	case "critical":
		return styleBeadSeverityCritical.Render("[critical]")
	case "major":
		return styleBeadSeverityMajor.Render("[major]")
	case "minor":
		return styleBeadSeverityMinor.Render("[minor]")
	default:
		return styleBeadSeverityMajor.Render("[" + severity + "]")
	}
}

// pluralIssue returns "issue" or "issues" based on count.
func pluralIssue(n int) string {
	if n == 1 {
		return "issue"
	}
	return "issues"
}
