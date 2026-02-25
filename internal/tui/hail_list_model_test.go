package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/papapumpkin/quasar/internal/ui"
)

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

// makeModelWithHailList creates an AppModel with the hail list overlay open.
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
