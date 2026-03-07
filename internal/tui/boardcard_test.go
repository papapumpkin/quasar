package tui

import (
	"strings"
	"testing"
)

func TestRenderBoardEntry_RunningCard(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150

	p := PhaseEntry{
		ID: "test-phase", Title: "Test Phase", Status: PhaseWorking,
		Cycles: 2, MaxCycles: 5, CostUSD: 0.42, LastActivity: "coding...",
	}

	result := bv.renderBoardEntry(p, false, 25)

	if !strings.Contains(result, "Test Phase") {
		t.Error("expected title in card")
	}
	if !strings.Contains(result, "[2/5]") {
		t.Error("expected progress indicator in card")
	}
	if !strings.Contains(result, "$0.42") {
		t.Error("expected cost badge in card")
	}
	if !strings.Contains(result, "coding...") {
		t.Error("expected activity subtitle in card")
	}
	if !strings.Contains(result, healthDot) {
		t.Error("expected health dot in card")
	}
}

func TestRenderBoardEntry_AttentionMarker(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150

	p := PhaseEntry{
		ID: "hail-phase", Title: "Needs Help", Status: PhaseWorking,
		HasPendingHails: true, Cycles: 1, MaxCycles: 5,
	}

	result := bv.renderBoardEntry(p, false, 25)

	if !strings.Contains(result, "!") {
		t.Error("expected attention marker for phase with pending hails")
	}
}

func TestRenderBoardEntry_GateAttention(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150

	p := PhaseEntry{ID: "gate", Title: "Review Gate", Status: PhaseGate}
	result := bv.renderBoardEntry(p, false, 25)

	if !strings.Contains(result, "!") {
		t.Error("expected attention marker for gate phase")
	}
}

func TestRenderBoardEntry_Selected(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150

	p := PhaseEntry{ID: "sel", Title: "Selected", Status: PhaseWaiting}
	result := bv.renderBoardEntry(p, true, 25)

	if !strings.Contains(result, selectionIndicator) {
		t.Error("expected selection indicator for selected card")
	}
}

func TestRenderBoardEntry_DoneNoActivity(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150

	p := PhaseEntry{
		ID: "done", Title: "Done Phase", Status: PhaseDone,
		CostUSD: 1.50, Cycles: 3, MaxCycles: 5, LastActivity: "coding...",
	}
	result := bv.renderBoardEntry(p, false, 25)

	// Activity subtitle should not appear on done phases.
	if strings.Contains(result, "coding...") {
		t.Error("activity subtitle should not appear on done phases")
	}
	// Cost and progress should still appear.
	if !strings.Contains(result, "$1.50") {
		t.Error("expected cost badge on done phase")
	}
}

func TestRenderBoardEntry_CompletionNote(t *testing.T) {
	t.Parallel()
	bv := NewBoardView()
	bv.Width = 150

	p := PhaseEntry{
		ID: "done", Title: "Done Phase", Status: PhaseDone,
		CompletionNote: "all tests pass",
	}
	result := bv.renderBoardEntry(p, false, 25)

	if !strings.Contains(result, "all tests pass") {
		t.Error("expected completion note on done phase card")
	}
}
