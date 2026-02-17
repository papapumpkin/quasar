package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// FileListView is a lightweight navigable list of changed files, replacing the
// expensive side-by-side diff rendering in the TUI viewport.
type FileListView struct {
	Files   []FileStatEntry
	Cursor  int
	Width   int
	BaseRef string
	HeadRef string
	WorkDir string
}

// NewFileListView creates a FileListView from the given file stats.
func NewFileListView(files []FileStatEntry, width int, baseRef, headRef, workDir string) *FileListView {
	return &FileListView{
		Files:   files,
		Width:   width,
		BaseRef: baseRef,
		HeadRef: headRef,
		WorkDir: workDir,
	}
}

// View renders the file list as a short styled string with a cursor indicator.
func (v *FileListView) View() string {
	if len(v.Files) == 0 {
		return styleDetailDim.Render("(no changes)")
	}

	// Find the longest path for alignment.
	maxPath := 0
	for _, f := range v.Files {
		if len(f.Path) > maxPath {
			maxPath = len(f.Path)
		}
	}

	// Cap alignment width to avoid overflow.
	available := v.Width - 4 // account for indicator + padding
	if maxPath > available {
		maxPath = available
	}

	var b strings.Builder
	for i, f := range v.Files {
		indicator := "  "
		pathStyle := lipgloss.NewStyle()
		if i == v.Cursor {
			indicator = styleSelectionIndicator.Render("▸") + " "
			pathStyle = pathStyle.Bold(true)
		}

		path := f.Path
		if len(path) > available {
			path = "…" + path[len(path)-available+1:]
		}

		// Right-pad path for alignment.
		padded := path + strings.Repeat(" ", maxPath-len(path))

		add := styleDiffStatAdd.Render(fmt.Sprintf("+%d", f.Additions))
		del := styleDiffStatDel.Render(fmt.Sprintf("-%d", f.Deletions))
		stat := fmt.Sprintf("| %s %s", add, del)

		b.WriteString(indicator)
		b.WriteString(pathStyle.Render(padded))
		b.WriteString(" ")
		b.WriteString(stat)
		if i < len(v.Files)-1 {
			b.WriteString("\n")
		}
	}

	b.WriteString("\n\n")
	hint := lipgloss.NewStyle().Foreground(colorMuted)
	b.WriteString(hint.Render("  ↑↓ navigate"))

	return b.String()
}

// MoveUp moves the cursor up, wrapping at the top.
func (v *FileListView) MoveUp() {
	if len(v.Files) == 0 {
		return
	}
	v.Cursor--
	if v.Cursor < 0 {
		v.Cursor = len(v.Files) - 1
	}
}

// MoveDown moves the cursor down, wrapping at the bottom.
func (v *FileListView) MoveDown() {
	if len(v.Files) == 0 {
		return
	}
	v.Cursor++
	if v.Cursor >= len(v.Files) {
		v.Cursor = 0
	}
}

// SelectedFile returns the file at the current cursor position.
// Returns a zero-value FileStatEntry if the list is empty.
func (v *FileListView) SelectedFile() FileStatEntry {
	if len(v.Files) == 0 {
		return FileStatEntry{}
	}
	return v.Files[v.Cursor]
}
