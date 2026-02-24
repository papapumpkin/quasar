package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// BoardColumn represents a canonical workflow column on the board.
type BoardColumn int

const (
	ColQueued  BoardColumn = iota // PhaseWaiting — tasks waiting for DAG dependencies
	ColRunning                    // PhaseWorking — quasar actively coding/reviewing
	ColReview                     // PhaseGate — awaiting human gate decision
	ColBlocked                    // Phases blocked by missing entanglements, file conflicts, or discoveries
	ColDone                       // PhaseDone, PhaseSkipped
	ColFailed                     // PhaseFailed
	colCount                      // sentinel for iteration
)

// columnMeta holds display metadata for a board column.
type columnMeta struct {
	Label string
	Color lipgloss.Color
}

// columnDefs maps each BoardColumn to its label and color from the galactic palette.
var columnDefs = [colCount]columnMeta{
	ColQueued:  {Label: "Queued", Color: colorPrimary},
	ColRunning: {Label: "Running", Color: colorAccent},
	ColReview:  {Label: "Review", Color: colorBlueshift},
	ColBlocked: {Label: "Blocked", Color: colorDanger},
	ColDone:    {Label: "Done", Color: colorSuccess},
	ColFailed:  {Label: "Failed", Color: colorDanger},
}

// Wide-terminal threshold: show all 6 columns including Blocked.
const boardWidthFull = 140

// Medium-terminal threshold: merge Blocked into Queued.
const boardWidthMedium = 100

// BoardView renders phases as a columnar board where tasks flow left-to-right
// through canonical states: Queued → Running → Review → Blocked → Done → Failed.
type BoardView struct {
	Phases []PhaseEntry
	Cursor int
	Width  int
	Height int
}

// NewBoardView creates an empty board view.
func NewBoardView() BoardView {
	return BoardView{}
}

// SelectedPhase returns the phase entry at the cursor position.
func (bv BoardView) SelectedPhase() *PhaseEntry {
	buckets := bv.partition()
	cols := bv.visibleColumns()
	flat := bv.flatOrder(buckets, cols)
	if bv.Cursor < 0 || bv.Cursor >= len(flat) {
		return nil
	}
	return flat[bv.Cursor]
}

// MoveUp moves the cursor to the previous phase in the flat column-first order,
// which may cross column boundaries. Use MoveLeft/MoveRight to jump between columns.
func (bv *BoardView) MoveUp() {
	if bv.Cursor > 0 {
		bv.Cursor--
	}
}

// MoveDown moves the cursor to the next phase in the flat column-first order,
// which may cross column boundaries. Use MoveLeft/MoveRight to jump between columns.
func (bv *BoardView) MoveDown() {
	buckets := bv.partition()
	cols := bv.visibleColumns()
	flat := bv.flatOrder(buckets, cols)
	max := len(flat) - 1
	if max < 0 {
		max = 0
	}
	if bv.Cursor < max {
		bv.Cursor++
	}
}

// MoveLeft moves the cursor to the previous column (first item in it).
func (bv *BoardView) MoveLeft() {
	buckets := bv.partition()
	cols := bv.visibleColumns()
	flat := bv.flatOrder(buckets, cols)
	if len(flat) == 0 {
		return
	}

	// Find which column/row the cursor is currently in.
	curCol, _ := bv.cursorPosition(buckets, cols, flat)
	if curCol < 0 {
		return
	}

	// Search left for a non-empty column.
	colIdx := -1
	for ci := curCol - 1; ci >= 0; ci-- {
		if len(buckets[cols[ci]]) > 0 {
			colIdx = ci
			break
		}
	}
	if colIdx < 0 {
		return
	}

	// Move cursor to first item of the target column.
	bv.Cursor = bv.flatIndexOfColumn(buckets, cols, colIdx)
}

// MoveRight moves the cursor to the next column (first item in it).
func (bv *BoardView) MoveRight() {
	buckets := bv.partition()
	cols := bv.visibleColumns()
	flat := bv.flatOrder(buckets, cols)
	if len(flat) == 0 {
		return
	}

	curCol, _ := bv.cursorPosition(buckets, cols, flat)
	if curCol < 0 {
		return
	}

	// Search right for a non-empty column.
	colIdx := -1
	for ci := curCol + 1; ci < len(cols); ci++ {
		if len(buckets[cols[ci]]) > 0 {
			colIdx = ci
			break
		}
	}
	if colIdx < 0 {
		return
	}

	bv.Cursor = bv.flatIndexOfColumn(buckets, cols, colIdx)
}

// partition distributes phases into column buckets based on status.
// It is width-aware: at medium terminal widths where Scanning and Blocked
// columns are not visible, their entries are remapped into Queued so that
// no phases are silently dropped from the board.
func (bv BoardView) partition() [colCount][]int {
	var buckets [colCount][]int
	visible := bv.visibleColumns()
	visibleSet := make(map[BoardColumn]bool, len(visible))
	for _, c := range visible {
		visibleSet[c] = true
	}
	for i, p := range bv.Phases {
		col := statusToColumn(p)
		// At medium width, Scanning and Blocked columns are not visible.
		// Remap their entries into Queued so phases are never lost.
		if !visibleSet[col] {
			col = ColQueued
		}
		buckets[col] = append(buckets[col], i)
	}
	return buckets
}

// statusToColumn maps a PhaseEntry to its board column.
func statusToColumn(p PhaseEntry) BoardColumn {
	switch p.Status {
	case PhaseWorking:
		return ColRunning
	case PhaseDone, PhaseSkipped:
		return ColDone
	case PhaseFailed:
		return ColFailed
	case PhaseGate:
		return ColReview
	default: // PhaseWaiting
		if p.BlockedBy != "" {
			return ColBlocked
		}
		return ColQueued
	}
}

// visibleColumns returns the columns to render based on terminal width.
// On wide terminals (>= 140): all 7 columns.
// On medium terminals (100-139): merge Scanning→Queued, Blocked→Queued.
// Below 100: should fall back to table view (caller decides).
func (bv BoardView) visibleColumns() []BoardColumn {
	if bv.Width >= boardWidthFull {
		return []BoardColumn{ColQueued, ColScanning, ColRunning, ColReview, ColBlocked, ColDone, ColFailed}
	}
	// Medium: omit Scanning and Blocked (their entries stay in Queued).
	return []BoardColumn{ColQueued, ColRunning, ColReview, ColDone, ColFailed}
}

// ShouldFallback returns true when the terminal is too narrow for the board view
// and the caller should fall back to the existing table view.
func (bv BoardView) ShouldFallback() bool {
	return bv.Width > 0 && bv.Width < boardWidthMedium
}

// flatOrder returns pointers to phases in column-first order matching visible columns.
// It accepts pre-computed buckets and columns to avoid redundant partition() calls.
func (bv BoardView) flatOrder(buckets [colCount][]int, cols []BoardColumn) []*PhaseEntry {
	var flat []*PhaseEntry
	for _, col := range cols {
		for _, idx := range buckets[col] {
			flat = append(flat, &bv.Phases[idx])
		}
	}
	return flat
}

// cursorPosition finds the column index and row within that column for the current cursor.
func (bv BoardView) cursorPosition(buckets [colCount][]int, cols []BoardColumn, flat []*PhaseEntry) (colIdx, row int) {
	if bv.Cursor < 0 || bv.Cursor >= len(flat) {
		return -1, -1
	}
	offset := 0
	for ci, col := range cols {
		count := len(buckets[col])
		if bv.Cursor < offset+count {
			return ci, bv.Cursor - offset
		}
		offset += count
	}
	return -1, -1
}

// flatIndexOfColumn returns the flat index of the first item in the given column index.
func (bv BoardView) flatIndexOfColumn(buckets [colCount][]int, cols []BoardColumn, colIdx int) int {
	idx := 0
	for ci := 0; ci < colIdx; ci++ {
		idx += len(buckets[cols[ci]])
	}
	return idx
}

// View renders the columnar board.
func (bv BoardView) View() string {
	if len(bv.Phases) == 0 {
		return ""
	}

	buckets := bv.partition()
	cols := bv.visibleColumns()
	flat := bv.flatOrder(buckets, cols)

	colWidth := bv.columnWidth(len(cols))

	// Build each column's rendered string.
	var rendered []string
	flatIdx := 0
	for _, col := range cols {
		var sb strings.Builder
		meta := columnDefs[col]

		// Column header.
		headerStyle := lipgloss.NewStyle().
			Foreground(meta.Color).
			Bold(true).
			Width(colWidth).
			Align(lipgloss.Center)
		sb.WriteString(headerStyle.Render(meta.Label))
		sb.WriteString("\n")

		// Separator under header.
		sep := strings.Repeat("─", colWidth)
		sepStyle := lipgloss.NewStyle().Foreground(colorMuted)
		sb.WriteString(sepStyle.Render(sep))
		sb.WriteString("\n")

		// Phase entries in this column.
		entries := buckets[col]
		if len(entries) == 0 {
			emptyStyle := lipgloss.NewStyle().
				Foreground(colorMuted).
				Width(colWidth).
				Align(lipgloss.Center)
			sb.WriteString(emptyStyle.Render("·"))
			sb.WriteString("\n")
		}
		for _, phaseIdx := range entries {
			p := bv.Phases[phaseIdx]
			selected := flatIdx == bv.Cursor && flatIdx < len(flat)

			sb.WriteString(bv.renderBoardEntry(p, selected, colWidth))
			sb.WriteString("\n")
			flatIdx++
		}

		// Pad column to colWidth.
		colStyle := lipgloss.NewStyle().Width(colWidth)
		rendered = append(rendered, colStyle.Render(sb.String()))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

// columnWidth computes the width for each column given the number of visible columns.
func (bv BoardView) columnWidth(numCols int) int {
	if numCols <= 0 {
		return 20
	}
	// Leave 1-char gap between columns.
	w := (bv.Width - (numCols - 1)) / numCols
	if w < 12 {
		w = 12
	}
	if w > 30 {
		w = 30
	}
	return w
}

// renderBoardEntry renders a single phase card within a column.
func (bv BoardView) renderBoardEntry(p PhaseEntry, selected bool, colWidth int) string {
	icon, _ := phaseIconAndStyleStatic(p)
	titleWidth := colWidth - 4 // icon + space + padding
	if titleWidth < 4 {
		titleWidth = 4
	}
	title := TruncateWithEllipsis(p.Title, titleWidth)
	if title == "" {
		title = TruncateWithEllipsis(p.ID, titleWidth)
	}

	var line string
	if selected {
		indicator := styleSelectionIndicator.Render(selectionIndicator)
		styledTitle := styleRowSelected.Render(title)
		line = fmt.Sprintf("%s %s %s", indicator, icon, styledTitle)
	} else {
		line = fmt.Sprintf("  %s %s", icon, title)
	}

	return line
}

// phaseIconAndStyleStatic returns the status icon for a phase (package-level, no spinner).
func phaseIconAndStyleStatic(p PhaseEntry) (string, lipgloss.Style) {
	switch p.Status {
	case PhaseDone:
		return styleRowDone.Render(iconDone), styleRowDone
	case PhaseWorking:
		return styleRowWorking.Render(iconWorking), styleRowWorking
	case PhaseFailed:
		return styleRowFailed.Render(iconFailed), styleRowFailed
	case PhaseGate:
		return styleRowGate.Render(iconGate), styleRowGate
	case PhaseSkipped:
		return styleRowWaiting.Render(iconSkipped), styleRowWaiting
	default:
		return styleRowWaiting.Render(iconWaiting), styleRowWaiting
	}
}
