package tui

import (
	"strings"
	"testing"
)

func TestHomeView_View_Empty(t *testing.T) {
	t.Parallel()

	hv := HomeView{Width: 80}
	out := hv.View()

	if !strings.Contains(out, "No nebulas found") {
		t.Errorf("expected empty state message, got:\n%s", out)
	}
}

func TestHomeView_View_SingleNebula(t *testing.T) {
	t.Parallel()

	hv := HomeView{
		Nebulae: []NebulaChoice{
			{Name: "alpha", Description: "First nebula", Path: "/tmp/alpha", Status: "ready", Phases: 3},
		},
		Cursor: 0,
		Width:  80,
	}
	out := hv.View()

	if !strings.Contains(out, "alpha") {
		t.Errorf("expected nebula name 'alpha' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "3 phases") {
		t.Errorf("expected '3 phases' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "ready") {
		t.Errorf("expected 'ready' status in output, got:\n%s", out)
	}
	if !strings.Contains(out, "First nebula") {
		t.Errorf("expected description 'First nebula' in output, got:\n%s", out)
	}
}

func TestHomeView_View_MultipleNebulae(t *testing.T) {
	t.Parallel()

	hv := HomeView{
		Nebulae: []NebulaChoice{
			{Name: "alpha", Description: "First", Path: "/tmp/alpha", Status: "ready", Phases: 3},
			{Name: "beta", Description: "Second", Path: "/tmp/beta", Status: "done", Phases: 5, Done: 5},
			{Name: "gamma", Description: "Third", Path: "/tmp/gamma", Status: "in_progress", Phases: 4, Done: 2},
		},
		Cursor: 1,
		Width:  80,
	}
	out := hv.View()

	if !strings.Contains(out, "alpha") {
		t.Errorf("expected 'alpha' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("expected 'beta' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "gamma") {
		t.Errorf("expected 'gamma' in output, got:\n%s", out)
	}
	// Cursor is on beta, so it should be highlighted with selection indicator.
	if !strings.Contains(out, selectionIndicator) {
		t.Errorf("expected selection indicator in output, got:\n%s", out)
	}
}

func TestHomeView_View_StatusColors(t *testing.T) {
	t.Parallel()

	statuses := []struct {
		status string
		label  string
	}{
		{"ready", "ready"},
		{"done", "done"},
		{"in_progress", "2/4 done"},
		{"partial", "1/4 partial"},
	}

	for _, tc := range statuses {
		t.Run(tc.status, func(t *testing.T) {
			t.Parallel()
			hv := HomeView{
				Nebulae: []NebulaChoice{
					{Name: "test", Status: tc.status, Phases: 4, Done: 2},
				},
				Cursor: 0,
				Width:  80,
			}
			// Adjust Done for partial case.
			if tc.status == "partial" {
				hv.Nebulae[0].Done = 1
			}
			out := hv.View()
			if !strings.Contains(out, tc.label) {
				t.Errorf("expected %q label for status %q, got:\n%s", tc.label, tc.status, out)
			}
		})
	}
}

func TestHomeView_View_SinglePhaseLabel(t *testing.T) {
	t.Parallel()

	hv := HomeView{
		Nebulae: []NebulaChoice{
			{Name: "single", Status: "ready", Phases: 1},
		},
		Cursor: 0,
		Width:  80,
	}
	out := hv.View()

	if !strings.Contains(out, "1 phase") {
		t.Errorf("expected '1 phase' (singular), got:\n%s", out)
	}
	// Should NOT contain "1 phases" (plural).
	if strings.Contains(out, "1 phases") {
		t.Errorf("should use singular 'phase', got:\n%s", out)
	}
}

func TestHomeView_View_NoDescription(t *testing.T) {
	t.Parallel()

	hv := HomeView{
		Nebulae: []NebulaChoice{
			{Name: "nodesc", Status: "ready", Phases: 2},
		},
		Cursor: 0,
		Width:  80,
	}
	out := hv.View()

	// The output should contain the name but the description line should be absent.
	if !strings.Contains(out, "nodesc") {
		t.Errorf("expected 'nodesc' in output, got:\n%s", out)
	}
	// Count lines: should be just the main row + trailing newline.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line (no description), got %d lines:\n%s", len(lines), out)
	}
}

func TestHomeView_SelectedNebula(t *testing.T) {
	t.Parallel()

	t.Run("empty list", func(t *testing.T) {
		t.Parallel()
		hv := HomeView{}
		if got := hv.SelectedNebula(); got != nil {
			t.Errorf("expected nil for empty list, got %+v", got)
		}
	})

	t.Run("valid cursor", func(t *testing.T) {
		t.Parallel()
		hv := HomeView{
			Nebulae: []NebulaChoice{
				{Name: "alpha"},
				{Name: "beta"},
			},
			Cursor: 1,
		}
		got := hv.SelectedNebula()
		if got == nil {
			t.Fatal("expected non-nil SelectedNebula")
		}
		if got.Name != "beta" {
			t.Errorf("expected 'beta', got %q", got.Name)
		}
	})
}

func TestHomeView_MoveUpDown(t *testing.T) {
	t.Parallel()

	hv := HomeView{
		Nebulae: []NebulaChoice{
			{Name: "a"}, {Name: "b"}, {Name: "c"},
		},
		Cursor: 0,
	}

	// Move down through the list.
	hv.MoveDown()
	if hv.Cursor != 1 {
		t.Errorf("after MoveDown from 0: expected cursor 1, got %d", hv.Cursor)
	}
	hv.MoveDown()
	if hv.Cursor != 2 {
		t.Errorf("after MoveDown from 1: expected cursor 2, got %d", hv.Cursor)
	}
	// At bottom, MoveDown should be a no-op.
	hv.MoveDown()
	if hv.Cursor != 2 {
		t.Errorf("MoveDown at bottom: expected cursor 2, got %d", hv.Cursor)
	}

	// Move back up.
	hv.MoveUp()
	if hv.Cursor != 1 {
		t.Errorf("after MoveUp from 2: expected cursor 1, got %d", hv.Cursor)
	}
	hv.MoveUp()
	if hv.Cursor != 0 {
		t.Errorf("after MoveUp from 1: expected cursor 0, got %d", hv.Cursor)
	}
	// At top, MoveUp should be a no-op.
	hv.MoveUp()
	if hv.Cursor != 0 {
		t.Errorf("MoveUp at top: expected cursor 0, got %d", hv.Cursor)
	}
}

func TestHomeView_CompactWidth(t *testing.T) {
	t.Parallel()

	hv := HomeView{
		Nebulae: []NebulaChoice{
			{Name: "a-very-long-nebula-name-that-might-be-truncated", Description: "desc", Status: "ready", Phases: 1},
		},
		Cursor: 0,
		Width:  50,
	}
	out := hv.View()

	// Should render without panicking with a narrow width.
	if out == "" {
		t.Error("expected non-empty output for compact width")
	}
}

func TestHomeFooterBindings(t *testing.T) {
	t.Parallel()

	km := DefaultKeyMap()
	bindings := HomeFooterBindings(km)

	if len(bindings) != 5 {
		t.Fatalf("expected 5 home footer bindings, got %d", len(bindings))
	}

	// Verify the enter binding says "run".
	enterHelp := bindings[2].Help()
	if enterHelp.Desc != "run" {
		t.Errorf("expected enter binding desc 'run', got %q", enterHelp.Desc)
	}

	// Verify the info binding says "info".
	infoHelp := bindings[3].Help()
	if infoHelp.Desc != "info" {
		t.Errorf("expected info binding desc 'info', got %q", infoHelp.Desc)
	}
}

func TestHomeStatusIconAndStyle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		icon   string
	}{
		{"ready", iconWaiting},
		{"done", iconDone},
		{"in_progress", iconWorking},
		{"partial", iconFailed},
	}

	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			t.Parallel()
			icon, _ := homeStatusIconAndStyle(tc.status)
			if icon != tc.icon {
				t.Errorf("status %q: expected icon %q, got %q", tc.status, tc.icon, icon)
			}
		})
	}
}

func TestHomeStatusLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		nc     NebulaChoice
		expect string
	}{
		{NebulaChoice{Status: "ready", Phases: 3}, "ready"},
		{NebulaChoice{Status: "done", Phases: 3, Done: 3}, "done"},
		{NebulaChoice{Status: "in_progress", Phases: 5, Done: 2}, "2/5 done"},
		{NebulaChoice{Status: "partial", Phases: 4, Done: 1}, "1/4 partial"},
	}

	for _, tc := range tests {
		t.Run(tc.nc.Status, func(t *testing.T) {
			t.Parallel()
			got := homeStatusLabel(tc.nc)
			if got != tc.expect {
				t.Errorf("expected %q, got %q", tc.expect, got)
			}
		})
	}
}
