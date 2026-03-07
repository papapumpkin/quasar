package tui

import (
	"strings"
	"testing"
)

func TestImpactViewEmpty(t *testing.T) {
	t.Parallel()
	iv := NewImpactView()
	iv.SetSize(80, 20)
	got := iv.View()
	if !strings.Contains(got, "No file impacts") {
		t.Errorf("expected empty placeholder, got: %s", got)
	}
}

func TestImpactViewRebuild(t *testing.T) {
	t.Parallel()
	iv := NewImpactView()
	iv.SetSize(100, 30)

	phaseFiles := map[string][]FileStatEntry{
		"phase-1": {
			{Path: "internal/foo.go", Additions: 10, Deletions: 2, Change: ChangeModified},
			{Path: "internal/bar.go", Additions: 5, Deletions: 0, Change: ChangeAdded},
		},
		"phase-2": {
			{Path: "internal/baz.go", Additions: 3, Deletions: 1, Change: ChangeModified},
		},
	}
	iv.Rebuild(phaseFiles)

	if iv.TotalFiles() != 3 {
		t.Errorf("TotalFiles() = %d, want 3", iv.TotalFiles())
	}
	if iv.OverlapCount() != 0 {
		t.Errorf("OverlapCount() = %d, want 0", iv.OverlapCount())
	}

	got := iv.View()
	if !strings.Contains(got, "phase-1") {
		t.Error("expected phase-1 in output")
	}
	if !strings.Contains(got, "phase-2") {
		t.Error("expected phase-2 in output")
	}
	if !strings.Contains(got, "foo.go") {
		t.Error("expected foo.go in output")
	}
}

func TestImpactViewOverlaps(t *testing.T) {
	t.Parallel()
	iv := NewImpactView()
	iv.SetSize(100, 30)

	phaseFiles := map[string][]FileStatEntry{
		"phase-1": {
			{Path: "shared.go", Additions: 10, Deletions: 2, Change: ChangeModified},
			{Path: "unique-1.go", Additions: 5, Deletions: 0, Change: ChangeAdded},
		},
		"phase-2": {
			{Path: "shared.go", Additions: 3, Deletions: 1, Change: ChangeModified},
			{Path: "unique-2.go", Additions: 1, Deletions: 0, Change: ChangeAdded},
		},
	}
	iv.Rebuild(phaseFiles)

	if iv.OverlapCount() != 1 {
		t.Errorf("OverlapCount() = %d, want 1", iv.OverlapCount())
	}
	if iv.TotalFiles() != 3 {
		t.Errorf("TotalFiles() = %d, want 3", iv.TotalFiles())
	}

	overlaps := iv.Overlaps()
	phases, ok := overlaps["shared.go"]
	if !ok {
		t.Fatal("expected shared.go in overlaps")
	}
	if len(phases) != 2 {
		t.Errorf("shared.go has %d phases, want 2", len(phases))
	}

	// Verify overlap warning in rendered output.
	got := iv.View()
	if !strings.Contains(got, "!!") {
		t.Error("expected overlap warning '!!' in output")
	}
	if !strings.Contains(got, "1 overlaps") {
		t.Error("expected '1 overlaps' in summary")
	}
}

func TestImpactViewMoveUpDown(t *testing.T) {
	t.Parallel()
	iv := NewImpactView()
	iv.SetSize(100, 30)

	phaseFiles := map[string][]FileStatEntry{
		"alpha": {{Path: "a.go", Change: ChangeModified}},
		"beta":  {{Path: "b.go", Change: ChangeAdded}},
		"gamma": {{Path: "c.go", Change: ChangeDeleted}},
	}
	iv.Rebuild(phaseFiles)

	if iv.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", iv.cursor)
	}

	iv.MoveDown()
	if iv.cursor != 1 {
		t.Errorf("after MoveDown: cursor = %d, want 1", iv.cursor)
	}

	iv.MoveDown()
	if iv.cursor != 2 {
		t.Errorf("after 2x MoveDown: cursor = %d, want 2", iv.cursor)
	}

	// Should not go past last group.
	iv.MoveDown()
	if iv.cursor != 2 {
		t.Errorf("after 3x MoveDown: cursor = %d, want 2 (clamped)", iv.cursor)
	}

	iv.MoveUp()
	if iv.cursor != 1 {
		t.Errorf("after MoveUp: cursor = %d, want 1", iv.cursor)
	}

	// Should not go below 0.
	iv.MoveUp()
	iv.MoveUp()
	if iv.cursor != 0 {
		t.Errorf("after excessive MoveUp: cursor = %d, want 0", iv.cursor)
	}
}

func TestImpactViewClampCursor(t *testing.T) {
	t.Parallel()
	iv := NewImpactView()
	iv.SetSize(80, 20)
	iv.cursor = 10 // out of bounds

	iv.ClampCursor()
	if iv.cursor != 0 {
		t.Errorf("ClampCursor on empty view: cursor = %d, want 0", iv.cursor)
	}

	phaseFiles := map[string][]FileStatEntry{
		"p1": {{Path: "a.go", Change: ChangeModified}},
		"p2": {{Path: "b.go", Change: ChangeAdded}},
	}
	iv.Rebuild(phaseFiles)
	iv.cursor = 10
	iv.ClampCursor()
	if iv.cursor != 1 {
		t.Errorf("ClampCursor with 2 groups: cursor = %d, want 1", iv.cursor)
	}
}

func TestFileScopeSection(t *testing.T) {
	t.Parallel()
	files := []FileStatEntry{
		{Path: "internal/foo.go", Additions: 10, Deletions: 2, Change: ChangeModified},
		{Path: "internal/bar.go", Additions: 5, Deletions: 0, Change: ChangeAdded},
	}
	got := FileScopeSection(files, 80)
	if !strings.Contains(got, "scope:") {
		t.Error("expected 'scope:' label")
	}
	if !strings.Contains(got, "foo.go") {
		t.Error("expected foo.go in scope section")
	}
	if !strings.Contains(got, "bar.go") {
		t.Error("expected bar.go in scope section")
	}
}

func TestFileScopeSectionEmpty(t *testing.T) {
	t.Parallel()
	got := FileScopeSection(nil, 80)
	if got != "" {
		t.Errorf("expected empty string for nil files, got: %q", got)
	}
}

func TestImpactTabRendersEmptyState(t *testing.T) {
	t.Parallel()
	m := nebulaModel()
	m.ActiveTab = TabImpact
	view := m.View()
	if !strings.Contains(view, "No file impacts") {
		t.Errorf("expected 'No file impacts' placeholder for impact tab, got:\n%s", view)
	}
}

func TestImpactViewAppendUnique(t *testing.T) {
	t.Parallel()

	t.Run("adds new element", func(t *testing.T) {
		t.Parallel()
		result := appendUnique([]string{"a", "b"}, "c")
		if len(result) != 3 {
			t.Errorf("expected 3 elements, got %d", len(result))
		}
	})

	t.Run("skips duplicate", func(t *testing.T) {
		t.Parallel()
		result := appendUnique([]string{"a", "b"}, "b")
		if len(result) != 2 {
			t.Errorf("expected 2 elements (no dup), got %d", len(result))
		}
	})

	t.Run("nil slice", func(t *testing.T) {
		t.Parallel()
		result := appendUnique(nil, "a")
		if len(result) != 1 {
			t.Errorf("expected 1 element, got %d", len(result))
		}
	})
}

func TestFormatFileStat(t *testing.T) {
	t.Parallel()

	t.Run("both additions and deletions", func(t *testing.T) {
		t.Parallel()
		got := formatFileStat(10, 3)
		if !strings.Contains(got, "+10") {
			t.Errorf("expected +10 in stat, got: %q", got)
		}
		if !strings.Contains(got, "-3") {
			t.Errorf("expected -3 in stat, got: %q", got)
		}
	})

	t.Run("zero stats", func(t *testing.T) {
		t.Parallel()
		got := formatFileStat(0, 0)
		if got != "" {
			t.Errorf("expected empty string for zero stats, got: %q", got)
		}
	})

	t.Run("additions only", func(t *testing.T) {
		t.Parallel()
		got := formatFileStat(5, 0)
		if !strings.Contains(got, "+5") {
			t.Errorf("expected +5, got: %q", got)
		}
	})
}

func TestChangeGlyphStyle(t *testing.T) {
	t.Parallel()
	// Just verify no panics for all change types.
	for _, ct := range []ChangeType{ChangeAdded, ChangeDeleted, ChangeRenamed, ChangeModified} {
		_ = changeGlyphStyle(ct)
	}
}
