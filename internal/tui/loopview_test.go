package tui

import (
	"strings"
	"testing"
	"time"
)

func TestLoopViewView_TreeConnectors(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.Cycles = []CycleEntry{
		{
			Number: 1,
			Agents: []AgentEntry{
				{Role: "coder", Done: true, CostUSD: 0.45, DurationMs: 12300},
				{Role: "reviewer", Done: true, CostUSD: 0.32, DurationMs: 8100, IssueCount: 2},
			},
		},
	}
	lv.Width = 80

	view := lv.View()

	if !strings.Contains(view, "├──") {
		t.Error("expected ├── connector for non-last agent")
	}
	if !strings.Contains(view, "└──") {
		t.Error("expected └── connector for last agent")
	}
}

func TestLoopViewView_SingleAgent(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.Cycles = []CycleEntry{
		{
			Number: 1,
			Agents: []AgentEntry{
				{Role: "coder", Done: true, CostUSD: 0.10, DurationMs: 5000},
			},
		},
	}

	view := lv.View()

	// Single agent should use └── (last connector).
	if !strings.Contains(view, "└──") {
		t.Error("expected └── connector for single agent")
	}
	// Should not have ├── since there's only one agent.
	if strings.Contains(view, "├──") {
		t.Error("should not have ├── for single agent")
	}
}

func TestLoopViewView_WorkingAgent(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.Cycles = []CycleEntry{
		{
			Number: 1,
			Agents: []AgentEntry{
				{Role: "coder", Done: false, StartedAt: time.Now()},
			},
		},
	}

	view := lv.View()

	if !strings.Contains(view, "working") {
		t.Error("expected 'working' for active agent")
	}
	if !strings.Contains(view, "└──") {
		t.Error("expected tree connector for working agent")
	}
}

func TestLoopViewView_MultipleCycles(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.Cycles = []CycleEntry{
		{
			Number: 1,
			Agents: []AgentEntry{
				{Role: "coder", Done: true, CostUSD: 0.45, DurationMs: 12000},
				{Role: "reviewer", Done: true, CostUSD: 0.32, DurationMs: 8000, IssueCount: 1},
			},
		},
		{
			Number: 2,
			Agents: []AgentEntry{
				{Role: "coder", Done: false, StartedAt: time.Now()},
			},
		},
	}

	view := lv.View()

	if !strings.Contains(view, "Cycle 1") {
		t.Error("expected Cycle 1 header")
	}
	if !strings.Contains(view, "Cycle 2") {
		t.Error("expected Cycle 2 header")
	}
}

func TestLoopViewView_IssueCount(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.Cycles = []CycleEntry{
		{
			Number: 1,
			Agents: []AgentEntry{
				{Role: "reviewer", Done: true, CostUSD: 0.32, DurationMs: 8000, IssueCount: 3},
			},
		},
	}

	view := lv.View()

	if !strings.Contains(view, "3 issue(s)") {
		t.Error("expected issue count in reviewer output")
	}
}

func TestLoopViewView_EmptyCycles(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()

	view := lv.View()

	if view != "" {
		t.Errorf("expected empty view for no cycles, got: %q", view)
	}
}

func TestLoopViewView_CycleHeaderSelection(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.Cycles = []CycleEntry{
		{Number: 1, Agents: []AgentEntry{{Role: "coder", Done: true, DurationMs: 1000}}},
	}
	lv.Cursor = 0 // Cycle header selected.

	view := lv.View()

	if !strings.Contains(view, selectionIndicator) {
		t.Error("expected selection indicator on cycle header")
	}
}

func TestLoopViewView_AgentSelection(t *testing.T) {
	t.Parallel()
	lv := NewLoopView()
	lv.Cycles = []CycleEntry{
		{Number: 1, Agents: []AgentEntry{{Role: "coder", Done: true, DurationMs: 1000}}},
	}
	lv.Cursor = 1 // First agent selected.

	view := lv.View()

	if !strings.Contains(view, selectionIndicator) {
		t.Error("expected selection indicator on agent entry")
	}
}
