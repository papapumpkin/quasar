// Package diff provides unified diff parsing, types, and statistics.
// It contains no rendering or UI logic.
package diff

import (
	"strconv"
	"strings"
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

// parseGitDiffPath extracts the file path from a "diff --git a/path b/path" line.
func parseGitDiffPath(line string) string {
	trimmed := strings.TrimPrefix(line, "diff --git ")
	if strings.HasPrefix(trimmed, "a/") {
		withoutA := trimmed[2:]
		pathLen := (len(withoutA) - 3) / 2
		if pathLen > 0 && pathLen < len(withoutA) {
			candidate := withoutA[:pathLen]
			expected := " b/" + candidate
			if len(withoutA) >= pathLen+len(expected) && withoutA[pathLen:pathLen+len(expected)] == expected {
				return candidate
			}
		}
		if idx := strings.LastIndex(withoutA, " b/"); idx >= 0 {
			return withoutA[idx+3:]
		}
	}
	return trimmed
}

// parseHunkHeader parses "@@ -oldStart[,oldCount] +newStart[,newCount] @@" into start lines.
func parseHunkHeader(line string) (oldStart, newStart int) {
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
func parseHunkContext(line string) string {
	idx := strings.Index(line[3:], " @@")
	if idx < 0 {
		return ""
	}
	after := line[3+idx+3:]
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
