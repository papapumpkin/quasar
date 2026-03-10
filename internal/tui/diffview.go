package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/papapumpkin/quasar/internal/diff"
)

// DiffLine represents a single line in a unified diff hunk.
type DiffLine = diff.DiffLine

// DiffLineType classifies a line in a unified diff.
type DiffLineType = diff.DiffLineType

// DiffHunk is a contiguous range of changes within a file.
type DiffHunk = diff.DiffHunk

// ChangeType classifies how a file was modified in a diff.
type ChangeType = diff.ChangeType

// FileDiff represents the diff for a single file.
type FileDiff = diff.FileDiff

// DiffStat holds the summary statistics for a diff.
type DiffStat = diff.DiffStat

// FileStatEntry holds per-file change counts.
type FileStatEntry = diff.FileStatEntry

// Re-export diff constants.
const (
	DiffLineContext = diff.DiffLineContext
	DiffLineAdd     = diff.DiffLineAdd
	DiffLineRemove  = diff.DiffLineRemove
	ChangeModified  = diff.ChangeModified
	ChangeAdded     = diff.ChangeAdded
	ChangeDeleted   = diff.ChangeDeleted
	ChangeRenamed   = diff.ChangeRenamed
)

// ParseUnifiedDiff parses a unified diff string into structured FileDiff slices.
func ParseUnifiedDiff(raw string) []FileDiff {
	return diff.ParseUnifiedDiff(raw)
}

// ComputeDiffStat computes summary statistics from parsed file diffs.
func ComputeDiffStat(files []FileDiff) DiffStat {
	return diff.ComputeDiffStat(files)
}

// ChangeTypeGlyph returns a single-character glyph for the change type.
func ChangeTypeGlyph(ct ChangeType) string {
	return diff.ChangeTypeGlyph(ct)
}

// DiffRenderOpts controls how a diff is rendered.
type DiffRenderOpts struct {
	SideBySide     bool
	CollapsedHunks map[int]bool // hunk index → collapsed
}

// SideBySidePair represents one row in the side-by-side view.
type SideBySidePair struct {
	Left  *DiffLine
	Right *DiffLine
}

// BuildSideBySidePairs converts a hunk into paired left/right lines.
// Removed lines appear on the left, added lines on the right.
// Context lines appear on both sides.
func BuildSideBySidePairs(hunk DiffHunk) []SideBySidePair {
	var pairs []SideBySidePair

	// Collect consecutive remove/add groups and pair them.
	lines := hunk.Lines
	i := 0
	for i < len(lines) {
		if lines[i].Type == DiffLineContext {
			l := lines[i]
			pairs = append(pairs, SideBySidePair{Left: &l, Right: &l})
			i++
			continue
		}

		// Collect consecutive removes.
		var removes []DiffLine
		for i < len(lines) && lines[i].Type == DiffLineRemove {
			removes = append(removes, lines[i])
			i++
		}
		// Collect consecutive adds.
		var adds []DiffLine
		for i < len(lines) && lines[i].Type == DiffLineAdd {
			adds = append(adds, lines[i])
			i++
		}

		// Pair removes with adds.
		maxLen := len(removes)
		if len(adds) > maxLen {
			maxLen = len(adds)
		}
		for j := 0; j < maxLen; j++ {
			pair := SideBySidePair{}
			if j < len(removes) {
				r := removes[j]
				pair.Left = &r
			}
			if j < len(adds) {
				a := adds[j]
				pair.Right = &a
			}
			pairs = append(pairs, pair)
		}
	}

	return pairs
}

// RenderDiffView renders a complete side-by-side diff view as a string.
// width is the available terminal width.
func RenderDiffView(raw string, width int) string {
	files := ParseUnifiedDiff(raw)
	if len(files) == 0 {
		return styleDiffContext.Render("(no diff available)")
	}

	stat := ComputeDiffStat(files)
	var b strings.Builder

	// Render stat summary.
	b.WriteString(renderDiffStat(stat))
	b.WriteString("\n\n")

	// Render each file.
	for i, f := range files {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(renderFileDiff(f, width))
	}

	return b.String()
}

// RenderSingleFileDiff parses a raw unified diff and renders the diff for a single file.
// Returns a placeholder message if the file is not found in the diff.
func RenderSingleFileDiff(raw string, path string, width int) string {
	files := ParseUnifiedDiff(raw)
	for _, f := range files {
		if f.Path == path {
			return renderFileDiff(f, width)
		}
	}
	return styleDiffContext.Render("(no diff for " + path + ")")
}

// renderDiffStat renders the stat summary block.
func renderDiffStat(stat DiffStat) string {
	var b strings.Builder

	// Summary line.
	summary := fmt.Sprintf("  %d file%s changed",
		stat.FilesChanged, pluralS(stat.FilesChanged))
	if stat.Insertions > 0 {
		summary += ", " + styleDiffStatAdd.Render(fmt.Sprintf("%d insertion%s(+)",
			stat.Insertions, pluralS(stat.Insertions)))
	}
	if stat.Deletions > 0 {
		summary += ", " + styleDiffStatDel.Render(fmt.Sprintf("%d deletion%s(-)",
			stat.Deletions, pluralS(stat.Deletions)))
	}
	b.WriteString(styleDiffStat.Render(summary))

	// Per-file stats.
	if len(stat.FileStats) > 0 {
		// Find longest path for alignment.
		maxPath := 0
		for _, fs := range stat.FileStats {
			if len(fs.Path) > maxPath {
				maxPath = len(fs.Path)
			}
		}
		for _, fs := range stat.FileStats {
			b.WriteString("\n")
			total := fs.Additions + fs.Deletions
			line := fmt.Sprintf("  %-*s | %3d ", maxPath, fs.Path, total)
			line += styleDiffStatAdd.Render(strings.Repeat("+", fs.Additions))
			line += styleDiffStatDel.Render(strings.Repeat("-", fs.Deletions))
			b.WriteString(styleDiffStat.Render(line))
		}
	}

	return b.String()
}

// pluralS returns "s" if n != 1.
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// RenderStatBar renders a proportional addition/deletion bar of the given width.
// For example, with 5 additions and 3 deletions at width 8: "+++++---"
func RenderStatBar(adds, dels, barWidth int) string {
	total := adds + dels
	if total == 0 || barWidth <= 0 {
		return ""
	}
	addCols := (adds * barWidth) / total
	delCols := barWidth - addCols
	// Ensure at least one column for non-zero counts.
	if adds > 0 && addCols == 0 {
		addCols = 1
		delCols = barWidth - 1
	}
	if dels > 0 && delCols == 0 {
		delCols = 1
		addCols = barWidth - 1
	}
	return styleDiffStatAdd.Render(strings.Repeat("+", addCols)) +
		styleDiffStatDel.Render(strings.Repeat("-", delCols))
}

// renderFileDiff renders a single file's diff in side-by-side format.
func renderFileDiff(f FileDiff, width int) string {
	return renderFileDiffWithOpts(f, width, DiffRenderOpts{SideBySide: true})
}

// renderFileDiffWithOpts renders a single file's diff with configurable options.
func renderFileDiffWithOpts(f FileDiff, width int, opts DiffRenderOpts) string {
	var b strings.Builder

	// File header.
	header := fmt.Sprintf("── %s ", f.Path)
	remaining := width - len(header)
	if remaining > 0 {
		header += strings.Repeat("─", remaining)
	}
	b.WriteString(styleDiffHeader.Render(header))
	b.WriteString("\n")

	for hi, hunk := range f.Hunks {
		// Render hunk context header if present.
		if hunk.Header != "" {
			ctx := styleDiffHunkCtx.Render("  @ " + hunk.Header)
			b.WriteString(ctx)
			b.WriteString("\n")
		}

		// Check if hunk is collapsed.
		if opts.CollapsedHunks != nil && opts.CollapsedHunks[hi] {
			adds, dels := countHunkChanges(hunk)
			summary := fmt.Sprintf("  ▸ %d lines (+%d -%d)", len(hunk.Lines), adds, dels)
			b.WriteString(styleDiffContext.Render(summary))
			b.WriteString("\n")
			continue
		}

		if opts.SideBySide {
			b.WriteString(renderHunkSideBySide(hunk, width))
		} else {
			b.WriteString(renderHunkUnified(hunk))
		}
	}

	return b.String()
}

// countHunkChanges counts additions and deletions in a hunk.
func countHunkChanges(hunk DiffHunk) (adds, dels int) {
	for _, l := range hunk.Lines {
		switch l.Type {
		case DiffLineAdd:
			adds++
		case DiffLineRemove:
			dels++
		}
	}
	return
}

// renderHunkSideBySide renders a hunk in side-by-side format.
func renderHunkSideBySide(hunk DiffHunk, width int) string {
	const numWidth = 4
	sep := styleDiffSep.Render(" │ ")
	sepLen := 3

	sideWidth := (width - sepLen) / 2
	contentWidth := sideWidth - numWidth - 1
	if contentWidth < 10 {
		contentWidth = 10
	}

	var b strings.Builder
	pairs := BuildSideBySidePairs(hunk)
	for _, pair := range pairs {
		left := renderSideLine(pair.Left, numWidth, contentWidth, true)
		right := renderSideLine(pair.Right, numWidth, contentWidth, false)
		b.WriteString(left)
		b.WriteString(sep)
		b.WriteString(right)
		b.WriteString("\n")
	}
	return b.String()
}

// renderHunkUnified renders a hunk in traditional unified diff format.
func renderHunkUnified(hunk DiffHunk) string {
	const numWidth = 4
	var b strings.Builder
	for _, line := range hunk.Lines {
		var prefix, numStr string
		switch line.Type {
		case DiffLineAdd:
			prefix = "+"
			if line.NewNum > 0 {
				numStr = fmt.Sprintf("%*d", numWidth, line.NewNum)
			}
		case DiffLineRemove:
			prefix = "-"
			if line.OldNum > 0 {
				numStr = fmt.Sprintf("%*d", numWidth, line.OldNum)
			}
		default:
			prefix = " "
			if line.NewNum > 0 {
				numStr = fmt.Sprintf("%*d", numWidth, line.NewNum)
			}
		}
		if numStr == "" {
			numStr = strings.Repeat(" ", numWidth)
		}
		rendered := styleDiffLineNum.Render(numStr) + " "
		content := prefix + line.Content
		switch line.Type {
		case DiffLineAdd:
			rendered += styleDiffAdd.Render(content)
		case DiffLineRemove:
			rendered += styleDiffRemove.Render(content)
		default:
			rendered += styleDiffContext.Render(content)
		}
		b.WriteString(rendered)
		b.WriteString("\n")
	}
	return b.String()
}

// RenderSingleFileDiffWithOpts parses a raw unified diff and renders the diff for a single file
// with the given render options (side-by-side, collapsed hunks).
func RenderSingleFileDiffWithOpts(raw string, path string, width int, opts DiffRenderOpts) string {
	files := ParseUnifiedDiff(raw)
	for _, f := range files {
		if f.Path == path {
			return renderFileDiffWithOpts(f, width, opts)
		}
	}
	return styleDiffContext.Render("(no diff for " + path + ")")
}

// HunkCount returns the number of hunks for the given file path in a raw diff.
func HunkCount(raw string, path string) int {
	files := ParseUnifiedDiff(raw)
	for _, f := range files {
		if f.Path == path {
			return len(f.Hunks)
		}
	}
	return 0
}

// renderSideLine renders one side (left or right) of a side-by-side diff row.
func renderSideLine(line *DiffLine, numWidth, contentWidth int, isLeft bool) string {
	if line == nil {
		// Empty side — pad with spaces.
		return strings.Repeat(" ", numWidth+1+contentWidth)
	}

	// Line number.
	var num int
	if isLeft {
		num = line.OldNum
	} else {
		num = line.NewNum
	}

	var numStr string
	if num > 0 {
		numStr = fmt.Sprintf("%*d", numWidth, num)
	} else {
		numStr = strings.Repeat(" ", numWidth)
	}
	numRendered := styleDiffLineNum.Render(numStr)

	// Content — truncate if too wide, using display width for correctness
	// with non-ASCII characters and tabs.
	content := line.Content
	w := lipgloss.Width(content)
	if w > contentWidth {
		// Truncate rune-by-rune until it fits.
		runes := []rune(content)
		for len(runes) > 0 && lipgloss.Width(string(runes))+1 > contentWidth {
			runes = runes[:len(runes)-1]
		}
		content = string(runes) + "…"
		w = lipgloss.Width(content)
	}
	// Pad content to fixed width using display width.
	if w < contentWidth {
		content = content + strings.Repeat(" ", contentWidth-w)
	}

	// Style based on line type.
	var styledContent string
	switch line.Type {
	case DiffLineAdd:
		styledContent = styleDiffAdd.Render(content)
	case DiffLineRemove:
		styledContent = styleDiffRemove.Render(content)
	default:
		styledContent = styleDiffContext.Render(content)
	}

	return numRendered + " " + styledContent
}
