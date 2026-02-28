package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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

// --- AppModel integration: MsgHailReceived tracking ---

func TestAppModelMsgHailReceivedTracking(t *testing.T) {
	t.Parallel()

	t.Run("adds hail to PendingHails", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.DisableSplash()
		m.Width = 120
		m.Height = 40

		msg := MsgHailReceived{
			PhaseID: "api-phase",
			Hail: ui.HailInfo{
				ID:      "hail-001",
				Kind:    "decision_needed",
				Summary: "Which API style?",
			},
		}

		result, _ := m.Update(msg)
		updated := result.(AppModel)

		if len(updated.PendingHails) != 1 {
			t.Fatalf("expected 1 pending hail, got %d", len(updated.PendingHails))
		}
		if updated.PendingHails[0].ID != "hail-001" {
			t.Errorf("expected hail ID %q, got %q", "hail-001", updated.PendingHails[0].ID)
		}
		if updated.StatusBar.HailCount != 1 {
			t.Errorf("expected HailCount 1, got %d", updated.StatusBar.HailCount)
		}
	})

	t.Run("accumulates multiple hails", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.DisableSplash()
		m.Width = 120
		m.Height = 40

		for i, id := range []string{"h1", "h2", "h3"} {
			msg := MsgHailReceived{
				Hail: ui.HailInfo{
					ID:      id,
					Kind:    "ambiguity",
					Summary: "Question",
					Cycle:   i + 1,
				},
			}
			result, _ := m.Update(msg)
			m = result.(AppModel)
		}

		if len(m.PendingHails) != 3 {
			t.Fatalf("expected 3 pending hails, got %d", len(m.PendingHails))
		}
		if m.StatusBar.HailCount != 3 {
			t.Errorf("expected HailCount 3, got %d", m.StatusBar.HailCount)
		}
	})

	t.Run("critical hails counted separately", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.DisableSplash()
		m.Width = 120
		m.Height = 40

		msgs := []MsgHailReceived{
			{Hail: ui.HailInfo{ID: "h1", Kind: "blocker"}},
			{Hail: ui.HailInfo{ID: "h2", Kind: "decision_needed"}},
			{Hail: ui.HailInfo{ID: "h3", Kind: "blocker"}},
		}
		for _, msg := range msgs {
			result, _ := m.Update(msg)
			m = result.(AppModel)
		}

		if m.StatusBar.CriticalHailCount != 2 {
			t.Errorf("expected CriticalHailCount 2, got %d", m.StatusBar.CriticalHailCount)
		}
	})
}

// --- AppModel integration: MsgHailResolved removes hail ---

func TestAppModelMsgHailResolved(t *testing.T) {
	t.Parallel()

	t.Run("removes matching hail by ID", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.DisableSplash()
		m.Width = 120
		m.Height = 40

		// Add two hails.
		m.PendingHails = []ui.HailInfo{
			{ID: "h1", Kind: "ambiguity", Summary: "Q1"},
			{ID: "h2", Kind: "blocker", Summary: "Q2"},
		}
		m.syncHailBadge()

		// Resolve one.
		result, _ := m.Update(MsgHailResolved{ID: "h1", Resolution: "done"})
		updated := result.(AppModel)

		if len(updated.PendingHails) != 1 {
			t.Fatalf("expected 1 pending hail, got %d", len(updated.PendingHails))
		}
		if updated.PendingHails[0].ID != "h2" {
			t.Errorf("expected remaining hail %q, got %q", "h2", updated.PendingHails[0].ID)
		}
		if updated.StatusBar.HailCount != 1 {
			t.Errorf("expected HailCount 1, got %d", updated.StatusBar.HailCount)
		}
	})

	t.Run("resolving unknown ID is a no-op", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.DisableSplash()
		m.Width = 120
		m.Height = 40
		m.PendingHails = []ui.HailInfo{
			{ID: "h1", Kind: "ambiguity"},
		}
		m.syncHailBadge()

		result, _ := m.Update(MsgHailResolved{ID: "unknown"})
		updated := result.(AppModel)

		if len(updated.PendingHails) != 1 {
			t.Errorf("expected 1 pending hail, got %d", len(updated.PendingHails))
		}
	})
}

// --- AppModel integration: H key opens hail list ---

func TestAppModelHKeyOpensHailList(t *testing.T) {
	t.Parallel()

	t.Run("H opens hail list with multiple pending", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.DisableSplash()
		m.Width = 120
		m.Height = 40
		m.PendingHails = makeTestHails(3)
		m.syncHailBadge()

		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}})
		updated := result.(AppModel)

		if updated.HailList == nil {
			t.Fatal("expected HailList overlay to be set")
		}
		if len(updated.HailList.Hails) != 3 {
			t.Errorf("expected 3 hails in list, got %d", len(updated.HailList.Hails))
		}
	})

	t.Run("H with single hail shows toast instead of list", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.DisableSplash()
		m.Width = 120
		m.Height = 40
		m.PendingHails = makeTestHails(1)
		m.syncHailBadge()

		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}})
		updated := result.(AppModel)

		if updated.HailList != nil {
			t.Error("expected HailList to be nil for single hail")
		}
		if len(updated.PendingHails) != 0 {
			t.Errorf("expected 0 pending hails after acknowledgement, got %d", len(updated.PendingHails))
		}
	})

	t.Run("H with no pending hails is a no-op", func(t *testing.T) {
		t.Parallel()
		m := NewAppModel(ModeNebula)
		m.DisableSplash()
		m.Width = 120
		m.Height = 40

		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}})
		updated := result.(AppModel)

		if updated.HailList != nil {
			t.Error("expected HailList to be nil when no hails pending")
		}
	})
}

// --- AppModel integration: hail list key handling ---

func TestAppModelHailListKeyHandling(t *testing.T) {
	t.Parallel()

	t.Run("esc dismisses hail list", func(t *testing.T) {
		t.Parallel()
		m := makeModelWithHailList()

		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEscape})
		updated := result.(AppModel)

		if updated.HailList != nil {
			t.Error("expected HailList to be dismissed on Esc")
		}
	})

	t.Run("up/down navigates hail list", func(t *testing.T) {
		t.Parallel()
		m := makeModelWithHailList()

		// Move down.
		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
		updated := result.(AppModel)

		if updated.HailList.Cursor != 1 {
			t.Errorf("expected Cursor 1 after down, got %d", updated.HailList.Cursor)
		}

		// Move up.
		result, _ = updated.handleKey(tea.KeyMsg{Type: tea.KeyUp})
		updated = result.(AppModel)

		if updated.HailList.Cursor != 0 {
			t.Errorf("expected Cursor 0 after up, got %d", updated.HailList.Cursor)
		}
	})

	t.Run("enter acknowledges selected hail", func(t *testing.T) {
		t.Parallel()
		m := makeModelWithHailList()
		initialCount := len(m.PendingHails)

		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		updated := result.(AppModel)

		if updated.HailList != nil {
			t.Error("expected HailList to be dismissed after Enter")
		}
		if len(updated.PendingHails) != initialCount-1 {
			t.Errorf("expected %d pending hails, got %d", initialCount-1, len(updated.PendingHails))
		}
	})
}

// --- AppModel integration: view renders hail list ---

func TestAppModelViewWithHailList(t *testing.T) {
	t.Parallel()

	t.Run("view renders hail list overlay", func(t *testing.T) {
		t.Parallel()
		m := makeModelWithHailList()

		view := m.View()

		if !strings.Contains(view, "HAILS") {
			t.Error("expected View output to contain HAILS list header")
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

func makeModelWithHailList() AppModel {
	m := NewAppModel(ModeNebula)
	m.DisableSplash()
	m.Width = 120
	m.Height = 40
	m.PendingHails = makeTestHails(3)
	m.syncHailBadge()
	m.HailList = NewHailListOverlay(m.PendingHails)
	m.HailList.Width = m.Width
	return m
}
