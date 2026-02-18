package tui

import (
	"testing"
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
