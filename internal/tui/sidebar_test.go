package tui

import (
	"strings"
	"testing"
)

func TestSidebarNewSidebar(t *testing.T) {
	t.Parallel()
	s := NewSidebar()
	if s.Cursor != 0 {
		t.Errorf("expected cursor 0, got %d", s.Cursor)
	}
	if len(s.Phases) != 0 {
		t.Errorf("expected empty phases, got %d", len(s.Phases))
	}
}

func TestSidebarSelectedPhaseEmpty(t *testing.T) {
	t.Parallel()
	s := NewSidebar()
	if s.SelectedPhase() != nil {
		t.Error("expected nil for empty sidebar")
	}
}

func TestSidebarSelectedPhase(t *testing.T) {
	t.Parallel()
	s := NewSidebar()
	s.Phases = []PhaseEntry{
		{ID: "ph-1", Title: "Phase One"},
		{ID: "ph-2", Title: "Phase Two"},
	}
	s.Cursor = 1
	p := s.SelectedPhase()
	if p == nil {
		t.Fatal("expected non-nil phase")
	}
	if p.ID != "ph-2" {
		t.Errorf("expected ph-2, got %s", p.ID)
	}
}

func TestSidebarMoveUpDown(t *testing.T) {
	t.Parallel()
	s := NewSidebar()
	s.Phases = []PhaseEntry{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
	}
	s.Height = 10

	s.MoveDown()
	if s.Cursor != 1 {
		t.Errorf("after MoveDown, cursor = %d, want 1", s.Cursor)
	}

	s.MoveDown()
	if s.Cursor != 2 {
		t.Errorf("after second MoveDown, cursor = %d, want 2", s.Cursor)
	}

	// Clamp at end.
	s.MoveDown()
	if s.Cursor != 2 {
		t.Errorf("should clamp at last phase, cursor = %d", s.Cursor)
	}

	s.MoveUp()
	if s.Cursor != 1 {
		t.Errorf("after MoveUp, cursor = %d, want 1", s.Cursor)
	}

	// Clamp at start.
	s.MoveUp()
	s.MoveUp()
	if s.Cursor != 0 {
		t.Errorf("should clamp at 0, cursor = %d", s.Cursor)
	}
}

func TestSidebarScrolling(t *testing.T) {
	t.Parallel()
	s := NewSidebar()
	s.Height = 5 // small viewport
	for i := 0; i < 20; i++ {
		s.Phases = append(s.Phases, PhaseEntry{ID: "ph-" + string(rune('a'+i))})
	}

	// Move cursor past the viewport.
	for i := 0; i < 10; i++ {
		s.MoveDown()
	}

	if s.Cursor != 10 {
		t.Errorf("cursor = %d, want 10", s.Cursor)
	}

	// Offset should have adjusted so cursor is visible.
	if s.Cursor < s.Offset || s.Cursor >= s.Offset+s.Height {
		t.Errorf("cursor %d not visible in viewport [%d, %d)", s.Cursor, s.Offset, s.Offset+s.Height)
	}
}

func TestSidebarSyncPhases(t *testing.T) {
	t.Parallel()
	s := NewSidebar()
	s.Height = 10
	phases := []PhaseEntry{
		{ID: "a"}, {ID: "b"}, {ID: "c"},
	}
	s.SyncPhases(phases)
	if len(s.Phases) != 3 {
		t.Errorf("expected 3 phases, got %d", len(s.Phases))
	}

	// Cursor clamped if phases shrink.
	s.Cursor = 2
	s.SyncPhases([]PhaseEntry{{ID: "a"}})
	if s.Cursor != 0 {
		t.Errorf("cursor should be clamped to 0, got %d", s.Cursor)
	}
}

func TestSidebarViewEmpty(t *testing.T) {
	t.Parallel()
	s := NewSidebar()
	s.Width = 25
	s.Height = 10
	view := s.View()
	if !strings.Contains(view, "PHASES") {
		t.Error("sidebar should contain title 'PHASES'")
	}
	if !strings.Contains(view, "no phases") {
		t.Error("empty sidebar should show 'no phases' message")
	}
}

func TestSidebarViewWithPhases(t *testing.T) {
	t.Parallel()
	s := NewSidebar()
	s.Width = 30
	s.Height = 15
	s.Phases = []PhaseEntry{
		{ID: "auth", Title: "Authentication", Status: PhaseDone},
		{ID: "db", Title: "Database", Status: PhaseWorking},
		{ID: "api", Title: "API Layer", Status: PhaseWaiting},
	}
	s.Cursor = 1

	view := s.View()
	if !strings.Contains(view, "PHASES") {
		t.Error("sidebar should contain title")
	}
	if !strings.Contains(view, iconDone) {
		t.Error("sidebar should show done icon for completed phase")
	}
	if !strings.Contains(view, iconWorking) {
		t.Error("sidebar should show working icon for active phase")
	}
}

func TestSidebarViewFocusedBorder(t *testing.T) {
	t.Parallel()
	s := NewSidebar()
	s.Width = 25
	s.Height = 10
	s.Phases = []PhaseEntry{{ID: "a", Title: "Alpha"}}

	// Unfocused render — should not panic.
	s.Focus = false
	view1 := s.View()

	// Focused render — should not panic.
	s.Focus = true
	view2 := s.View()

	// Both should render without panics and contain content.
	if !strings.Contains(view1, "PHASES") || !strings.Contains(view2, "PHASES") {
		t.Error("both focused and unfocused should render title")
	}
}

func TestSidebarViewTooSmall(t *testing.T) {
	t.Parallel()
	s := NewSidebar()
	s.Width = 3
	s.Height = 1
	s.Phases = []PhaseEntry{{ID: "a"}}
	view := s.View()
	if view != "" {
		t.Errorf("expected empty string for too-small sidebar, got %q", view)
	}
}

func TestSidebarScrollIndicators(t *testing.T) {
	t.Parallel()
	s := NewSidebar()
	s.Width = 30
	s.Height = 6 // title(1) + spacing(1) + 4 visible lines
	for i := 0; i < 10; i++ {
		s.Phases = append(s.Phases, PhaseEntry{ID: "ph", Title: "Phase"})
	}

	// Scroll down so there are items above.
	s.Cursor = 5
	s.ensureVisible()

	view := s.View()
	if !strings.Contains(view, "more") {
		t.Error("should show scroll indicator when items are out of view")
	}
}

func TestPhaseStatusIcon(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status PhaseStatus
		icon   string
	}{
		{PhaseDone, iconDone},
		{PhaseFailed, iconFailed},
		{PhaseWorking, iconWorking},
		{PhaseGate, iconGate},
		{PhaseSkipped, iconSkipped},
		{PhaseWaiting, iconWaiting},
	}
	for _, tt := range tests {
		got := phaseStatusIcon(tt.status)
		if got != tt.icon {
			t.Errorf("phaseStatusIcon(%d) = %q, want %q", tt.status, got, tt.icon)
		}
	}
}

func TestSidebarWidthConstants(t *testing.T) {
	t.Parallel()
	if SidebarMinWidth < 10 {
		t.Errorf("SidebarMinWidth = %d, want >= 10", SidebarMinWidth)
	}
	if SidebarMaxWidth < SidebarMinWidth {
		t.Errorf("SidebarMaxWidth (%d) < SidebarMinWidth (%d)", SidebarMaxWidth, SidebarMinWidth)
	}
	if SidebarCollapseWidth < MinWidth {
		t.Errorf("SidebarCollapseWidth (%d) < MinWidth (%d)", SidebarCollapseWidth, MinWidth)
	}
}

func TestComputeSidebarWidth(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		termWidth int
		wantMin   int
		wantMax   int
	}{
		{"narrow collapse", SidebarCollapseWidth - 1, 0, 0},
		{"at collapse threshold", SidebarCollapseWidth, SidebarMinWidth, SidebarMaxWidth},
		{"medium terminal", 120, SidebarMinWidth, SidebarMaxWidth},
		{"wide terminal", 220, SidebarMinWidth, SidebarMaxWidth},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ComputeSidebarWidth(tt.termWidth)
			if tt.wantMin == 0 && tt.wantMax == 0 {
				if got != 0 {
					t.Errorf("ComputeSidebarWidth(%d) = %d, want 0 (collapsed)", tt.termWidth, got)
				}
				return
			}
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("ComputeSidebarWidth(%d) = %d, want [%d, %d]", tt.termWidth, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}
