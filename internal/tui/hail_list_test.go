package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/ui"
)

// --- HailListOverlay creation tests ---

func TestNewHailListOverlay(t *testing.T) {
	t.Parallel()

	t.Run("creates list with cursor at zero", func(t *testing.T) {
		t.Parallel()
		hails := makeTestHails(3)
		list := NewHailListOverlay(hails)

		if list.Cursor != 0 {
			t.Errorf("expected Cursor 0, got %d", list.Cursor)
		}
		if len(list.Hails) != 3 {
			t.Errorf("expected 3 hails, got %d", len(list.Hails))
		}
	})

	t.Run("empty hails list", func(t *testing.T) {
		t.Parallel()
		list := NewHailListOverlay(nil)

		if len(list.Hails) != 0 {
			t.Errorf("expected 0 hails, got %d", len(list.Hails))
		}
		if list.Selected() != nil {
			t.Error("expected Selected to return nil for empty list")
		}
	})
}

// --- Navigation tests ---

func TestHailListNavigation(t *testing.T) {
	t.Parallel()

	t.Run("move down increments cursor", func(t *testing.T) {
		t.Parallel()
		list := NewHailListOverlay(makeTestHails(3))
		list.MoveDown()

		if list.Cursor != 1 {
			t.Errorf("expected Cursor 1, got %d", list.Cursor)
		}
	})

	t.Run("move down clamps at bottom", func(t *testing.T) {
		t.Parallel()
		list := NewHailListOverlay(makeTestHails(2))
		list.MoveDown()
		list.MoveDown()
		list.MoveDown()

		if list.Cursor != 1 {
			t.Errorf("expected Cursor 1, got %d", list.Cursor)
		}
	})

	t.Run("move up decrements cursor", func(t *testing.T) {
		t.Parallel()
		list := NewHailListOverlay(makeTestHails(3))
		list.Cursor = 2
		list.MoveUp()

		if list.Cursor != 1 {
			t.Errorf("expected Cursor 1, got %d", list.Cursor)
		}
	})

	t.Run("move up clamps at top", func(t *testing.T) {
		t.Parallel()
		list := NewHailListOverlay(makeTestHails(3))
		list.MoveUp()

		if list.Cursor != 0 {
			t.Errorf("expected Cursor 0, got %d", list.Cursor)
		}
	})
}

// --- Selected tests ---

func TestHailListSelected(t *testing.T) {
	t.Parallel()

	t.Run("returns current hail", func(t *testing.T) {
		t.Parallel()
		hails := makeTestHails(3)
		list := NewHailListOverlay(hails)
		list.Cursor = 1

		selected := list.Selected()
		if selected == nil {
			t.Fatal("expected non-nil Selected")
		}
		if selected.ID != "hail-2" {
			t.Errorf("expected ID %q, got %q", "hail-2", selected.ID)
		}
	})

	t.Run("returns nil for empty list", func(t *testing.T) {
		t.Parallel()
		list := NewHailListOverlay(nil)

		if list.Selected() != nil {
			t.Error("expected nil for empty list")
		}
	})
}

// --- View rendering tests ---

func TestHailListView(t *testing.T) {
	t.Parallel()

	t.Run("contains HAILS header with count", func(t *testing.T) {
		t.Parallel()
		list := NewHailListOverlay(makeTestHails(3))
		view := list.View(80, 24)

		if !strings.Contains(view, "HAILS") {
			t.Error("expected view to contain 'HAILS' header")
		}
		if !strings.Contains(view, "3 pending") {
			t.Error("expected view to contain '3 pending'")
		}
	})

	t.Run("contains hail summaries", func(t *testing.T) {
		t.Parallel()
		list := NewHailListOverlay(makeTestHails(2))
		view := list.View(80, 24)

		if !strings.Contains(view, "Test hail 1") {
			t.Error("expected view to contain first hail summary")
		}
		if !strings.Contains(view, "Test hail 2") {
			t.Error("expected view to contain second hail summary")
		}
	})

	t.Run("contains kind badges", func(t *testing.T) {
		t.Parallel()
		list := NewHailListOverlay(makeTestHails(2))
		view := list.View(80, 24)

		if !strings.Contains(view, "blocker") {
			t.Error("expected view to contain kind badge")
		}
	})

	t.Run("contains cursor indicator", func(t *testing.T) {
		t.Parallel()
		list := NewHailListOverlay(makeTestHails(2))
		view := list.View(80, 24)

		if !strings.Contains(view, "▸") {
			t.Error("expected view to contain cursor indicator '▸'")
		}
	})

	t.Run("contains footer hints", func(t *testing.T) {
		t.Parallel()
		list := NewHailListOverlay(makeTestHails(1))
		view := list.View(80, 24)

		if !strings.Contains(view, "navigate") {
			t.Error("expected view to contain footer hint")
		}
	})

	t.Run("empty list shows no pending message", func(t *testing.T) {
		t.Parallel()
		list := NewHailListOverlay(nil)
		view := list.View(80, 24)

		if !strings.Contains(view, "No pending hails") {
			t.Error("expected view to contain 'No pending hails'")
		}
	})

	t.Run("orange border (rounded)", func(t *testing.T) {
		t.Parallel()
		list := NewHailListOverlay(makeTestHails(1))
		view := list.View(80, 24)

		if !strings.Contains(view, "╭") {
			t.Error("expected rounded border top-left corner")
		}
		if !strings.Contains(view, "╯") {
			t.Error("expected rounded border bottom-right corner")
		}
	})

	t.Run("narrow terminal constrains width", func(t *testing.T) {
		t.Parallel()
		list := NewHailListOverlay(makeTestHails(1))
		view := list.View(40, 24)

		if !strings.Contains(view, "HAILS") {
			t.Error("expected list to render in narrow terminal")
		}
	})
}

// --- helpers ---

func makeTestHails(n int) []ui.HailInfo {
	kinds := []string{"blocker", "decision_needed", "ambiguity", "human_review"}
	roles := []string{"coder", "reviewer"}

	hails := make([]ui.HailInfo, n)
	for i := range hails {
		hails[i] = ui.HailInfo{
			ID:         fmt.Sprintf("hail-%d", i+1),
			Kind:       kinds[i%len(kinds)],
			Cycle:      i + 1,
			SourceRole: roles[i%len(roles)],
			Summary:    fmt.Sprintf("Test hail %d", i+1),
			Detail:     fmt.Sprintf("Detail for hail %d", i+1),
		}
	}
	return hails
}
