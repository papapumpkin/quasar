package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HomeFilter selects which nebulas are shown on the home landing page.
type HomeFilter int

const (
	HomeFilterAll        HomeFilter = iota // show all nebulas
	HomeFilterReady                        // show ready and in-progress
	HomeFilterInProgress                   // show in-progress only
	HomeFilterDone                         // show done only
	homeFilterCount                        // sentinel for cycling
)

// String returns a human-readable label for the filter.
func (f HomeFilter) String() string {
	switch f {
	case HomeFilterReady:
		return "active"
	case HomeFilterInProgress:
		return "in progress"
	case HomeFilterDone:
		return "done"
	default:
		return "all"
	}
}

// Next returns the next filter in the cycle.
func (f HomeFilter) Next() HomeFilter {
	return (f + 1) % homeFilterCount
}

// Matches returns whether a nebula choice passes this filter.
func (f HomeFilter) Matches(nc NebulaChoice) bool {
	switch f {
	case HomeFilterReady:
		return nc.Status == "ready" || nc.Status == "in_progress"
	case HomeFilterInProgress:
		return nc.Status == "in_progress"
	case HomeFilterDone:
		return nc.Status == "done"
	default:
		return true
	}
}

// FilterNebulae returns the subset of choices that match the filter.
func (f HomeFilter) FilterNebulae(all []NebulaChoice) []NebulaChoice {
	if f == HomeFilterAll {
		return all
	}
	var out []NebulaChoice
	for _, nc := range all {
		if f.Matches(nc) {
			out = append(out, nc)
		}
	}
	return out
}

// HomeView renders the landing page list of discovered nebulas.
type HomeView struct {
	Nebulae []NebulaChoice
	Cursor  int
	Offset  int // first visible item index for viewport scrolling
	Width   int
	Height  int        // available lines for the list (0 = no constraint)
	Filter  HomeFilter // active filter
}

// View renders the home landing page with a scrollable list of nebulas.
// Each row shows an indicator, the nebula name, phase count, and status.
// An empty state is shown when no nebulas are found.
// When Height is set and items overflow, a viewport window follows the cursor.
func (hv HomeView) View() string {
	var b strings.Builder

	// Filter bar — always shown so the user knows which filter is active.
	b.WriteString(hv.renderFilterBar())
	b.WriteString("\n")

	if len(hv.Nebulae) == 0 {
		b.WriteString(hv.renderEmpty())
		return b.String()
	}

	// Adjust available height for the filter bar line.
	listHeight := hv.Height - 1
	if listHeight < 0 {
		listHeight = 0
	}

	// Check if all items fit without scrolling.
	if listHeight <= 0 || hv.totalLines() <= listHeight {
		for i, nc := range hv.Nebulae {
			b.WriteString(hv.renderNebulaRow(i, nc))
			b.WriteString("\n")
		}
		return b.String()
	}

	// Items overflow — render a windowed view with scroll indicators.
	listView := HomeView{
		Nebulae: hv.Nebulae,
		Cursor:  hv.Cursor,
		Offset:  hv.Offset,
		Width:   hv.Width,
		Height:  listHeight,
		Filter:  hv.Filter,
	}
	offset := listView.ensureCursorVisible()
	b.WriteString(listView.renderWindow(offset))
	return b.String()
}

// totalLines returns the total number of visible lines for all nebula rows.
func (hv HomeView) totalLines() int {
	n := 0
	for i := range hv.Nebulae {
		n += hv.rowHeight(i)
	}
	return n
}

// rowHeight returns the number of visible lines for the given nebula row.
func (hv HomeView) rowHeight(i int) int {
	if hv.Nebulae[i].Description != "" {
		return 2
	}
	return 1
}

// ensureCursorVisible returns an adjusted offset that guarantees the cursor
// row is visible within the available Height.
func (hv HomeView) ensureCursorVisible() int {
	offset := hv.Offset
	n := len(hv.Nebulae)
	if n == 0 {
		return 0
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= n {
		offset = n - 1
	}

	// Cursor above offset: snap to cursor.
	if hv.Cursor < offset {
		return hv.Cursor
	}

	// Cursor below visible window: increase offset until it fits.
	for offset < hv.Cursor {
		contentLines := hv.Height
		if offset > 0 {
			contentLines-- // reserve for "↑ more"
		}
		contentLines-- // reserve for "↓ more"

		lines := 0
		for i := offset; i <= hv.Cursor && i < n; i++ {
			lines += hv.rowHeight(i)
		}
		if lines <= contentLines {
			break
		}
		offset++
	}

	return offset
}

// renderWindow renders the visible subset of rows with scroll indicators.
func (hv HomeView) renderWindow(offset int) string {
	var b strings.Builder
	n := len(hv.Nebulae)

	showUp := offset > 0
	if showUp {
		b.WriteString("  " + styleDetailDim.Render(fmt.Sprintf("↑ %d more", offset)) + "\n")
	}

	// Compute available content lines, reserving for indicators.
	contentLines := hv.Height
	if showUp {
		contentLines--
	}
	// Tentatively reserve 1 line for the down indicator.
	contentLines--

	usedLines := 0
	endIdx := offset
	for i := offset; i < n; i++ {
		rh := hv.rowHeight(i)
		if usedLines+rh > contentLines {
			break
		}
		usedLines += rh
		endIdx = i + 1
	}

	// If no down indicator is needed, reclaim the reserved line.
	if endIdx >= n {
		contentLines++
		// Re-check if we can fit one more row.
		for endIdx < n {
			rh := hv.rowHeight(endIdx)
			if usedLines+rh > contentLines {
				break
			}
			usedLines += rh
			endIdx++
		}
	}

	for i := offset; i < endIdx; i++ {
		b.WriteString(hv.renderNebulaRow(i, hv.Nebulae[i]))
		b.WriteString("\n")
	}

	if remaining := n - endIdx; remaining > 0 {
		b.WriteString("  " + styleDetailDim.Render(fmt.Sprintf("↓ %d more", remaining)) + "\n")
	}

	return b.String()
}

// renderFilterBar renders the filter chips (all / active / in progress / done).
func (hv HomeView) renderFilterBar() string {
	filters := []HomeFilter{HomeFilterAll, HomeFilterReady, HomeFilterInProgress, HomeFilterDone}
	var parts []string
	for _, f := range filters {
		label := f.String()
		if f == hv.Filter {
			parts = append(parts, styleRowSelected.Render("["+label+"]"))
		} else {
			parts = append(parts, styleDetailDim.Render(" "+label+" "))
		}
	}
	return "  " + strings.Join(parts, " ")
}

// renderEmpty renders the empty state when no nebulas are discovered.
func (hv HomeView) renderEmpty() string {
	if hv.Filter != HomeFilterAll {
		msg := fmt.Sprintf("No nebulas matching filter %q", hv.Filter.String())
		return "  " + styleDetailDim.Render(msg) + "\n"
	}
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
