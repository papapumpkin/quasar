package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestModeHomeConstant(t *testing.T) {
	t.Parallel()

	// ModeHome should be distinct from ModeLoop and ModeNebula.
	if ModeHome == ModeLoop {
		t.Error("ModeHome should not equal ModeLoop")
	}
	if ModeHome == ModeNebula {
		t.Error("ModeHome should not equal ModeNebula")
	}
}

func TestNewAppModel_ModeHome(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeHome)
	if m.Mode != ModeHome {
		t.Errorf("expected mode ModeHome, got %d", m.Mode)
	}
	if m.Splash == nil {
		t.Error("expected splash to be non-nil by default")
	}
	if m.PhaseLoops == nil {
		t.Error("expected PhaseLoops map to be initialized")
	}
}

func TestNewAppModel_ModeHome_HomeFields(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeHome)
	m.HomeDir = "/tmp/nebulae"
	m.HomeNebulae = []NebulaChoice{
		{Name: "Test", Description: "A test nebula", Path: "/tmp/nebulae/test", Status: "ready", Phases: 3},
	}
	m.HomeCursor = 0

	if m.HomeDir != "/tmp/nebulae" {
		t.Errorf("expected HomeDir '/tmp/nebulae', got %q", m.HomeDir)
	}
	if len(m.HomeNebulae) != 1 {
		t.Fatalf("expected 1 home nebula, got %d", len(m.HomeNebulae))
	}
	if m.HomeNebulae[0].Description != "A test nebula" {
		t.Errorf("expected description 'A test nebula', got %q", m.HomeNebulae[0].Description)
	}
	if m.HomeCursor != 0 {
		t.Errorf("expected HomeCursor 0, got %d", m.HomeCursor)
	}
}

func TestNewHomeProgram(t *testing.T) {
	t.Parallel()

	choices := []NebulaChoice{
		{Name: "Alpha", Description: "First", Path: "/tmp/.nebulae/alpha", Status: "ready", Phases: 2},
		{Name: "Beta", Description: "Second", Path: "/tmp/.nebulae/beta", Status: "done", Phases: 3, Done: 3},
	}

	p := NewHomeProgram("/tmp/.nebulae", choices, false)
	if p == nil {
		t.Fatal("expected non-nil program")
	}
}

func TestNewHomeProgram_NoSplash(t *testing.T) {
	t.Parallel()

	choices := []NebulaChoice{
		{Name: "Gamma", Description: "Third", Path: "/tmp/.nebulae/gamma", Status: "ready", Phases: 1},
	}

	p := NewHomeProgram("/tmp/.nebulae", choices, true)
	if p == nil {
		t.Fatal("expected non-nil program")
	}
}

func TestNewHomeProgram_EmptyChoices(t *testing.T) {
	t.Parallel()

	p := NewHomeProgram("/tmp/.nebulae", nil, false)
	if p == nil {
		t.Fatal("expected non-nil program even with no choices")
	}
}

// newHomeModel creates a home-mode AppModel with nebula choices for testing.
func newHomeModel(choices []NebulaChoice) *AppModel {
	m := NewAppModel(ModeHome)
	m.Splash = nil // skip splash for key-handling tests
	m.HomeNebulae = choices
	m.HomeCursor = 0
	m.Width = 120
	m.Height = 40
	return &m
}

// --- Home-mode key handling tests ---

func TestHomeKey_UpDown(t *testing.T) {
	t.Parallel()

	choices := []NebulaChoice{
		{Name: "Alpha", Path: "/a", Status: "ready", Phases: 2},
		{Name: "Beta", Path: "/b", Status: "done", Phases: 3, Done: 3},
		{Name: "Gamma", Path: "/c", Status: "ready", Phases: 1},
	}

	t.Run("down increments cursor", func(t *testing.T) {
		t.Parallel()
		m := newHomeModel(choices)
		if m.HomeCursor != 0 {
			t.Fatalf("expected initial cursor 0, got %d", m.HomeCursor)
		}

		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
		rm := result.(AppModel)
		if rm.HomeCursor != 1 {
			t.Errorf("expected cursor 1 after down, got %d", rm.HomeCursor)
		}
	})

	t.Run("up decrements cursor", func(t *testing.T) {
		t.Parallel()
		m := newHomeModel(choices)
		m.HomeCursor = 2

		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
		rm := result.(AppModel)
		if rm.HomeCursor != 1 {
			t.Errorf("expected cursor 1 after up, got %d", rm.HomeCursor)
		}
	})

	t.Run("j moves down", func(t *testing.T) {
		t.Parallel()
		m := newHomeModel(choices)

		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		rm := result.(AppModel)
		if rm.HomeCursor != 1 {
			t.Errorf("expected cursor 1 after j, got %d", rm.HomeCursor)
		}
	})

	t.Run("k moves up", func(t *testing.T) {
		t.Parallel()
		m := newHomeModel(choices)
		m.HomeCursor = 1

		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
		rm := result.(AppModel)
		if rm.HomeCursor != 0 {
			t.Errorf("expected cursor 0 after k, got %d", rm.HomeCursor)
		}
	})
}

func TestHomeKey_CursorClamped(t *testing.T) {
	t.Parallel()

	choices := []NebulaChoice{
		{Name: "Alpha", Path: "/a", Status: "ready", Phases: 2},
		{Name: "Beta", Path: "/b", Status: "done", Phases: 3},
	}

	t.Run("up clamps at zero", func(t *testing.T) {
		t.Parallel()
		m := newHomeModel(choices)
		m.HomeCursor = 0

		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
		rm := result.(AppModel)
		if rm.HomeCursor != 0 {
			t.Errorf("expected cursor 0 (clamped), got %d", rm.HomeCursor)
		}
	})

	t.Run("down clamps at max", func(t *testing.T) {
		t.Parallel()
		m := newHomeModel(choices)
		m.HomeCursor = 1 // already at last

		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
		rm := result.(AppModel)
		if rm.HomeCursor != 1 {
			t.Errorf("expected cursor 1 (clamped), got %d", rm.HomeCursor)
		}
	})
}

func TestHomeKey_EnterSelectsNebula(t *testing.T) {
	t.Parallel()

	choices := []NebulaChoice{
		{Name: "Alpha", Path: "/path/alpha", Status: "ready", Phases: 2},
		{Name: "Beta", Path: "/path/beta", Status: "ready", Phases: 3},
	}

	t.Run("enter launches plan preview", func(t *testing.T) {
		t.Parallel()
		m := newHomeModel(choices)
		m.HomeCursor = 1

		result, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		rm := result.(AppModel)

		// Enter should activate the plan preview, not immediately select.
		if !rm.ShowPlanPreview {
			t.Error("expected ShowPlanPreview to be true")
		}
		if rm.PlanPreview == nil {
			t.Error("expected PlanPreview to be non-nil")
		}
		// cmd should be non-nil (goroutine to compute plan).
		if cmd == nil {
			t.Fatal("expected non-nil cmd for plan computation")
		}
		// SelectedNebula and NextNebula should NOT be set yet.
		if rm.SelectedNebula != "" {
			t.Errorf("expected empty SelectedNebula before apply, got %q", rm.SelectedNebula)
		}
		if rm.NextNebula != "" {
			t.Errorf("expected empty NextNebula before apply, got %q", rm.NextNebula)
		}
	})

	t.Run("enter on empty list is no-op", func(t *testing.T) {
		t.Parallel()
		m := newHomeModel(nil)

		result, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		rm := result.(AppModel)

		if rm.SelectedNebula != "" {
			t.Errorf("expected empty SelectedNebula, got %q", rm.SelectedNebula)
		}
		if cmd != nil {
			t.Error("expected nil cmd for enter on empty list")
		}
	})
}

func TestHomeKey_QuitExits(t *testing.T) {
	t.Parallel()

	m := newHomeModel([]NebulaChoice{
		{Name: "Alpha", Path: "/a", Status: "ready", Phases: 1},
	})

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("expected non-nil cmd (tea.Quit) for q key")
	}
}

func TestHomeKey_InfoToggle(t *testing.T) {
	t.Parallel()

	choices := []NebulaChoice{
		{Name: "Alpha", Description: "First nebula", Path: "/a", Status: "ready", Phases: 2},
	}

	t.Run("i toggles ShowPlan off then on", func(t *testing.T) {
		t.Parallel()
		m := newHomeModel(choices)
		// ShowPlan is true by default in home mode.
		if !m.ShowPlan {
			t.Fatal("expected ShowPlan true by default in home mode")
		}

		// First press: toggle off.
		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
		rm := result.(AppModel)
		if rm.ShowPlan {
			t.Error("expected ShowPlan false after first toggle")
		}

		// Second press: toggle on.
		result2, _ := rm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
		rm2 := result2.(AppModel)
		if !rm2.ShowPlan {
			t.Error("expected ShowPlan true after second toggle")
		}
	})

	t.Run("? also toggles detail panel", func(t *testing.T) {
		t.Parallel()
		m := newHomeModel(choices)

		// Toggle off with ?.
		result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
		rm := result.(AppModel)
		if rm.ShowPlan {
			t.Error("expected ShowPlan false after ? toggle")
		}
	})
}

func TestHomeKey_DetailUpdatesOnCursorMove(t *testing.T) {
	t.Parallel()

	choices := []NebulaChoice{
		{Name: "Alpha", Description: "First", Path: "/a", Status: "ready", Phases: 2},
		{Name: "Beta", Description: "Second", Path: "/b", Status: "done", Phases: 4, Done: 4},
	}

	m := newHomeModel(choices)
	// Move down to Beta.
	result, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	rm := result.(AppModel)

	if rm.HomeCursor != 1 {
		t.Fatalf("expected cursor 1, got %d", rm.HomeCursor)
	}

	// The detail title should be the selected nebula's name.
	if rm.Detail.title != "Beta" {
		t.Errorf("expected detail title 'Beta', got %q", rm.Detail.title)
	}
}

func TestNewAppModel_ModeHome_ShowPlanDefault(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeHome)
	if !m.ShowPlan {
		t.Error("expected ShowPlan to be true by default in home mode")
	}
}

func TestNewAppModel_ModeLoop_ShowPlanDefault(t *testing.T) {
	t.Parallel()

	m := NewAppModel(ModeLoop)
	if m.ShowPlan {
		t.Error("expected ShowPlan to be false by default in loop mode")
	}
}

func TestShowDetailPanel_HomeMode(t *testing.T) {
	t.Parallel()

	t.Run("visible when ShowPlan true and nebulae present", func(t *testing.T) {
		t.Parallel()
		m := newHomeModel([]NebulaChoice{
			{Name: "Alpha", Path: "/a", Status: "ready", Phases: 1},
		})

		if !m.showDetailPanel() {
			t.Error("expected detail panel visible in home mode with ShowPlan=true")
		}
	})

	t.Run("hidden when ShowPlan false", func(t *testing.T) {
		t.Parallel()
		m := newHomeModel([]NebulaChoice{
			{Name: "Alpha", Path: "/a", Status: "ready", Phases: 1},
		})
		m.ShowPlan = false

		if m.showDetailPanel() {
			t.Error("expected detail panel hidden when ShowPlan=false")
		}
	})

	t.Run("hidden when no nebulae", func(t *testing.T) {
		t.Parallel()
		m := newHomeModel(nil)

		if m.showDetailPanel() {
			t.Error("expected detail panel hidden when no nebulae")
		}
	})
}
