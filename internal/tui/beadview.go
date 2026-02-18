package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Bead status icons.
const (
	beadIconOpen       = "●"
	beadIconInProgress = "◎"
	beadIconClosed     = "✓"
)

// Tree connector characters.
const (
	treeConnectorMid  = "├─"
	treeConnectorLast = "└─"
)

// BeadView renders a tree of beads (parent task + child issues) as a DAG with inline titles.
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

// treePrefixLen is the visual width of "  ├─ ✓ " (2 indent + 2 connector + 1 space + 1 icon + 1 space = 7).
const treePrefixLen = 7

// View renders the bead hierarchy as a tree with progress bar and inline titles.
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

	// Sort children by (Cycle, original index) for topological ordering.
	sorted := sortChildrenByCycle(v.Root.Children)

	// Render tree lines.
	for i, c := range sorted {
		connector := treeConnectorMid
		if i == len(sorted)-1 {
			connector = treeConnectorLast
		}
		icon, iconStyle := beadStatusIcon(c.Status)

		// Truncate title to fit available width.
		maxTitle := v.Width - treePrefixLen
		title := c.Title
		if maxTitle > 0 {
			title = TruncateWithEllipsis(title, maxTitle)
		}

		b.WriteString("  ")
		b.WriteString(connector)
		b.WriteString(" ")
		b.WriteString(iconStyle.Render(icon))
		b.WriteString(" ")
		b.WriteString(title)
		b.WriteString("\n")
	}

	return b.String()
}

// sortChildrenByCycle returns a copy of children sorted by ascending Cycle number,
// preserving original order within the same cycle. Children with Cycle <= 0 are
// treated as cycle 1.
func sortChildrenByCycle(children []BeadInfo) []BeadInfo {
	type indexed struct {
		bead  BeadInfo
		index int
	}
	items := make([]indexed, len(children))
	for i, c := range children {
		items[i] = indexed{bead: c, index: i}
	}
	sort.SliceStable(items, func(a, b int) bool {
		ca := items[a].bead.Cycle
		if ca <= 0 {
			ca = 1
		}
		cb := items[b].bead.Cycle
		if cb <= 0 {
			cb = 1
		}
		if ca != cb {
			return ca < cb
		}
		return items[a].index < items[b].index
	})
	result := make([]BeadInfo, len(items))
	for i, it := range items {
		result[i] = it.bead
	}
	return result
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
