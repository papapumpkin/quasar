package tui

import (
	"fmt"
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
	// Count lines: filter bar + main row (no description line).
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines (filter bar + row), got %d lines:\n%s", len(lines), out)
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

	if len(bindings) != 6 {
		t.Fatalf("expected 6 home footer bindings, got %d", len(bindings))
	}

	// Verify the enter binding says "run".
	enterHelp := bindings[2].Help()
	if enterHelp.Desc != "run" {
		t.Errorf("expected enter binding desc 'run', got %q", enterHelp.Desc)
	}

	// Verify the filter binding says "filter".
	filterHelp := bindings[3].Help()
	if filterHelp.Desc != "filter" {
		t.Errorf("expected filter binding desc 'filter', got %q", filterHelp.Desc)
	}

	// Verify the info binding says "info".
	infoHelp := bindings[4].Help()
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

func TestHomeView_ScrollingCursorVisible(t *testing.T) {
	t.Parallel()

	// Create 10 nebulae with descriptions (2 lines each = 20 total lines).
	// Height of 6 means only ~2-3 rows visible at a time.
	nebulae := make([]NebulaChoice, 10)
	for i := range nebulae {
		nebulae[i] = NebulaChoice{
			Name:        fmt.Sprintf("nebula-%d", i),
			Description: fmt.Sprintf("Description for nebula %d", i),
			Status:      "ready",
			Phases:      1,
		}
	}

	t.Run("cursor at bottom scrolls viewport", func(t *testing.T) {
		t.Parallel()
		hv := HomeView{
			Nebulae: nebulae,
			Cursor:  8, // near the bottom
			Offset:  0, // viewport starts at top
			Width:   80,
			Height:  6,
		}
		out := hv.View()

		// The cursor's nebula must be visible.
		if !strings.Contains(out, "nebula-8") {
			t.Errorf("expected cursor nebula 'nebula-8' to be visible, got:\n%s", out)
		}
		// First nebula should NOT be visible (scrolled past).
		if strings.Contains(out, "nebula-0") {
			t.Errorf("expected nebula-0 to be scrolled out of view, got:\n%s", out)
		}
		// Should show an up indicator.
		if !strings.Contains(out, "more") {
			t.Errorf("expected scroll indicator, got:\n%s", out)
		}
	})

	t.Run("cursor at top with high offset snaps back", func(t *testing.T) {
		t.Parallel()
		hv := HomeView{
			Nebulae: nebulae,
			Cursor:  0,
			Offset:  5, // offset past cursor
			Width:   80,
			Height:  6,
		}
		out := hv.View()

		// Cursor nebula must be visible.
		if !strings.Contains(out, "nebula-0") {
			t.Errorf("expected cursor nebula 'nebula-0' to be visible, got:\n%s", out)
		}
	})

	t.Run("all items fit no scrolling", func(t *testing.T) {
		t.Parallel()
		small := nebulae[:2] // 2 items × 2 lines = 4 lines, fits in height 10
		hv := HomeView{
			Nebulae: small,
			Cursor:  0,
			Width:   80,
			Height:  10,
		}
		out := hv.View()

		// Both should be visible, no scroll indicators.
		if !strings.Contains(out, "nebula-0") {
			t.Errorf("expected 'nebula-0', got:\n%s", out)
		}
		if !strings.Contains(out, "nebula-1") {
			t.Errorf("expected 'nebula-1', got:\n%s", out)
		}
		if strings.Contains(out, "more") {
			t.Errorf("expected no scroll indicators when all items fit, got:\n%s", out)
		}
	})

	t.Run("zero height renders all", func(t *testing.T) {
		t.Parallel()
		hv := HomeView{
			Nebulae: nebulae,
			Cursor:  0,
			Width:   80,
			Height:  0, // no constraint
		}
		out := hv.View()

		// All items should be rendered.
		if !strings.Contains(out, "nebula-0") {
			t.Errorf("expected 'nebula-0', got:\n%s", out)
		}
		if !strings.Contains(out, "nebula-9") {
			t.Errorf("expected 'nebula-9', got:\n%s", out)
		}
	})
}

func TestHomeView_EnsureCursorVisible(t *testing.T) {
	t.Parallel()

	nebulae := make([]NebulaChoice, 8)
	for i := range nebulae {
		nebulae[i] = NebulaChoice{
			Name:        fmt.Sprintf("n%d", i),
			Description: fmt.Sprintf("desc %d", i),
			Status:      "ready",
			Phases:      1,
		}
	}

	t.Run("offset stays when cursor is visible", func(t *testing.T) {
		t.Parallel()
		hv := HomeView{
			Nebulae: nebulae,
			Cursor:  1,
			Offset:  0,
			Height:  8, // enough for ~3 rows with descriptions
		}
		got := hv.ensureCursorVisible()
		if got != 0 {
			t.Errorf("expected offset 0 (cursor visible), got %d", got)
		}
	})

	t.Run("offset snaps to cursor when cursor above", func(t *testing.T) {
		t.Parallel()
		hv := HomeView{
			Nebulae: nebulae,
			Cursor:  1,
			Offset:  5,
			Height:  6,
		}
		got := hv.ensureCursorVisible()
		if got != 1 {
			t.Errorf("expected offset to snap to cursor 1, got %d", got)
		}
	})

	t.Run("offset increases when cursor below visible window", func(t *testing.T) {
		t.Parallel()
		hv := HomeView{
			Nebulae: nebulae,
			Cursor:  7,
			Offset:  0,
			Height:  6,
		}
		got := hv.ensureCursorVisible()
		if got <= 0 {
			t.Errorf("expected offset > 0 to bring cursor 7 into view, got %d", got)
		}
	})
}

func TestHomeFilter_Cycle(t *testing.T) {
	t.Parallel()

	f := HomeFilterAll
	f = f.Next()
	if f != HomeFilterReady {
		t.Errorf("expected HomeFilterReady, got %d", f)
	}
	f = f.Next()
	if f != HomeFilterInProgress {
		t.Errorf("expected HomeFilterInProgress, got %d", f)
	}
	f = f.Next()
	if f != HomeFilterDone {
		t.Errorf("expected HomeFilterDone, got %d", f)
	}
	f = f.Next()
	if f != HomeFilterAll {
		t.Errorf("expected HomeFilterAll after full cycle, got %d", f)
	}
}

func TestHomeFilter_FilterNebulae(t *testing.T) {
	t.Parallel()

	all := []NebulaChoice{
		{Name: "a", Status: "ready"},
		{Name: "b", Status: "in_progress"},
		{Name: "c", Status: "done"},
		{Name: "d", Status: "ready"},
	}

	tests := []struct {
		filter HomeFilter
		want   int
	}{
		{HomeFilterAll, 4},
		{HomeFilterReady, 3},      // ready + in_progress
		{HomeFilterInProgress, 1}, // in_progress only
		{HomeFilterDone, 1},       // done only
	}

	for _, tc := range tests {
		t.Run(tc.filter.String(), func(t *testing.T) {
			t.Parallel()
			got := tc.filter.FilterNebulae(all)
			if len(got) != tc.want {
				t.Errorf("filter %q: expected %d, got %d", tc.filter, tc.want, len(got))
			}
		})
	}
}

func TestHomeView_FilterBarRendered(t *testing.T) {
	t.Parallel()

	hv := HomeView{
		Nebulae: []NebulaChoice{
			{Name: "test", Status: "ready", Phases: 1},
		},
		Cursor: 0,
		Width:  80,
		Filter: HomeFilterReady,
	}
	out := hv.View()

	// The active filter label should appear highlighted.
	if !strings.Contains(out, "active") {
		t.Errorf("expected 'active' filter label in output, got:\n%s", out)
	}
}

func TestHomeView_EmptyFilter(t *testing.T) {
	t.Parallel()

	hv := HomeView{
		Nebulae: nil,
		Width:   80,
		Filter:  HomeFilterDone,
	}
	out := hv.View()

	if !strings.Contains(out, "No nebulas matching filter") {
		t.Errorf("expected empty filter message, got:\n%s", out)
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
