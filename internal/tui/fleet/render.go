package fleet

import (
	"fmt"
	"strings"
)

// laneTitles labels the three lanes in display order.
var laneTitles = [3]string{"Awaiting Approval", "In Flight", "Recent"}

// minColWidth is the floor for a single lane column.
const minColWidth = 24

// selectGutter prefixes the selected card line; unselGutter prefixes the rest.
// Both are the same display width so card text stays column-aligned, and an
// inactive Selection renders every card with unselGutter — byte-identical to a
// cursorless render, which keeps the existing golden fixtures valid.
const (
	selectGutter = "▌ "
	unselGutter  = "  "
)

// noCard is the markCard sentinel meaning "no slot in this block is selected".
// headerCard means the repo header itself (e.g. a folded repo) is selected.
const (
	noCard     = -2
	headerCard = -1
)

// Selection identifies the highlighted slot for RenderFleet: the active lane,
// the index of the repo within the fleet, and the index of the card within that
// repo's active-lane list (headerCard marks the repo header, e.g. a folded
// repo). Active is false when nothing should be highlighted — an empty fleet or
// a non-fleet interaction mode — in which case no marker is drawn.
type Selection struct {
	Lane    int
	RepoIdx int
	CardIdx int
	Active  bool
}

// RenderFleet renders the fleet to a deterministic, ANSI-free string laid out as
// three repo-aligned columns, marking the selected slot per sel. It is a pure
// function of (fleet, width, sel) so it can be golden-tested. Repos are grouped
// row-wise so a repo's three lanes line up horizontally.
func RenderFleet(f Fleet, width int, sel Selection) string {
	col := colWidth(width)
	var b strings.Builder

	b.WriteString(headerRow(col))
	b.WriteByte('\n')
	b.WriteString(dividerRow(col))
	b.WriteByte('\n')

	if len(f.Repos) == 0 {
		b.WriteString("(no registered repos — run `quasar repo register <path>`)")
		return b.String()
	}

	for i, lane := range f.Repos {
		// mark returns the selected card index for a given lane column of this
		// repo, or noCard when the selection is elsewhere.
		mark := func(laneNum int) int {
			if sel.Active && sel.Lane == laneNum && sel.RepoIdx == i {
				return sel.CardIdx
			}
			return noCard
		}
		blocks := [3][]string{
			awaitingBlock(lane, col, mark(0)),
			inflightBlock(lane, col, mark(1)),
			recentBlock(lane, col, mark(2)),
		}
		b.WriteString(joinBlocks(blocks, col))
		if i < len(f.Repos)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// gutter returns the 2-column line prefix marking selection state.
func gutter(selected bool) string {
	if selected {
		return selectGutter
	}
	return unselGutter
}

// colWidth divides the terminal width across three columns plus separators.
func colWidth(width int) int {
	col := (width - 2*len(sep)) / 3
	if col < minColWidth {
		return minColWidth
	}
	return col
}

// sep separates adjacent lane columns.
const sep = " │ "

// headerRow renders the three lane titles.
func headerRow(col int) string {
	cells := []string{pad(laneTitles[0], col), pad(laneTitles[1], col), pad(laneTitles[2], col)}
	return strings.Join(cells, sep)
}

// dividerRow renders a horizontal rule under the header.
func dividerRow(col int) string {
	rule := strings.Repeat("─", col)
	return strings.Join([]string{rule, rule, rule}, sep)
}

// awaitingBlock renders a repo's awaiting-approval lane block. markCard is the
// selected card index within this block (noCard/headerCard otherwise).
func awaitingBlock(lane RepoLane, col, markCard int) []string {
	lines := []string{repoHeader(lane, col, markCard == headerCard)}
	if lane.Folded {
		return lines
	}
	if len(lane.AwaitingApproval) == 0 {
		return append(lines, pad("  (none)", col))
	}
	for i, c := range lane.AwaitingApproval {
		lines = append(lines, pad(gutter(i == markCard)+fmt.Sprintf("%s %s", c.SourceLabel, c.Title), col))
	}
	return lines
}

// inflightBlock renders a repo's in-flight lane block. The run's primary line
// carries the selection marker; its step sub-line stays indented.
func inflightBlock(lane RepoLane, col, markCard int) []string {
	lines := []string{repoHeader(lane, col, markCard == headerCard)}
	if lane.Folded {
		return lines
	}
	if len(lane.InFlight) == 0 {
		return append(lines, pad("  (idle)", col))
	}
	for i, r := range lane.InFlight {
		lines = append(lines, pad(gutter(i == markCard)+fmt.Sprintf("%s ▸ %s", shortRunID(r.RunID), r.ConstellationName), col))
		lines = append(lines, pad(fmt.Sprintf("    ↻ step %d/%d — %s", r.StepIndex, r.StepCount, runDetail(r)), col))
	}
	return lines
}

// recentBlock renders a repo's recent (terminal) lane block.
func recentBlock(lane RepoLane, col, markCard int) []string {
	lines := []string{repoHeader(lane, col, markCard == headerCard)}
	if lane.Folded {
		return lines
	}
	if len(lane.Recent) == 0 {
		return append(lines, pad("  (none)", col))
	}
	for i, c := range lane.Recent {
		lines = append(lines, pad(gutter(i == markCard)+fmt.Sprintf("%s %s%s", statusGlyph(c.Status), c.Title, prSuffix(c)), col))
	}
	return lines
}

// repoHeader renders a repo's group header, prefixed with a fold marker. When
// selected (a folded repo under the cursor), it carries the selection gutter so
// the operator can see which repo a fold/unfold will act on.
func repoHeader(lane RepoLane, col int, selected bool) string {
	marker := "▾"
	if lane.Folded {
		marker = "▸"
	}
	body := fmt.Sprintf("%s %s", marker, lane.DisplayName)
	if selected {
		return pad(selectGutter+body, col)
	}
	return pad(body, col)
}

// joinBlocks stacks three equal-width blocks side by side, padding shorter
// blocks with blank cells so columns stay aligned.
func joinBlocks(blocks [3][]string, col int) string {
	height := 0
	for _, blk := range blocks {
		if len(blk) > height {
			height = len(blk)
		}
	}
	blank := strings.Repeat(" ", col)
	var b strings.Builder
	for row := 0; row < height; row++ {
		cells := make([]string, 3)
		for c := 0; c < 3; c++ {
			if row < len(blocks[c]) {
				cells[c] = blocks[c][row]
			} else {
				cells[c] = blank
			}
		}
		b.WriteString(strings.Join(cells, sep))
		b.WriteByte('\n')
	}
	return b.String()
}

// runDetail describes a run's current activity for the in-flight lane.
func runDetail(r RunCard) string {
	if r.State == "paused" {
		return "paused"
	}
	if r.CurrentNode != "" {
		return r.CurrentNode + " running"
	}
	return r.State
}

// prSuffix renders a recent card's PR status, if any.
func prSuffix(c NebulaCard) string {
	if c.PRNumber == 0 || c.PRStatus == "" {
		return ""
	}
	return fmt.Sprintf(" (PR #%d %s)", c.PRNumber, c.PRStatus)
}

// statusGlyph maps a terminal status to a compact glyph.
func statusGlyph(status string) string {
	switch status {
	case "merged", "done", "shipped":
		return "✓"
	case "failed", "canceled", "rejected":
		return "✗"
	default:
		return "•"
	}
}

// shortRunID returns a stable short form of a run id for display.
func shortRunID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// pad truncates or right-pads s to exactly width runes.
func pad(s string, width int) string {
	r := []rune(s)
	if len(r) > width {
		if width <= 1 {
			return string(r[:width])
		}
		return string(r[:width-1]) + "…"
	}
	return s + strings.Repeat(" ", width-len(r))
}
