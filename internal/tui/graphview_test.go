package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewGraphView_Empty(t *testing.T) {
	t.Parallel()
	gv := NewGraphView(nil, 80, 24)
	if gv.renderer == nil {
		t.Fatal("renderer should be initialized even with no phases")
	}
	if len(gv.waves) != 0 {
		t.Errorf("expected 0 waves, got %d", len(gv.waves))
	}
	view := gv.View()
	if !strings.Contains(view, "No graph data") {
		t.Errorf("expected empty placeholder, got:\n%s", view)
	}
}

func TestNewGraphView_SinglePhase(t *testing.T) {
	t.Parallel()
	phases := []PhaseInfo{
		{ID: "p1", Title: "Phase 1"},
	}
	gv := NewGraphView(phases, 80, 24)
	if len(gv.waves) != 1 {
		t.Fatalf("expected 1 wave, got %d", len(gv.waves))
	}
	if len(gv.nodeIDs) != 1 {
		t.Fatalf("expected 1 nodeID, got %d", len(gv.nodeIDs))
	}
	if gv.SelectedPhaseID() != "p1" {
		t.Errorf("expected selected phase p1, got %q", gv.SelectedPhaseID())
	}
}

func TestNewGraphView_WithDeps(t *testing.T) {
	t.Parallel()
	phases := []PhaseInfo{
		{ID: "p1", Title: "Phase 1"},
		{ID: "p2", Title: "Phase 2", DependsOn: []string{"p1"}},
		{ID: "p3", Title: "Phase 3", DependsOn: []string{"p1"}},
	}
	gv := NewGraphView(phases, 80, 24)
	if len(gv.waves) != 2 {
		t.Fatalf("expected 2 waves, got %d", len(gv.waves))
	}
	if len(gv.nodeIDs) != 3 {
		t.Fatalf("expected 3 nodeIDs, got %d", len(gv.nodeIDs))
	}
	// Wave 1 should contain p1 (no deps), wave 2 should contain p2 and p3.
	if gv.waves[0].NodeIDs[0] != "p1" {
		t.Errorf("wave 0 should contain p1, got %v", gv.waves[0].NodeIDs)
	}
}

func TestGraphView_SetPhaseStatus(t *testing.T) {
	t.Parallel()
	phases := []PhaseInfo{
		{ID: "p1", Title: "Phase 1"},
		{ID: "p2", Title: "Phase 2"},
	}
	gv := NewGraphView(phases, 80, 24)
	gv.SetPhaseStatus("p1", PhaseWorking)
	if gv.statuses["p1"] != PhaseWorking {
		t.Errorf("expected PhaseWorking for p1, got %d", gv.statuses["p1"])
	}
	gv.SetPhaseStatus("p1", PhaseDone)
	if gv.statuses["p1"] != PhaseDone {
		t.Errorf("expected PhaseDone for p1, got %d", gv.statuses["p1"])
	}
}

func TestGraphView_CursorNavigation(t *testing.T) {
	t.Parallel()
	phases := []PhaseInfo{
		{ID: "p1", Title: "Phase 1"},
		{ID: "p2", Title: "Phase 2"},
		{ID: "p3", Title: "Phase 3"},
	}
	gv := NewGraphView(phases, 80, 24)

	// Initial cursor at 0.
	if gv.SelectedPhaseID() != "p1" {
		t.Errorf("initial selection should be p1, got %q", gv.SelectedPhaseID())
	}

	// Move down.
	gv.MoveDown()
	if gv.SelectedPhaseID() != "p2" {
		t.Errorf("after MoveDown should be p2, got %q", gv.SelectedPhaseID())
	}

	gv.MoveDown()
	if gv.SelectedPhaseID() != "p3" {
		t.Errorf("after 2x MoveDown should be p3, got %q", gv.SelectedPhaseID())
	}

	// Move down past end — should stay at p3.
	gv.MoveDown()
	if gv.SelectedPhaseID() != "p3" {
		t.Errorf("after MoveDown past end should stay at p3, got %q", gv.SelectedPhaseID())
	}

	// Move up.
	gv.MoveUp()
	if gv.SelectedPhaseID() != "p2" {
		t.Errorf("after MoveUp should be p2, got %q", gv.SelectedPhaseID())
	}

	// Move up past start — should stay at p1.
	gv.MoveUp()
	gv.MoveUp()
	if gv.SelectedPhaseID() != "p1" {
		t.Errorf("after MoveUp past start should stay at p1, got %q", gv.SelectedPhaseID())
	}
}

func TestGraphView_ToggleTracks(t *testing.T) {
	t.Parallel()
	phases := []PhaseInfo{
		{ID: "p1", Title: "Phase 1"},
	}
	gv := NewGraphView(phases, 80, 24)

	if gv.showTracks {
		t.Error("tracks should be off initially")
	}
	gv.ToggleTracks()
	if !gv.showTracks {
		t.Error("tracks should be on after toggle")
	}
	if gv.renderer.TrackMap == nil {
		t.Error("TrackMap should be set when tracks are on")
	}
	gv.ToggleTracks()
	if gv.showTracks {
		t.Error("tracks should be off after second toggle")
	}
	if gv.renderer.TrackMap != nil {
		t.Error("TrackMap should be nil when tracks are off")
	}
}

func TestGraphView_ToggleCriticalPath(t *testing.T) {
	t.Parallel()
	phases := []PhaseInfo{
		{ID: "p1", Title: "Phase 1"},
		{ID: "p2", Title: "Phase 2", DependsOn: []string{"p1"}},
	}
	gv := NewGraphView(phases, 80, 24)

	if gv.showCriticalPath {
		t.Error("critical path should be off initially")
	}
	gv.ToggleCriticalPath()
	if !gv.showCriticalPath {
		t.Error("critical path should be on after toggle")
	}
	if gv.renderer.CriticalPath == nil {
		t.Error("CriticalPath should be set when toggled on")
	}
	// Both p1 and p2 should be on the critical path (linear chain).
	if !gv.renderer.CriticalPath["p1"] || !gv.renderer.CriticalPath["p2"] {
		t.Errorf("expected p1 and p2 on critical path, got %v", gv.renderer.CriticalPath)
	}
	gv.ToggleCriticalPath()
	if gv.showCriticalPath {
		t.Error("critical path should be off after second toggle")
	}
}

func TestGraphView_ViewContainsNodeTitles(t *testing.T) {
	t.Parallel()
	phases := []PhaseInfo{
		{ID: "p1", Title: "Alpha Phase"},
		{ID: "p2", Title: "Beta Phase"},
	}
	gv := NewGraphView(phases, 100, 30)
	view := gv.View()
	if !strings.Contains(view, "Alpha Phase") {
		t.Error("view should contain 'Alpha Phase'")
	}
	if !strings.Contains(view, "Beta Phase") {
		t.Error("view should contain 'Beta Phase'")
	}
}

func TestGraphView_StatusColorsInView(t *testing.T) {
	t.Parallel()
	phases := []PhaseInfo{
		{ID: "p1", Title: "Done Phase"},
		{ID: "p2", Title: "Running Phase"},
		{ID: "p3", Title: "Failed Phase"},
	}
	gv := NewGraphView(phases, 100, 30)
	gv.SetPhaseStatus("p1", PhaseDone)
	gv.SetPhaseStatus("p2", PhaseWorking)
	gv.SetPhaseStatus("p3", PhaseFailed)

	view := gv.View()
	// The view should contain the cursor indicator with the selected node
	// (p1 is first in wave order).
	if !strings.Contains(view, "Done Phase") {
		t.Error("view should show 'Done Phase'")
	}
}

func TestGraphView_ZeroValueSafe(t *testing.T) {
	t.Parallel()
	var gv GraphView
	// All operations on zero-value GraphView should not panic.
	gv.SetSize(80, 24)
	gv.SetPhaseStatus("p1", PhaseDone)
	gv.MoveUp()
	gv.MoveDown()
	gv.ToggleTracks()
	gv.ToggleCriticalPath()
	if id := gv.SelectedPhaseID(); id != "" {
		t.Errorf("zero-value SelectedPhaseID should be empty, got %q", id)
	}
	view := gv.View()
	if !strings.Contains(view, "No graph data") {
		t.Errorf("zero-value View should show placeholder, got:\n%s", view)
	}
}

func TestGraphView_PhaseStatusMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status PhaseStatus
		want   string
	}{
		{PhaseWaiting, "queued"},
		{PhaseWorking, "running"},
		{PhaseDone, "done"},
		{PhaseFailed, "failed"},
		{PhaseGate, "blocked"},
		{PhaseSkipped, "done"},
	}
	for _, tt := range tests {
		got := phaseStatusToDAGState(tt.status)
		if got != tt.want {
			t.Errorf("phaseStatusToDAGState(%d) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

// TestGraphTabKeyBindings verifies that 't' and 'c' keys work in the graph tab.
func TestGraphTabKeyBindings(t *testing.T) {
	t.Parallel()

	t.Run("t toggles tracks", func(t *testing.T) {
		t.Parallel()
		m := nebulaModel()
		// Initialize graph with phases.
		m.Graph = NewGraphView([]PhaseInfo{
			{ID: "p1", Title: "Phase 1"},
		}, m.Width, m.Height)
		m.ActiveTab = TabGraph

		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}}
		updated, _ := m.Update(msg)
		m = updated.(AppModel)
		if !m.Graph.showTracks {
			t.Error("expected tracks to be toggled on after pressing 't'")
		}
	})

	t.Run("c toggles critical path", func(t *testing.T) {
		t.Parallel()
		m := nebulaModel()
		m.Graph = NewGraphView([]PhaseInfo{
			{ID: "p1", Title: "Phase 1"},
		}, m.Width, m.Height)
		m.ActiveTab = TabGraph

		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}}
		updated, _ := m.Update(msg)
		m = updated.(AppModel)
		if !m.Graph.showCriticalPath {
			t.Error("expected critical path to be toggled on after pressing 'c'")
		}
	})
}
