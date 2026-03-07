package tui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/papapumpkin/quasar/internal/bus"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/tycho"
	"github.com/papapumpkin/quasar/internal/ui"
)

// ── Mock bus/subscription for testing ────────────────────────────────────────

// mockBus implements bus.Bus for testing.
type mockBus struct {
	ch     chan bus.Event
	closed bool
}

func newMockBus() *mockBus {
	return &mockBus{ch: make(chan bus.Event, 64)}
}

func (b *mockBus) Publish(_ context.Context, ev bus.Event) error { b.ch <- ev; return nil }
func (b *mockBus) Subscribe(_ string, _ int) bus.Subscription    { return &mockSub{ch: b.ch} }
func (b *mockBus) Close() error                                  { close(b.ch); b.closed = true; return nil }

// mockSub implements bus.Subscription for testing.
type mockSub struct {
	ch chan bus.Event
}

func (s *mockSub) Events() <-chan bus.Event { return s.ch }
func (s *mockSub) Unsubscribe()             {}

// ── mapEvent tests ───────────────────────────────────────────────────────────

func TestMapEvent_PhaseLifecycle(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	tests := []struct {
		name  string
		event bus.Event
		check func(t *testing.T, msg tea.Msg)
	}{
		{
			name:  "PhaseTaskStarted",
			event: bus.Event{Kind: bus.KindPhaseTaskStarted, PhaseID: "p1", BeadID: "b1", Title: "task"},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgPhaseTaskStarted)
				if !ok {
					t.Fatalf("expected MsgPhaseTaskStarted, got %T", msg)
				}
				if m.PhaseID != "p1" || m.BeadID != "b1" || m.Title != "task" {
					t.Errorf("fields mismatch: %+v", m)
				}
			},
		},
		{
			name:  "PhaseTaskComplete",
			event: bus.Event{Kind: bus.KindPhaseTaskComplete, PhaseID: "p1", BeadID: "b1", CostUSD: 1.5},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgPhaseTaskComplete)
				if !ok {
					t.Fatalf("expected MsgPhaseTaskComplete, got %T", msg)
				}
				if m.PhaseID != "p1" || m.TotalCost != 1.5 {
					t.Errorf("fields mismatch: %+v", m)
				}
			},
		},
		{
			name:  "PhaseCycleStart",
			event: bus.Event{Kind: bus.KindPhaseCycleStart, PhaseID: "p1", Cycle: 2, MaxCycles: 5},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgPhaseCycleStart)
				if !ok {
					t.Fatalf("expected MsgPhaseCycleStart, got %T", msg)
				}
				if m.Cycle != 2 || m.MaxCycles != 5 {
					t.Errorf("fields mismatch: %+v", m)
				}
			},
		},
		{
			name:  "PhaseAgentStart",
			event: bus.Event{Kind: bus.KindPhaseAgentStart, PhaseID: "p1", Role: "coder"},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgPhaseAgentStart)
				if !ok {
					t.Fatalf("expected MsgPhaseAgentStart, got %T", msg)
				}
				if m.Role != "coder" {
					t.Errorf("role = %q, want coder", m.Role)
				}
			},
		},
		{
			name:  "PhaseAgentDone",
			event: bus.Event{Kind: bus.KindPhaseAgentDone, PhaseID: "p1", Role: "coder", CostUSD: 0.5, DurationMs: 1000, Tokens: 500},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgPhaseAgentDone)
				if !ok {
					t.Fatalf("expected MsgPhaseAgentDone, got %T", msg)
				}
				if m.CostUSD != 0.5 || m.DurationMs != 1000 || m.Tokens != 500 {
					t.Errorf("fields mismatch: %+v", m)
				}
			},
		},
		{
			name:  "PhaseAgentOutput",
			event: bus.Event{Kind: bus.KindPhaseAgentOutput, PhaseID: "p1", Role: "coder", Cycle: 1, Message: "output"},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgPhaseAgentOutput)
				if !ok {
					t.Fatalf("expected MsgPhaseAgentOutput, got %T", msg)
				}
				if m.Output != "output" {
					t.Errorf("output = %q, want output", m.Output)
				}
			},
		},
		{
			name:  "PhaseIssuesFound",
			event: bus.Event{Kind: bus.KindPhaseIssuesFound, PhaseID: "p1", Count: 3},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgPhaseIssuesFound)
				if !ok {
					t.Fatalf("expected MsgPhaseIssuesFound, got %T", msg)
				}
				if m.Count != 3 {
					t.Errorf("count = %d, want 3", m.Count)
				}
			},
		},
		{
			name:  "PhaseApproved",
			event: bus.Event{Kind: bus.KindPhaseApproved, PhaseID: "p1"},
			check: func(t *testing.T, msg tea.Msg) {
				if _, ok := msg.(MsgPhaseApproved); !ok {
					t.Fatalf("expected MsgPhaseApproved, got %T", msg)
				}
			},
		},
		{
			name:  "PhaseError",
			event: bus.Event{Kind: bus.KindPhaseError, PhaseID: "p1", Message: "boom"},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgPhaseError)
				if !ok {
					t.Fatalf("expected MsgPhaseError, got %T", msg)
				}
				if m.Msg != "boom" {
					t.Errorf("msg = %q, want boom", m.Msg)
				}
			},
		},
		{
			name:  "PhaseRefactorPending",
			event: bus.Event{Kind: bus.KindPhaseRefactorPending, PhaseID: "p1"},
			check: func(t *testing.T, msg tea.Msg) {
				if _, ok := msg.(MsgPhaseRefactorPending); !ok {
					t.Fatalf("expected MsgPhaseRefactorPending, got %T", msg)
				}
			},
		},
		{
			name:  "PhaseRefactorApplied",
			event: bus.Event{Kind: bus.KindPhaseRefactorApplied, PhaseID: "p1"},
			check: func(t *testing.T, msg tea.Msg) {
				if _, ok := msg.(MsgPhaseRefactorApplied); !ok {
					t.Fatalf("expected MsgPhaseRefactorApplied, got %T", msg)
				}
			},
		},
		{
			name:  "PhaseScanning",
			event: bus.Event{Kind: bus.KindPhaseScanning, PhaseID: "p1"},
			check: func(t *testing.T, msg tea.Msg) {
				if _, ok := msg.(MsgPhaseScanning); !ok {
					t.Fatalf("expected MsgPhaseScanning, got %T", msg)
				}
			},
		},
		{
			name:  "PhaseFindingLifecycle_nil",
			event: bus.Event{Kind: bus.KindPhaseFindingLifecycle, PhaseID: "p1"},
			check: func(t *testing.T, msg tea.Msg) {
				if msg != nil {
					t.Fatalf("expected nil, got %T", msg)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := sub.mapEvent(tc.event)
			tc.check(t, msg)
		})
	}
}

func TestMapEvent_SingleTaskLifecycle(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	tests := []struct {
		name  string
		event bus.Event
		check func(t *testing.T, msg tea.Msg)
	}{
		{
			name:  "TaskStarted",
			event: bus.Event{Kind: bus.KindTaskStarted, BeadID: "b1", Title: "task"},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgTaskStarted)
				if !ok {
					t.Fatalf("expected MsgTaskStarted, got %T", msg)
				}
				if m.BeadID != "b1" || m.Title != "task" {
					t.Errorf("fields mismatch: %+v", m)
				}
			},
		},
		{
			name:  "TaskComplete",
			event: bus.Event{Kind: bus.KindTaskComplete, BeadID: "b1", CostUSD: 2.0},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgTaskComplete)
				if !ok {
					t.Fatalf("expected MsgTaskComplete, got %T", msg)
				}
				if m.TotalCost != 2.0 {
					t.Errorf("total cost = %f, want 2.0", m.TotalCost)
				}
			},
		},
		{
			name:  "CycleStart",
			event: bus.Event{Kind: bus.KindCycleStart, Cycle: 1, MaxCycles: 3},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgCycleStart)
				if !ok {
					t.Fatalf("expected MsgCycleStart, got %T", msg)
				}
				if m.Cycle != 1 || m.MaxCycles != 3 {
					t.Errorf("fields mismatch: %+v", m)
				}
			},
		},
		{
			name:  "AgentStart",
			event: bus.Event{Kind: bus.KindAgentStart, Role: "reviewer"},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgAgentStart)
				if !ok {
					t.Fatalf("expected MsgAgentStart, got %T", msg)
				}
				if m.Role != "reviewer" {
					t.Errorf("role = %q, want reviewer", m.Role)
				}
			},
		},
		{
			name:  "AgentDone",
			event: bus.Event{Kind: bus.KindAgentDone, Role: "coder", CostUSD: 0.3, DurationMs: 500, Tokens: 100},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgAgentDone)
				if !ok {
					t.Fatalf("expected MsgAgentDone, got %T", msg)
				}
				if m.CostUSD != 0.3 || m.DurationMs != 500 || m.Tokens != 100 {
					t.Errorf("fields mismatch: %+v", m)
				}
			},
		},
		{
			name:  "AgentOutput",
			event: bus.Event{Kind: bus.KindAgentOutput, Role: "coder", Cycle: 1, Message: "output text"},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgAgentOutput)
				if !ok {
					t.Fatalf("expected MsgAgentOutput, got %T", msg)
				}
				if m.Output != "output text" {
					t.Errorf("output = %q, want 'output text'", m.Output)
				}
			},
		},
		{
			name:  "IssuesFound",
			event: bus.Event{Kind: bus.KindIssuesFound, Count: 2},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgIssuesFound)
				if !ok {
					t.Fatalf("expected MsgIssuesFound, got %T", msg)
				}
				if m.Count != 2 {
					t.Errorf("count = %d, want 2", m.Count)
				}
			},
		},
		{
			name:  "Approved",
			event: bus.Event{Kind: bus.KindApproved},
			check: func(t *testing.T, msg tea.Msg) {
				if _, ok := msg.(MsgApproved); !ok {
					t.Fatalf("expected MsgApproved, got %T", msg)
				}
			},
		},
		{
			name:  "MaxCyclesReached",
			event: bus.Event{Kind: bus.KindMaxCyclesReached, MaxCycles: 5},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgMaxCyclesReached)
				if !ok {
					t.Fatalf("expected MsgMaxCyclesReached, got %T", msg)
				}
				if m.Max != 5 {
					t.Errorf("max = %d, want 5", m.Max)
				}
			},
		},
		{
			name:  "BudgetExceeded",
			event: bus.Event{Kind: bus.KindBudgetExceeded, Spent: 10.0, Limit: 8.0},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgBudgetExceeded)
				if !ok {
					t.Fatalf("expected MsgBudgetExceeded, got %T", msg)
				}
				if m.Spent != 10.0 || m.Limit != 8.0 {
					t.Errorf("fields mismatch: %+v", m)
				}
			},
		},
		{
			name:  "Error",
			event: bus.Event{Kind: bus.KindError, Message: "fail"},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgError)
				if !ok {
					t.Fatalf("expected MsgError, got %T", msg)
				}
				if m.Msg != "fail" {
					t.Errorf("msg = %q, want fail", m.Msg)
				}
			},
		},
		{
			name:  "Info",
			event: bus.Event{Kind: bus.KindInfo, Message: "info"},
			check: func(t *testing.T, msg tea.Msg) {
				m, ok := msg.(MsgInfo)
				if !ok {
					t.Fatalf("expected MsgInfo, got %T", msg)
				}
				if m.Msg != "info" {
					t.Errorf("msg = %q, want info", m.Msg)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := sub.mapEvent(tc.event)
			tc.check(t, msg)
		})
	}
}

// ── Hail mapping tests ──────────────────────────────────────────────────────

func TestMapEvent_HailReceived_MapsToMsgHailReceived(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	hailInfo := ui.HailInfo{
		ID:         "h1",
		Kind:       "decision_needed",
		Cycle:      2,
		SourceRole: "coder",
		Summary:    "Need help",
		Detail:     "What should I do?",
		Options:    []string{"A", "B"},
	}

	t.Run("KindHailReceived", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{
			Kind: bus.KindHailReceived,
			Hail: &bus.HailPayload{Discovery: hailInfo},
		}
		msg := sub.mapEvent(ev)
		m, ok := msg.(MsgHailReceived)
		if !ok {
			t.Fatalf("expected MsgHailReceived, got %T", msg)
		}
		if m.Hail.ID != "h1" || m.Hail.Kind != "decision_needed" || m.Hail.Summary != "Need help" {
			t.Errorf("HailInfo mismatch: %+v", m.Hail)
		}
	})

	t.Run("KindPhaseHailReceived", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{
			Kind:    bus.KindPhaseHailReceived,
			PhaseID: "p1",
			Hail:    &bus.HailPayload{Discovery: hailInfo},
		}
		msg := sub.mapEvent(ev)
		m, ok := msg.(MsgHailReceived)
		if !ok {
			t.Fatalf("expected MsgHailReceived, got %T", msg)
		}
		if m.PhaseID != "p1" {
			t.Errorf("PhaseID = %q, want p1", m.PhaseID)
		}
		if m.Hail.ID != "h1" {
			t.Errorf("HailInfo.ID = %q, want h1", m.Hail.ID)
		}
	})

	t.Run("KindHailReceived_nil_payload", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{Kind: bus.KindHailReceived}
		msg := sub.mapEvent(ev)
		if msg != nil {
			t.Fatalf("expected nil for nil payload, got %T", msg)
		}
	})

	t.Run("KindHailReceived_wrong_type", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{
			Kind: bus.KindHailReceived,
			Hail: &bus.HailPayload{Discovery: "not a HailInfo"},
		}
		msg := sub.mapEvent(ev)
		if msg != nil {
			t.Fatalf("expected nil for wrong Discovery type, got %T", msg)
		}
	})
}

func TestMapEvent_Hail_MapsToMsgHail(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	t.Run("valid_discovery", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{
			Kind:    bus.KindHail,
			PhaseID: "p1",
			Hail: &bus.HailPayload{
				Discovery: fabric.Discovery{Kind: "ambiguity", Detail: "unclear spec"},
			},
		}
		msg := sub.mapEvent(ev)
		m, ok := msg.(MsgHail)
		if !ok {
			t.Fatalf("expected MsgHail, got %T", msg)
		}
		if m.PhaseID != "p1" || m.Discovery.Kind != "ambiguity" {
			t.Errorf("fields mismatch: %+v", m)
		}
		if m.ResponseCh != nil {
			t.Error("expected nil ResponseCh when bus has no ResponseCh")
		}
	})

	t.Run("with_response_channel", func(t *testing.T) {
		t.Parallel()
		responseCh := make(chan any, 1)
		ev := bus.Event{
			Kind:    bus.KindHail,
			PhaseID: "p1",
			Hail: &bus.HailPayload{
				Discovery:  fabric.Discovery{Kind: "decision"},
				ResponseCh: responseCh,
			},
		}
		msg := sub.mapEvent(ev)
		m, ok := msg.(MsgHail)
		if !ok {
			t.Fatalf("expected MsgHail, got %T", msg)
		}
		if m.ResponseCh == nil {
			t.Fatal("expected non-nil ResponseCh")
		}
		// Send a response through the typed channel and verify it arrives.
		m.ResponseCh <- "approved"
		select {
		case got := <-responseCh:
			if got != "approved" {
				t.Errorf("forwarded response = %v, want 'approved'", got)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for forwarded response")
		}
	})

	t.Run("nil_payload", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{Kind: bus.KindHail}
		msg := sub.mapEvent(ev)
		if msg != nil {
			t.Fatalf("expected nil, got %T", msg)
		}
	})

	t.Run("wrong_discovery_type", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{
			Kind: bus.KindHail,
			Hail: &bus.HailPayload{Discovery: 42},
		}
		msg := sub.mapEvent(ev)
		if msg != nil {
			t.Fatalf("expected nil for wrong Discovery type, got %T", msg)
		}
	})
}

// ── Rich payload mapping tests ───────────────────────────────────────────────

func TestMapEvent_CycleSummary(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	t.Run("phase", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{
			Kind:    bus.KindPhaseCycleSummary,
			PhaseID: "p1",
			CycleSummary: &bus.CycleSummaryPayload{
				Cycle:     2,
				MaxCycles: 5,
				Phase:     "review",
				CostUSD:   0.5,
				Approved:  true,
			},
		}
		msg := sub.mapEvent(ev)
		m, ok := msg.(MsgPhaseCycleSummary)
		if !ok {
			t.Fatalf("expected MsgPhaseCycleSummary, got %T", msg)
		}
		if m.Data.Cycle != 2 || !m.Data.Approved || m.Data.Phase != "review" {
			t.Errorf("fields mismatch: %+v", m.Data)
		}
	})

	t.Run("loop", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{
			Kind: bus.KindCycleSummary,
			CycleSummary: &bus.CycleSummaryPayload{
				Cycle:     1,
				MaxCycles: 3,
				CostUSD:   0.2,
			},
		}
		msg := sub.mapEvent(ev)
		m, ok := msg.(MsgCycleSummary)
		if !ok {
			t.Fatalf("expected MsgCycleSummary, got %T", msg)
		}
		if m.Data.Cycle != 1 || m.Data.MaxCycles != 3 {
			t.Errorf("fields mismatch: %+v", m.Data)
		}
	})

	t.Run("nil_payload", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{Kind: bus.KindPhaseCycleSummary}
		if msg := sub.mapEvent(ev); msg != nil {
			t.Fatalf("expected nil, got %T", msg)
		}
	})
}

func TestMapEvent_AgentDiff(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	t.Run("phase", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{
			Kind:    bus.KindPhaseAgentDiff,
			PhaseID: "p1",
			Role:    "coder",
			Cycle:   1,
			AgentDiff: &bus.AgentDiffPayload{
				Diff:    "--- a/f\n+++ b/f",
				BaseRef: "abc",
				HeadRef: "def",
				Files:   []string{"file.go"},
				WorkDir: "/tmp",
			},
		}
		msg := sub.mapEvent(ev)
		m, ok := msg.(MsgPhaseAgentDiff)
		if !ok {
			t.Fatalf("expected MsgPhaseAgentDiff, got %T", msg)
		}
		if m.Diff != "--- a/f\n+++ b/f" || m.BaseRef != "abc" || len(m.Files) != 1 {
			t.Errorf("fields mismatch: %+v", m)
		}
	})

	t.Run("loop", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{
			Kind: bus.KindAgentDiff,
			Role: "coder",
			AgentDiff: &bus.AgentDiffPayload{
				Diff: "diff",
			},
		}
		msg := sub.mapEvent(ev)
		m, ok := msg.(MsgAgentDiff)
		if !ok {
			t.Fatalf("expected MsgAgentDiff, got %T", msg)
		}
		if m.Diff != "diff" {
			t.Errorf("diff = %q, want diff", m.Diff)
		}
	})

	t.Run("nil_payload", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{Kind: bus.KindPhaseAgentDiff}
		if msg := sub.mapEvent(ev); msg != nil {
			t.Fatalf("expected nil, got %T", msg)
		}
	})
}

func TestMapEvent_BeadUpdate(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	root := &BeadInfo{ID: "b1", Title: "task", Status: "open", Type: "task"}

	t.Run("phase", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{
			Kind:    bus.KindPhaseBeadUpdate,
			PhaseID: "p1",
			BeadTree: &bus.BeadTreePayload{
				TaskBeadID: "b1",
				Root:       root,
			},
		}
		msg := sub.mapEvent(ev)
		m, ok := msg.(MsgPhaseBeadUpdate)
		if !ok {
			t.Fatalf("expected MsgPhaseBeadUpdate, got %T", msg)
		}
		if m.TaskBeadID != "b1" || m.Root.ID != "b1" {
			t.Errorf("fields mismatch: %+v", m)
		}
	})

	t.Run("loop", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{
			Kind: bus.KindBeadUpdate,
			BeadTree: &bus.BeadTreePayload{
				TaskBeadID: "b2",
				Root:       root,
			},
		}
		msg := sub.mapEvent(ev)
		m, ok := msg.(MsgBeadUpdate)
		if !ok {
			t.Fatalf("expected MsgBeadUpdate, got %T", msg)
		}
		if m.TaskBeadID != "b2" {
			t.Errorf("TaskBeadID = %q, want b2", m.TaskBeadID)
		}
	})

	t.Run("nil_payload", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{Kind: bus.KindPhaseBeadUpdate}
		if msg := sub.mapEvent(ev); msg != nil {
			t.Fatalf("expected nil, got %T", msg)
		}
	})

	t.Run("wrong_root_type", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{
			Kind: bus.KindPhaseBeadUpdate,
			BeadTree: &bus.BeadTreePayload{
				TaskBeadID: "b1",
				Root:       "not-a-bead-info",
			},
		}
		if msg := sub.mapEvent(ev); msg != nil {
			t.Fatalf("expected nil for wrong Root type, got %T", msg)
		}
	})
}

func TestMapEvent_HotAdded(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{
			Kind:    bus.KindPhaseHotAdded,
			PhaseID: "p1",
			HotAdd:  &bus.HotAddPayload{Title: "hot fix", DependsOn: []string{"p0"}},
		}
		msg := sub.mapEvent(ev)
		m, ok := msg.(MsgPhaseHotAdded)
		if !ok {
			t.Fatalf("expected MsgPhaseHotAdded, got %T", msg)
		}
		if m.Title != "hot fix" || len(m.DependsOn) != 1 {
			t.Errorf("fields mismatch: %+v", m)
		}
	})

	t.Run("nil_payload", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{Kind: bus.KindPhaseHotAdded}
		if msg := sub.mapEvent(ev); msg != nil {
			t.Fatalf("expected nil, got %T", msg)
		}
	})
}

// ── Nebula control mapping tests ─────────────────────────────────────────────

func TestMapEvent_NebulaProgress(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	ev := bus.Event{
		Kind: bus.KindNebulaProgress,
		Progress: &bus.ProgressPayload{
			Completed:    3,
			Total:        10,
			OpenBeads:    7,
			ClosedBeads:  3,
			TotalCostUSD: 4.5,
		},
	}
	msg := sub.mapEvent(ev)
	m, ok := msg.(MsgNebulaProgress)
	if !ok {
		t.Fatalf("expected MsgNebulaProgress, got %T", msg)
	}
	if m.Completed != 3 || m.Total != 10 || m.TotalCostUSD != 4.5 {
		t.Errorf("fields mismatch: %+v", m)
	}
}

func TestMapEvent_NebulaDone(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{
			Kind: bus.KindNebulaDone,
			DoneResults: &bus.DonePayload{
				Results: []any{nebula.WorkerResult{PhaseID: "p1"}},
			},
		}
		msg := sub.mapEvent(ev)
		m, ok := msg.(MsgNebulaDone)
		if !ok {
			t.Fatalf("expected MsgNebulaDone, got %T", msg)
		}
		if len(m.Results) != 1 || m.Results[0].PhaseID != "p1" {
			t.Errorf("results mismatch: %+v", m.Results)
		}
	})

	t.Run("nil_payload", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{Kind: bus.KindNebulaDone}
		if msg := sub.mapEvent(ev); msg != nil {
			t.Fatalf("expected nil, got %T", msg)
		}
	})
}

func TestMapEvent_GatePrompt(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	t.Run("valid_checkpoint", func(t *testing.T) {
		t.Parallel()
		responseCh := make(chan any, 1)
		checkpoint := &nebula.Checkpoint{PhaseID: "p1"}
		ev := bus.Event{
			Kind: bus.KindGatePrompt,
			GatePrompt: &bus.GatePromptPayload{
				Checkpoint: checkpoint,
				ResponseCh: responseCh,
			},
		}
		msg := sub.mapEvent(ev)
		m, ok := msg.(MsgGatePrompt)
		if !ok {
			t.Fatalf("expected MsgGatePrompt, got %T", msg)
		}
		if m.Checkpoint.PhaseID != "p1" {
			t.Errorf("PhaseID = %q, want p1", m.Checkpoint.PhaseID)
		}
		if m.ResponseCh == nil {
			t.Fatal("expected non-nil ResponseCh")
		}
		// Send a gate action and verify forwarding.
		m.ResponseCh <- nebula.GateActionAccept
		select {
		case got := <-responseCh:
			if got != nebula.GateActionAccept {
				t.Errorf("forwarded action = %v, want GateApprove", got)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for forwarded gate action")
		}
	})

	t.Run("nil_payload", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{Kind: bus.KindGatePrompt}
		if msg := sub.mapEvent(ev); msg != nil {
			t.Fatalf("expected nil, got %T", msg)
		}
	})

	t.Run("wrong_checkpoint_type", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{
			Kind: bus.KindGatePrompt,
			GatePrompt: &bus.GatePromptPayload{
				Checkpoint: "not a checkpoint",
			},
		}
		if msg := sub.mapEvent(ev); msg != nil {
			t.Fatalf("expected nil for wrong Checkpoint type, got %T", msg)
		}
	})
}

func TestMapEvent_GateResolved(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	ev := bus.Event{
		Kind:    bus.KindGateResolved,
		PhaseID: "p1",
		GateResolved: &bus.GateResolvedPayload{
			Action: string(nebula.GateActionAccept),
		},
	}
	msg := sub.mapEvent(ev)
	m, ok := msg.(MsgGateResolved)
	if !ok {
		t.Fatalf("expected MsgGateResolved, got %T", msg)
	}
	if m.Action != nebula.GateActionAccept {
		t.Errorf("action = %v, want GateActionAccept", m.Action)
	}
}

func TestMapEvent_HealingAttempt(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	ev := bus.Event{
		Kind: bus.KindHealingAttempt,
		Healing: &bus.HealingPayload{
			FailedPhaseID:    "p1",
			FailureKind:      "test_failure",
			RemediationID:    "r1",
			RemediationTitle: "fix tests",
		},
	}
	msg := sub.mapEvent(ev)
	m, ok := msg.(MsgHealingAttempt)
	if !ok {
		t.Fatalf("expected MsgHealingAttempt, got %T", msg)
	}
	if m.FailedPhaseID != "p1" || m.RemediationID != "r1" {
		t.Errorf("fields mismatch: %+v", m)
	}
}

func TestMapEvent_HailResolved(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	t.Run("phase", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{
			Kind:         bus.KindPhaseHailResolved,
			PhaseID:      "p1",
			HailResolved: &bus.HailResolvedPayload{ID: "h1", Resolution: "approved"},
		}
		msg := sub.mapEvent(ev)
		m, ok := msg.(MsgHailResolved)
		if !ok {
			t.Fatalf("expected MsgHailResolved, got %T", msg)
		}
		if m.PhaseID != "p1" || m.ID != "h1" || m.Resolution != "approved" {
			t.Errorf("fields mismatch: %+v", m)
		}
	})

	t.Run("loop", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{
			Kind:         bus.KindHailResolved,
			HailResolved: &bus.HailResolvedPayload{ID: "h2", Resolution: "rejected"},
		}
		msg := sub.mapEvent(ev)
		m, ok := msg.(MsgHailResolved)
		if !ok {
			t.Fatalf("expected MsgHailResolved, got %T", msg)
		}
		if m.ID != "h2" || m.Resolution != "rejected" {
			t.Errorf("fields mismatch: %+v", m)
		}
	})
}

func TestMapEvent_PhaseActivity(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	ev := bus.Event{
		Kind:    bus.KindPhaseActivity,
		PhaseID: "p1",
		Message: "reading foo.go",
	}
	msg := sub.mapEvent(ev)
	m, ok := msg.(MsgWorkerActivity)
	if !ok {
		t.Fatalf("expected MsgWorkerActivity, got %T", msg)
	}
	if m.PhaseID != "p1" {
		t.Errorf("PhaseID = %q, want %q", m.PhaseID, "p1")
	}
	if m.Activity != "reading foo.go" {
		t.Errorf("Activity = %q, want %q", m.Activity, "reading foo.go")
	}
}

func TestMapEvent_StaleWarning(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		items := []tycho.StaleItem{{Kind: "claim", ID: "file.go"}}
		ev := bus.Event{
			Kind:         bus.KindStaleWarning,
			StaleWarning: &bus.StaleWarningPayload{Items: items},
		}
		msg := sub.mapEvent(ev)
		m, ok := msg.(MsgStaleWarning)
		if !ok {
			t.Fatalf("expected MsgStaleWarning, got %T", msg)
		}
		if len(m.Items) != 1 || m.Items[0].ID != "file.go" {
			t.Errorf("items mismatch: %+v", m.Items)
		}
	})

	t.Run("nil_payload", func(t *testing.T) {
		t.Parallel()
		ev := bus.Event{Kind: bus.KindStaleWarning}
		if msg := sub.mapEvent(ev); msg != nil {
			t.Fatalf("expected nil, got %T", msg)
		}
	})
}

func TestMapEvent_ScratchpadEntry(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	now := time.Now()
	ev := bus.Event{
		Kind:      bus.KindScratchpadEntry,
		Timestamp: now,
		PhaseID:   "p1",
		Message:   "note",
	}
	msg := sub.mapEvent(ev)
	m, ok := msg.(MsgScratchpadEntry)
	if !ok {
		t.Fatalf("expected MsgScratchpadEntry, got %T", msg)
	}
	if m.PhaseID != "p1" || m.Text != "note" || m.Timestamp != now {
		t.Errorf("fields mismatch: %+v", m)
	}
}

func TestMapEvent_UnknownKind_ReturnsNil(t *testing.T) {
	t.Parallel()
	sub := &BusSubscriber{done: make(chan struct{})}

	ev := bus.Event{Kind: "unknown.kind.here"}
	if msg := sub.mapEvent(ev); msg != nil {
		t.Fatalf("expected nil for unknown kind, got %T", msg)
	}
}

// ── Goroutine leak tests ─────────────────────────────────────────────────────

func TestForwardGateAction_ExitsOnDone(t *testing.T) {
	t.Parallel()
	from := make(chan nebula.GateAction, 1)
	to := make(chan any, 1)
	done := make(chan struct{})

	finished := make(chan struct{})
	go func() {
		forwardGateAction(from, to, done)
		close(finished)
	}()

	// Close done without sending a response — goroutine should exit.
	close(done)
	select {
	case <-finished:
		// Success: goroutine exited.
	case <-time.After(time.Second):
		t.Fatal("forwardGateAction did not exit after done closed")
	}

	// Verify nothing was forwarded.
	select {
	case v := <-to:
		t.Fatalf("unexpected forwarded value: %v", v)
	default:
	}
}

func TestForwardHailResponse_ExitsOnDone(t *testing.T) {
	t.Parallel()
	from := make(chan string, 1)
	to := make(chan any, 1)
	done := make(chan struct{})

	finished := make(chan struct{})
	go func() {
		forwardHailResponse(from, to, done)
		close(finished)
	}()

	// Close done without sending a response — goroutine should exit.
	close(done)
	select {
	case <-finished:
		// Success: goroutine exited.
	case <-time.After(time.Second):
		t.Fatal("forwardHailResponse did not exit after done closed")
	}
}

func TestForwardGateAction_Forwards(t *testing.T) {
	t.Parallel()
	from := make(chan nebula.GateAction, 1)
	to := make(chan any, 1)
	done := make(chan struct{})

	finished := make(chan struct{})
	go func() {
		forwardGateAction(from, to, done)
		close(finished)
	}()

	from <- nebula.GateActionAccept
	select {
	case got := <-to:
		if got != nebula.GateActionAccept {
			t.Errorf("forwarded = %v, want GateApprove", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for forwarded gate action")
	}

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("goroutine did not exit after forwarding")
	}
}

func TestForwardHailResponse_Forwards(t *testing.T) {
	t.Parallel()
	from := make(chan string, 1)
	to := make(chan any, 1)
	done := make(chan struct{})

	finished := make(chan struct{})
	go func() {
		forwardHailResponse(from, to, done)
		close(finished)
	}()

	from <- "response"
	select {
	case got := <-to:
		if got != "response" {
			t.Errorf("forwarded = %v, want 'response'", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for forwarded hail response")
	}

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("goroutine did not exit after forwarding")
	}
}

// ── Lifecycle tests ──────────────────────────────────────────────────────────

func TestBusSubscriber_StartStop(t *testing.T) {
	t.Parallel()
	mb := newMockBus()
	model := NewAppModel(ModeLoop)
	model.Detail = NewDetailPanel(80, 10)
	p := tea.NewProgram(model, tea.WithoutSignalHandler())

	sub := NewBusSubscriber(p, mb, 0)
	sub.Start()

	// Stop should unsubscribe and wait for the goroutine to exit.
	// Close the bus to unblock the goroutine.
	mb.Close()

	done := make(chan struct{})
	go func() {
		sub.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success.
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return in time — possible goroutine leak")
	}
}
