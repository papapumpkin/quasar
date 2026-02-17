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
	Lines []DiffLine
}

// FileDiff represents the diff for a single file.
type FileDiff struct {
	Path  string
	Hunks []DiffHunk
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

		// Skip index, ---, +++ lines.
		if strings.HasPrefix(line, "index ") ||
			strings.HasPrefix(line, "--- ") ||
			strings.HasPrefix(line, "+++ ") ||
			strings.HasPrefix(line, "new file mode") ||
			strings.HasPrefix(line, "deleted file mode") ||
			strings.HasPrefix(line, "old mode") ||
			strings.HasPrefix(line, "new mode") ||
			strings.HasPrefix(line, "similarity index") ||
			strings.HasPrefix(line, "rename from") ||
			strings.HasPrefix(line, "rename to") ||
			strings.HasPrefix(line, "Binary files") {
			continue
		}

		// Parse hunk header: @@ -oldStart,oldCount +newStart,newCount @@
		if strings.HasPrefix(line, "@@ ") {
			if current == nil {
				continue
			}
			oldStart, newStart := parseHunkHeader(line)
			hunk := DiffHunk{}
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

// renderFileDiff renders a single file's diff in side-by-side format.
func renderFileDiff(f FileDiff, width int) string {
	var b strings.Builder

	// File header.
	header := fmt.Sprintf("── %s ", f.Path)
	remaining := width - len(header)
	if remaining > 0 {
		header += strings.Repeat("─", remaining)
	}
	b.WriteString(styleDiffHeader.Render(header))
	b.WriteString("\n")

	// Line number width (4 chars is enough for most files).
	const numWidth = 4
	sep := styleDiffSep.Render(" │ ")
	sepLen := 3

	// Each side gets: numWidth + 1(space) + content.
	// Total: numWidth + 1 + content + sep + numWidth + 1 + content.
	sideWidth := (width - sepLen) / 2
	contentWidth := sideWidth - numWidth - 1
	if contentWidth < 10 {
		contentWidth = 10
	}

	for _, hunk := range f.Hunks {
		pairs := BuildSideBySidePairs(hunk)
		for _, pair := range pairs {
			left := renderSideLine(pair.Left, numWidth, contentWidth, true)
			right := renderSideLine(pair.Right, numWidth, contentWidth, false)
			b.WriteString(left)
			b.WriteString(sep)
			b.WriteString(right)
			b.WriteString("\n")
		}
	}

	return b.String()
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
