package tui

import (
	"strings"
	"testing"
	"time"
)

func TestNebulaViewView_WaveSeparators(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Phases = []PhaseEntry{
		{ID: "setup", Status: PhaseDone, Wave: 1, CostUSD: 0.15, Cycles: 2},
		{ID: "auth", Status: PhaseWorking, Wave: 2, Cycles: 1, MaxCycles: 5, StartedAt: time.Now()},
		{ID: "tests", Status: PhaseWaiting, Wave: 2, BlockedBy: "auth"},
	}
	nv.Width = 80

	view := nv.View()

	if !strings.Contains(view, "Wave 1") {
		t.Error("expected wave 1 separator in view")
	}
	if !strings.Contains(view, "Wave 2") {
		t.Error("expected wave 2 separator in view")
	}
}

func TestNebulaViewView_ColumnAlignment(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Phases = []PhaseEntry{
		{ID: "short", Status: PhaseDone, Wave: 1, CostUSD: 1.50, Cycles: 3},
		{ID: "a-much-longer-phase-id", Status: PhaseWaiting, Wave: 1},
	}
	nv.Width = 80

	view := nv.View()

	// Both phase IDs should appear.
	if !strings.Contains(view, "short") {
		t.Error("expected 'short' in view")
	}
	if !strings.Contains(view, "a-much-longer-phase-id") {
		t.Error("expected longer phase ID in view")
	}
}

func TestNebulaViewView_CycleProgress(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Phases = []PhaseEntry{
		{ID: "auth", Status: PhaseWorking, Wave: 1, Cycles: 2, MaxCycles: 5, StartedAt: time.Now()},
	}
	nv.Width = 80

	view := nv.View()

	if !strings.Contains(view, "cycle 2/5") {
		t.Errorf("expected cycle progress 'cycle 2/5' in view, got:\n%s", view)
	}
}

func TestNebulaViewView_CycleProgressNoMax(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Phases = []PhaseEntry{
		{ID: "auth", Status: PhaseWorking, Wave: 1, Cycles: 3, MaxCycles: 0, StartedAt: time.Now()},
	}
	nv.Width = 80

	view := nv.View()

	if !strings.Contains(view, "cycle 3") {
		t.Errorf("expected 'cycle 3' in view, got:\n%s", view)
	}
	// Should not contain "cycle 3/0".
	if strings.Contains(view, "cycle 3/0") {
		t.Error("should not show cycle X/0 when MaxCycles is 0")
	}
}

func TestNebulaViewView_DoneStateDetail(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Phases = []PhaseEntry{
		{ID: "setup", Status: PhaseDone, Wave: 1, CostUSD: 0.15, Cycles: 2},
	}
	nv.Width = 80

	view := nv.View()

	if !strings.Contains(view, "$0.15") {
		t.Error("expected cost in done state")
	}
	if !strings.Contains(view, "2 cycle(s)") {
		t.Error("expected cycle count in done state")
	}
}

func TestNebulaViewView_DoneStateWithElapsed(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Phases = []PhaseEntry{
		{
			ID:        "setup",
			Status:    PhaseDone,
			Wave:      1,
			CostUSD:   0.15,
			Cycles:    2,
			StartedAt: time.Now().Add(-30 * time.Second),
		},
	}
	nv.Width = 80

	view := nv.View()

	// Should show elapsed time for done phase.
	if !strings.Contains(view, "30s") {
		t.Errorf("expected elapsed time in done state, got:\n%s", view)
	}
}

func TestNebulaViewView_BlockedDetail(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Phases = []PhaseEntry{
		{ID: "tests", Status: PhaseWaiting, Wave: 2, BlockedBy: "auth"},
	}
	nv.Width = 80

	view := nv.View()

	if !strings.Contains(view, "blocked: auth") {
		t.Error("expected blocked detail in view")
	}
}

func TestNebulaViewView_SelectionIndicator(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Phases = []PhaseEntry{
		{ID: "a", Status: PhaseDone, Wave: 1},
		{ID: "b", Status: PhaseWaiting, Wave: 1},
	}
	nv.Cursor = 0
	nv.Width = 80

	view := nv.View()

	if !strings.Contains(view, selectionIndicator) {
		t.Error("expected selection indicator in view")
	}
}

func TestNebulaViewView_EmptyPhases(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Width = 80

	view := nv.View()

	if view != "" {
		t.Errorf("expected empty view for no phases, got: %q", view)
	}
}

func TestNebulaViewView_NoWaveSeparatorForWaveZero(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Phases = []PhaseEntry{
		{ID: "a", Status: PhaseWaiting, Wave: 0},
		{ID: "b", Status: PhaseWaiting, Wave: 0},
	}
	nv.Width = 80

	view := nv.View()

	if strings.Contains(view, "Wave") {
		t.Error("should not show wave separator for wave 0")
	}
}

func TestNebulaViewView_RefactoredIndicator(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Phases = []PhaseEntry{
		{ID: "auth", Status: PhaseWorking, Wave: 1, Cycles: 2, MaxCycles: 5, StartedAt: time.Now(), Refactored: true},
	}
	nv.Width = 80

	view := nv.View()

	if !strings.Contains(view, "⟳ refactored") {
		t.Errorf("expected refactored indicator in view, got:\n%s", view)
	}
}

func TestNebulaViewView_RefactoredIndicatorNotShownWhenFalse(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Phases = []PhaseEntry{
		{ID: "auth", Status: PhaseWorking, Wave: 1, Cycles: 2, MaxCycles: 5, StartedAt: time.Now(), Refactored: false},
	}
	nv.Width = 80

	view := nv.View()

	if strings.Contains(view, "refactored") {
		t.Errorf("should not show refactored indicator when Refactored is false, got:\n%s", view)
	}
}

func TestSetPhaseRefactored(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Phases = []PhaseEntry{
		{ID: "a", Status: PhaseWorking, Wave: 1},
		{ID: "b", Status: PhaseWaiting, Wave: 1},
	}

	nv.SetPhaseRefactored("a", true)
	if !nv.Phases[0].Refactored {
		t.Error("expected phase 'a' Refactored=true")
	}
	if nv.Phases[1].Refactored {
		t.Error("expected phase 'b' Refactored=false")
	}

	nv.SetPhaseRefactored("a", false)
	if nv.Phases[0].Refactored {
		t.Error("expected phase 'a' Refactored=false after clear")
	}
}

func TestNebulaViewView_AllStatuses(t *testing.T) {
	t.Parallel()
	nv := NewNebulaView()
	nv.Phases = []PhaseEntry{
		{ID: "done", Status: PhaseDone, Wave: 1, CostUSD: 0.10, Cycles: 1},
		{ID: "working", Status: PhaseWorking, Wave: 1, StartedAt: time.Now()},
		{ID: "failed", Status: PhaseFailed, Wave: 1},
		{ID: "gate", Status: PhaseGate, Wave: 1},
		{ID: "skipped", Status: PhaseSkipped, Wave: 1},
		{ID: "waiting", Status: PhaseWaiting, Wave: 1},
	}
	nv.Width = 80

	view := nv.View()

	for _, id := range []string{"done", "working", "failed", "gate", "skipped", "waiting"} {
		if !strings.Contains(view, id) {
			t.Errorf("expected phase %q in view", id)
		}
	}
}
