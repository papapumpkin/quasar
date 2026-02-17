package tui

import (
	"strings"
	"testing"
)

func TestFileListView_View_Empty(t *testing.T) {
	t.Parallel()
	v := NewFileListView(nil, 80, "", "", "")
	got := v.View()
	if !strings.Contains(got, "(no changes)") {
		t.Errorf("View() = %q, want to contain '(no changes)'", got)
	}
}

func TestFileListView_View_RendersFiles(t *testing.T) {
	t.Parallel()
	files := []FileStatEntry{
		{Path: "internal/tui/model.go", Additions: 42, Deletions: 15},
		{Path: "internal/tui/diffview.go", Additions: 8, Deletions: 3},
	}
	v := NewFileListView(files, 80, "abc", "def", "/repo")

	got := v.View()

	// First file should be selected (cursor at 0).
	if !strings.Contains(got, "▸") {
		t.Error("View() should contain cursor indicator ▸")
	}
	if !strings.Contains(got, "model.go") {
		t.Error("View() should contain model.go")
	}
	if !strings.Contains(got, "diffview.go") {
		t.Error("View() should contain diffview.go")
	}
	if !strings.Contains(got, "+42") {
		t.Error("View() should contain +42 additions")
	}
	if !strings.Contains(got, "-15") {
		t.Error("View() should contain -15 deletions")
	}
	if !strings.Contains(got, "+8") {
		t.Error("View() should contain +8 additions")
	}
	if !strings.Contains(got, "-3") {
		t.Error("View() should contain -3 deletions")
	}
}

func TestFileListView_MoveDown(t *testing.T) {
	t.Parallel()
	files := []FileStatEntry{
		{Path: "a.go", Additions: 1, Deletions: 0},
		{Path: "b.go", Additions: 2, Deletions: 1},
		{Path: "c.go", Additions: 3, Deletions: 2},
	}
	v := NewFileListView(files, 80, "", "", "")

	if v.Cursor != 0 {
		t.Fatalf("initial Cursor = %d, want 0", v.Cursor)
	}

	v.MoveDown()
	if v.Cursor != 1 {
		t.Errorf("after MoveDown Cursor = %d, want 1", v.Cursor)
	}

	v.MoveDown()
	if v.Cursor != 2 {
		t.Errorf("after 2x MoveDown Cursor = %d, want 2", v.Cursor)
	}

	// Wrap around.
	v.MoveDown()
	if v.Cursor != 0 {
		t.Errorf("after wrap MoveDown Cursor = %d, want 0", v.Cursor)
	}
}

func TestFileListView_MoveUp(t *testing.T) {
	t.Parallel()
	files := []FileStatEntry{
		{Path: "a.go", Additions: 1, Deletions: 0},
		{Path: "b.go", Additions: 2, Deletions: 1},
	}
	v := NewFileListView(files, 80, "", "", "")

	// Wrap from top.
	v.MoveUp()
	if v.Cursor != 1 {
		t.Errorf("after MoveUp from 0 Cursor = %d, want 1 (wrap)", v.Cursor)
	}

	v.MoveUp()
	if v.Cursor != 0 {
		t.Errorf("after 2x MoveUp Cursor = %d, want 0", v.Cursor)
	}
}

func TestFileListView_MoveUp_Empty(t *testing.T) {
	t.Parallel()
	v := NewFileListView(nil, 80, "", "", "")
	v.MoveUp() // should not panic
	if v.Cursor != 0 {
		t.Errorf("Cursor = %d, want 0 for empty list", v.Cursor)
	}
}

func TestFileListView_MoveDown_Empty(t *testing.T) {
	t.Parallel()
	v := NewFileListView(nil, 80, "", "", "")
	v.MoveDown() // should not panic
	if v.Cursor != 0 {
		t.Errorf("Cursor = %d, want 0 for empty list", v.Cursor)
	}
}

func TestFileListView_SelectedFile(t *testing.T) {
	t.Parallel()
	files := []FileStatEntry{
		{Path: "first.go", Additions: 10, Deletions: 5},
		{Path: "second.go", Additions: 20, Deletions: 15},
	}
	v := NewFileListView(files, 80, "", "", "")

	got := v.SelectedFile()
	if got.Path != "first.go" {
		t.Errorf("SelectedFile().Path = %q, want %q", got.Path, "first.go")
	}

	v.MoveDown()
	got = v.SelectedFile()
	if got.Path != "second.go" {
		t.Errorf("SelectedFile().Path = %q, want %q", got.Path, "second.go")
	}
}

func TestFileListView_SelectedFile_Empty(t *testing.T) {
	t.Parallel()
	v := NewFileListView(nil, 80, "", "", "")
	got := v.SelectedFile()
	if got.Path != "" {
		t.Errorf("SelectedFile().Path = %q, want empty for nil list", got.Path)
	}
}

func TestFileListView_CursorIndicatorMovesWithSelection(t *testing.T) {
	t.Parallel()
	files := []FileStatEntry{
		{Path: "a.go", Additions: 1, Deletions: 0},
		{Path: "b.go", Additions: 2, Deletions: 0},
	}
	v := NewFileListView(files, 80, "", "", "")

	// Initially cursor is on a.go.
	lines := strings.Split(v.View(), "\n")
	if len(lines) < 2 {
		t.Fatal("expected at least 2 lines in view")
	}
	if !strings.Contains(lines[0], "▸") {
		t.Error("cursor should be on first line initially")
	}
	if strings.Contains(lines[1], "▸") {
		t.Error("cursor should not be on second line initially")
	}

	// Move down — cursor should be on b.go.
	v.MoveDown()
	lines = strings.Split(v.View(), "\n")
	if strings.Contains(lines[0], "▸") {
		t.Error("cursor should not be on first line after MoveDown")
	}
	if !strings.Contains(lines[1], "▸") {
		t.Error("cursor should be on second line after MoveDown")
	}
}
