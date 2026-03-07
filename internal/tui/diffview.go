package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// DiffLine represents a single line in a unified diff hunk.
type DiffLine struct {
	Type    DiffLineType
	Content string
	OldNum  int // 0 means no line number (e.g. added line has no old num)
	NewNum  int // 0 means no line number (e.g. removed line has no new num)
}

// DiffLineType classifies a line in a unified diff.
type DiffLineType int

const (
	// DiffLineContext is an unchanged context line.
	DiffLineContext DiffLineType = iota
	// DiffLineAdd is an added line.
	DiffLineAdd
	// DiffLineRemove is a removed line.
	DiffLineRemove
)

// DiffHunk is a contiguous range of changes within a file.
type DiffHunk struct {
	Header string // function/method context from @@ line (e.g. "func (l *Loop) Run")
	Lines  []DiffLine
}

// ChangeType classifies how a file was modified in a diff.
type ChangeType int

const (
	// ChangeModified indicates the file was modified.
	ChangeModified ChangeType = iota
	// ChangeAdded indicates the file was newly added.
	ChangeAdded
	// ChangeDeleted indicates the file was deleted.
	ChangeDeleted
	// ChangeRenamed indicates the file was renamed.
	ChangeRenamed
)

// ChangeTypeGlyph returns a single-character glyph for the change type.
func ChangeTypeGlyph(ct ChangeType) string {
	switch ct {
	case ChangeAdded:
		return "A"
	case ChangeDeleted:
		return "D"
	case ChangeRenamed:
		return "R"
	default:
		return "M"
	}
}

// FileDiff represents the diff for a single file.
type FileDiff struct {
	Path   string
	Change ChangeType
	Hunks  []DiffHunk
}

// DiffStat holds the summary statistics for a diff.
type DiffStat struct {
	FilesChanged int
	Insertions   int
	Deletions    int
	FileStats    []FileStatEntry
}

// FileStatEntry holds per-file change counts.
type FileStatEntry struct {
	Path      string
	Additions int
	Deletions int
	Change    ChangeType
}

// DiffRenderOpts controls how a diff is rendered.
type DiffRenderOpts struct {
	SideBySide     bool
	CollapsedHunks map[int]bool // hunk index → collapsed
}

// ParseUnifiedDiff parses a unified diff string into structured FileDiff slices.
func ParseUnifiedDiff(raw string) []FileDiff {
	if raw == "" {
		return nil
	}

	lines := strings.Split(raw, "\n")
	var files []FileDiff
	var current *FileDiff

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// Detect file header: "diff --git a/... b/..."
		if strings.HasPrefix(line, "diff --git ") {
			path := parseGitDiffPath(line)
			files = append(files, FileDiff{Path: path})
			current = &files[len(files)-1]
			continue
		}

		// Detect change type from header metadata lines.
		if current != nil {
			if strings.HasPrefix(line, "new file mode") {
				current.Change = ChangeAdded
				continue
			}
			if strings.HasPrefix(line, "deleted file mode") {
				current.Change = ChangeDeleted
				continue
			}
			if strings.HasPrefix(line, "rename from") || strings.HasPrefix(line, "rename to") {
				current.Change = ChangeRenamed
				continue
			}
		}

		// Skip index, ---, +++ and other metadata lines.
		if strings.HasPrefix(line, "index ") ||
			strings.HasPrefix(line, "--- ") ||
			strings.HasPrefix(line, "+++ ") ||
			strings.HasPrefix(line, "old mode") ||
			strings.HasPrefix(line, "new mode") ||
			strings.HasPrefix(line, "similarity index") ||
			strings.HasPrefix(line, "Binary files") {
			continue
		}

		// Parse hunk header: @@ -oldStart,oldCount +newStart,newCount @@ optional context
		if strings.HasPrefix(line, "@@ ") {
			if current == nil {
				continue
			}
			oldStart, newStart := parseHunkHeader(line)
			hunkCtx := parseHunkContext(line)
			hunk := DiffHunk{Header: hunkCtx}
			oldNum := oldStart
			newNum := newStart

			for i++; i < len(lines); i++ {
				hl := lines[i]
				if strings.HasPrefix(hl, "diff --git ") ||
					strings.HasPrefix(hl, "@@ ") {
					i-- // re-process this line
					break
				}
				if len(hl) == 0 {
					// Empty lines in diff context.
					hunk.Lines = append(hunk.Lines, DiffLine{
						Type:    DiffLineContext,
						Content: "",
						OldNum:  oldNum,
						NewNum:  newNum,
					})
					oldNum++
					newNum++
					continue
				}

				prefix := hl[0]
				content := hl[1:]
				switch prefix {
				case ' ':
					hunk.Lines = append(hunk.Lines, DiffLine{
						Type:    DiffLineContext,
						Content: content,
						OldNum:  oldNum,
						NewNum:  newNum,
					})
					oldNum++
					newNum++
				case '-':
					hunk.Lines = append(hunk.Lines, DiffLine{
						Type:    DiffLineRemove,
						Content: content,
						OldNum:  oldNum,
					})
					oldNum++
				case '+':
					hunk.Lines = append(hunk.Lines, DiffLine{
						Type:    DiffLineAdd,
						Content: content,
						NewNum:  newNum,
					})
					newNum++
				case '\\':
					// "\ No newline at end of file" — skip.
				default:
					// Treat unexpected lines as context.
					hunk.Lines = append(hunk.Lines, DiffLine{
						Type:    DiffLineContext,
						Content: hl,
						OldNum:  oldNum,
						NewNum:  newNum,
					})
					oldNum++
					newNum++
				}
			}

			current.Hunks = append(current.Hunks, hunk)
			continue
		}
	}

	return files
}

// parseGitDiffPath extracts the file path from a "diff --git a/path b/path" line.
// It uses the "a/" prefix to split, which is more reliable than " b/" for
// paths that might contain the literal string " b/" in them.
func parseGitDiffPath(line string) string {
	// Format: "diff --git a/path b/path"
	// Strip the "diff --git " prefix first.
	trimmed := strings.TrimPrefix(line, "diff --git ")
	// The line has the form "a/<path> b/<path>". The a/ path and b/ path are
	// identical for renames-free diffs. Split on " a/" prefix to extract the
	// path after "a/", then strip the duplicate " b/<path>" suffix.
	if strings.HasPrefix(trimmed, "a/") {
		// Remove the "a/" prefix.
		withoutA := trimmed[2:]
		// The remaining is "<path> b/<path>". The midpoint is where " b/"
		// appears such that the text after it equals the text before it.
		// We find the midpoint by checking length: total = 2*pathLen + 3 (" b/").
		pathLen := (len(withoutA) - 3) / 2
		if pathLen > 0 && pathLen < len(withoutA) {
			candidate := withoutA[:pathLen]
			expected := " b/" + candidate
			if len(withoutA) >= pathLen+len(expected) && withoutA[pathLen:pathLen+len(expected)] == expected {
				return candidate
			}
		}
		// Fallback: split on last " b/".
		if idx := strings.LastIndex(withoutA, " b/"); idx >= 0 {
			return withoutA[idx+3:]
		}
	}
	// Final fallback.
	return trimmed
}

// parseHunkHeader parses "@@ -oldStart[,oldCount] +newStart[,newCount] @@" into start lines.
func parseHunkHeader(line string) (oldStart, newStart int) {
	// Strip @@ markers.
	line = strings.TrimPrefix(line, "@@ ")
	idx := strings.Index(line, " @@")
	if idx >= 0 {
		line = line[:idx]
	}

	parts := strings.Fields(line)
	if len(parts) >= 2 {
		oldStart = parseRangeStart(parts[0])
		newStart = parseRangeStart(parts[1])
	}
	return
}

// parseHunkContext extracts the function/method context string from a hunk header.
// Input: "@@ -10,7 +10,12 @@ func Login(w http.ResponseWriter) {"
// Output: "func Login(w http.ResponseWriter) {"
func parseHunkContext(line string) string {
	// Find the closing @@ marker.
	idx := strings.Index(line[3:], " @@")
	if idx < 0 {
		return ""
	}
	after := line[3+idx+3:] // skip past " @@"
	ctx := strings.TrimSpace(after)
	return ctx
}

// parseRangeStart parses "-N,M" or "+N,M" returning N.
func parseRangeStart(s string) int {
	s = strings.TrimLeft(s, "-+")
	if idx := strings.Index(s, ","); idx >= 0 {
		s = s[:idx]
	}
	n, _ := strconv.Atoi(s)
	return n
}

// ComputeDiffStat computes summary statistics from parsed file diffs.
func ComputeDiffStat(files []FileDiff) DiffStat {
	stat := DiffStat{FilesChanged: len(files)}
	for _, f := range files {
		var adds, dels int
		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				switch l.Type {
				case DiffLineAdd:
					adds++
				case DiffLineRemove:
					dels++
				}
			}
		}
		stat.Insertions += adds
		stat.Deletions += dels
		stat.FileStats = append(stat.FileStats, FileStatEntry{
			Path:      f.Path,
			Additions: adds,
			Deletions: dels,
			Change:    f.Change,
		})
	}
	return stat
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
