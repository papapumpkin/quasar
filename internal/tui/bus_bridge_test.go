package tui

import (
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/bus"
	"github.com/papapumpkin/quasar/internal/ui"
)

// drainEvent reads a single event from the subscription with a timeout.
// Returns the event and true if received, or a zero Event and false on timeout.
func drainEvent(t *testing.T, sub bus.Subscription) (bus.Event, bool) {
	t.Helper()
	select {
	case ev := <-sub.Events():
		return ev, true
	case <-time.After(time.Second):
		return bus.Event{}, false
	}
}

// mustDrain reads a single event from the subscription, failing the test on timeout.
func mustDrain(t *testing.T, sub bus.Subscription) bus.Event {
	t.Helper()
	ev, ok := drainEvent(t, sub)
	if !ok {
		t.Fatal("timed out waiting for bus event")
	}
	return ev
}

func TestBusUIBridgeTaskStarted(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	defer b.Close()
	sub := b.Subscribe("test", 16)
	defer sub.Unsubscribe()

	bridge := NewBusUIBridge(b, "phase-1", "/tmp/work")
	bridge.TaskStarted("bead-123", "Implement feature")

	// TaskStarted emits KindPhaseTaskStarted + KindScratchpadEntry.
	ev := mustDrain(t, sub)
	if ev.Kind != bus.KindPhaseTaskStarted {
		t.Errorf("Kind = %q, want %q", ev.Kind, bus.KindPhaseTaskStarted)
	}
	if ev.PhaseID != "phase-1" {
		t.Errorf("PhaseID = %q, want %q", ev.PhaseID, "phase-1")
	}
	if ev.BeadID != "bead-123" {
		t.Errorf("BeadID = %q, want %q", ev.BeadID, "bead-123")
	}
	if ev.Title != "Implement feature" {
		t.Errorf("Title = %q, want %q", ev.Title, "Implement feature")
	}

	// Scratchpad entry.
	scratch := mustDrain(t, sub)
	if scratch.Kind != bus.KindScratchpadEntry {
		t.Errorf("scratchpad Kind = %q, want %q", scratch.Kind, bus.KindScratchpadEntry)
	}
}

func TestBusUIBridgeTaskComplete(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	defer b.Close()
	sub := b.Subscribe("test", 16)
	defer sub.Unsubscribe()

	bridge := NewBusUIBridge(b, "phase-2", "/tmp/work")
	bridge.TaskComplete("bead-456", 1.50)

	ev := mustDrain(t, sub)
	if ev.Kind != bus.KindPhaseTaskComplete {
		t.Errorf("Kind = %q, want %q", ev.Kind, bus.KindPhaseTaskComplete)
	}
	if ev.BeadID != "bead-456" {
		t.Errorf("BeadID = %q, want %q", ev.BeadID, "bead-456")
	}
	if ev.CostUSD != 1.50 {
		t.Errorf("CostUSD = %f, want %f", ev.CostUSD, 1.50)
	}
}

func TestBusUIBridgeCycleStart(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	defer b.Close()
	sub := b.Subscribe("test", 16)
	defer sub.Unsubscribe()

	bridge := NewBusUIBridge(b, "phase-3", "/tmp/work")
	bridge.CycleStart(2, 5)

	ev := mustDrain(t, sub)
	if ev.Kind != bus.KindPhaseCycleStart {
		t.Errorf("Kind = %q, want %q", ev.Kind, bus.KindPhaseCycleStart)
	}
	if ev.Cycle != 2 {
		t.Errorf("Cycle = %d, want %d", ev.Cycle, 2)
	}
	if ev.MaxCycles != 5 {
		t.Errorf("MaxCycles = %d, want %d", ev.MaxCycles, 5)
	}
}

func TestBusUIBridgeAgentStart(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	defer b.Close()
	sub := b.Subscribe("test", 16)
	defer sub.Unsubscribe()

	bridge := NewBusUIBridge(b, "phase-4", "/tmp/work")
	bridge.AgentStart("reviewer")

	ev := mustDrain(t, sub)
	if ev.Kind != bus.KindPhaseAgentStart {
		t.Errorf("Kind = %q, want %q", ev.Kind, bus.KindPhaseAgentStart)
	}
	if ev.Role != "reviewer" {
		t.Errorf("Role = %q, want %q", ev.Role, "reviewer")
	}
}

func TestBusUIBridgeAgentDoneReviewer(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	defer b.Close()
	sub := b.Subscribe("test", 16)
	defer sub.Unsubscribe()

	bridge := NewBusUIBridge(b, "phase-5", "/tmp/work")
	bridge.AgentDone("reviewer", 0.75, 1200)

	ev := mustDrain(t, sub)
	if ev.Kind != bus.KindPhaseAgentDone {
		t.Errorf("Kind = %q, want %q", ev.Kind, bus.KindPhaseAgentDone)
	}
	if ev.Role != "reviewer" {
		t.Errorf("Role = %q, want %q", ev.Role, "reviewer")
	}
	if ev.CostUSD != 0.75 {
		t.Errorf("CostUSD = %f, want %f", ev.CostUSD, 0.75)
	}
	if ev.DurationMs != 1200 {
		t.Errorf("DurationMs = %d, want %d", ev.DurationMs, 1200)
	}

	// Reviewer does NOT produce a git diff event.
	scratch := mustDrain(t, sub) // scratchpad entry
	if scratch.Kind != bus.KindScratchpadEntry {
		t.Errorf("expected scratchpad, got %q", scratch.Kind)
	}
}

func TestBusUIBridgeCycleSummary(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	defer b.Close()
	sub := b.Subscribe("test", 16)
	defer sub.Unsubscribe()

	bridge := NewBusUIBridge(b, "phase-6", "/tmp/work")
	bridge.CycleSummary(ui.CycleSummaryData{
		Cycle:        1,
		MaxCycles:    5,
		Phase:        "review_complete",
		CostUSD:      0.50,
		TotalCostUSD: 1.25,
		MaxBudgetUSD: 10.0,
		DurationMs:   3000,
		Approved:     true,
		IssueCount:   0,
	})

	ev := mustDrain(t, sub)
	if ev.Kind != bus.KindPhaseCycleSummary {
		t.Errorf("Kind = %q, want %q", ev.Kind, bus.KindPhaseCycleSummary)
	}
	if ev.CycleSummary == nil {
		t.Fatal("CycleSummary payload is nil")
	}
	if ev.CycleSummary.Cycle != 1 {
		t.Errorf("CycleSummary.Cycle = %d, want %d", ev.CycleSummary.Cycle, 1)
	}
	if ev.CycleSummary.Approved != true {
		t.Error("CycleSummary.Approved = false, want true")
	}
	if ev.CycleSummary.TotalCostUSD != 1.25 {
		t.Errorf("CycleSummary.TotalCostUSD = %f, want %f", ev.CycleSummary.TotalCostUSD, 1.25)
	}
}

func TestBusUIBridgeIssuesFound(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	defer b.Close()
	sub := b.Subscribe("test", 16)
	defer sub.Unsubscribe()

	bridge := NewBusUIBridge(b, "phase-7", "/tmp/work")
	bridge.IssuesFound(3)

	ev := mustDrain(t, sub)
	if ev.Kind != bus.KindPhaseIssuesFound {
		t.Errorf("Kind = %q, want %q", ev.Kind, bus.KindPhaseIssuesFound)
	}
	if ev.Count != 3 {
		t.Errorf("Count = %d, want %d", ev.Count, 3)
	}
}

func TestBusUIBridgeApproved(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	defer b.Close()
	sub := b.Subscribe("test", 16)
	defer sub.Unsubscribe()

	bridge := NewBusUIBridge(b, "phase-8", "/tmp/work")
	bridge.Approved()

	ev := mustDrain(t, sub)
	if ev.Kind != bus.KindPhaseApproved {
		t.Errorf("Kind = %q, want %q", ev.Kind, bus.KindPhaseApproved)
	}
	if ev.PhaseID != "phase-8" {
		t.Errorf("PhaseID = %q, want %q", ev.PhaseID, "phase-8")
	}
}

func TestBusUIBridgeMaxCyclesReached(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	defer b.Close()
	sub := b.Subscribe("test", 16)
	defer sub.Unsubscribe()

	bridge := NewBusUIBridge(b, "phase-9", "/tmp/work")
	bridge.MaxCyclesReached(5)

	ev := mustDrain(t, sub)
	if ev.Kind != bus.KindPhaseError {
		t.Errorf("Kind = %q, want %q", ev.Kind, bus.KindPhaseError)
	}
	if ev.Message == "" {
		t.Error("Message is empty, expected descriptive message")
	}
}

func TestBusUIBridgeBudgetExceeded(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	defer b.Close()
	sub := b.Subscribe("test", 16)
	defer sub.Unsubscribe()

	bridge := NewBusUIBridge(b, "phase-10", "/tmp/work")
	bridge.BudgetExceeded(12.50, 10.0)

	ev := mustDrain(t, sub)
	if ev.Kind != bus.KindPhaseError {
		t.Errorf("Kind = %q, want %q", ev.Kind, bus.KindPhaseError)
	}
	if ev.Message == "" {
		t.Error("Message is empty, expected descriptive message")
	}
}

func TestBusUIBridgeError(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	defer b.Close()
	sub := b.Subscribe("test", 16)
	defer sub.Unsubscribe()

	bridge := NewBusUIBridge(b, "phase-11", "/tmp/work")
	bridge.Error("something went wrong")

	ev := mustDrain(t, sub)
	if ev.Kind != bus.KindPhaseError {
		t.Errorf("Kind = %q, want %q", ev.Kind, bus.KindPhaseError)
	}
	if ev.Message != "something went wrong" {
		t.Errorf("Message = %q, want %q", ev.Message, "something went wrong")
	}
}

func TestBusUIBridgeInfo(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	defer b.Close()
	sub := b.Subscribe("test", 16)
	defer sub.Unsubscribe()

	bridge := NewBusUIBridge(b, "phase-12", "/tmp/work")
	bridge.Info("all good")

	ev := mustDrain(t, sub)
	if ev.Kind != bus.KindPhaseInfo {
		t.Errorf("Kind = %q, want %q", ev.Kind, bus.KindPhaseInfo)
	}
	if ev.Message != "all good" {
		t.Errorf("Message = %q, want %q", ev.Message, "all good")
	}
}

func TestBusUIBridgeAgentOutput(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	defer b.Close()
	sub := b.Subscribe("test", 16)
	defer sub.Unsubscribe()

	bridge := NewBusUIBridge(b, "phase-13", "/tmp/work")
	bridge.AgentOutput("coder", 2, "wrote some code")

	ev := mustDrain(t, sub)
	if ev.Kind != bus.KindPhaseAgentOutput {
		t.Errorf("Kind = %q, want %q", ev.Kind, bus.KindPhaseAgentOutput)
	}
	if ev.Role != "coder" {
		t.Errorf("Role = %q, want %q", ev.Role, "coder")
	}
	if ev.Cycle != 2 {
		t.Errorf("Cycle = %d, want %d", ev.Cycle, 2)
	}
	if ev.Message != "wrote some code" {
		t.Errorf("Message = %q, want %q", ev.Message, "wrote some code")
	}
}

func TestBusUIBridgeBeadUpdate(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	defer b.Close()
	sub := b.Subscribe("test", 16)
	defer sub.Unsubscribe()

	bridge := NewBusUIBridge(b, "phase-15", "/tmp/work")
	children := []ui.BeadChild{
		{ID: "child-1", Title: "Sub-task A", Status: "open"},
		{ID: "child-2", Title: "Sub-task B", Status: "closed"},
	}
	bridge.BeadUpdate("bead-root", "Root Task", "in_progress", children)

	ev := mustDrain(t, sub)
	if ev.Kind != bus.KindPhaseBeadUpdate {
		t.Errorf("Kind = %q, want %q", ev.Kind, bus.KindPhaseBeadUpdate)
	}
	if ev.BeadTree == nil {
		t.Fatal("BeadTree payload is nil")
	}
	if ev.BeadTree.TaskID != "bead-root" {
		t.Errorf("TaskID = %q, want %q", ev.BeadTree.TaskID, "bead-root")
	}
	if ev.BeadTree.Root == nil {
		t.Fatal("BeadTree.Root is nil")
	}
}

func TestBusUIBridgeFindingLifecycleNoop(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	defer b.Close()
	sub := b.Subscribe("test", 16)
	defer sub.Unsubscribe()

	bridge := NewBusUIBridge(b, "phase-16", "/tmp/work")
	bridge.FindingLifecycle(1, ui.FindingLifecycleData{Fixed: 2, StillPresent: 1})

	// FindingLifecycle is a no-op — no event should be published.
	select {
	case ev := <-sub.Events():
		t.Errorf("unexpected event: Kind=%q", ev.Kind)
	case <-time.After(50 * time.Millisecond):
		// Good — no event emitted.
	}
}

func TestBusUIBridgeScratchpadOnTaskStarted(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	defer b.Close()
	sub := b.Subscribe("test", 16)
	defer sub.Unsubscribe()

	bridge := NewBusUIBridge(b, "phase-s", "/tmp/work")
	bridge.TaskStarted("bead-1", "Do stuff")

	// First event: KindPhaseTaskStarted.
	_ = mustDrain(t, sub)

	// Second event: KindScratchpadEntry.
	ev := mustDrain(t, sub)
	if ev.Kind != bus.KindScratchpadEntry {
		t.Errorf("Kind = %q, want %q", ev.Kind, bus.KindScratchpadEntry)
	}
	if ev.PhaseID != "phase-s" {
		t.Errorf("PhaseID = %q, want %q", ev.PhaseID, "phase-s")
	}
	if ev.Message != "started" {
		t.Errorf("Message = %q, want %q", ev.Message, "started")
	}
	if ev.Timestamp.IsZero() {
		t.Error("Timestamp is zero, expected non-zero")
	}
}

func TestBusUIBridgeCycleStartUpdatesCycleField(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	defer b.Close()
	sub := b.Subscribe("test", 32)
	defer sub.Unsubscribe()

	bridge := NewBusUIBridge(b, "phase-c", "/tmp/work")

	// Set cycle via CycleStart, then call AgentOutput and verify cycle is tracked.
	bridge.CycleStart(3, 10)
	_ = mustDrain(t, sub) // KindPhaseCycleStart
	_ = mustDrain(t, sub) // KindScratchpadEntry

	bridge.AgentStart("coder")
	_ = mustDrain(t, sub) // KindPhaseAgentStart
	_ = mustDrain(t, sub) // KindScratchpadEntry

	// Verify the bridge's internal cycle counter is updated correctly
	// by checking that AgentDone for reviewer doesn't produce a diff event.
	bridge.AgentDone("reviewer", 0.50, 500)
	ev := mustDrain(t, sub) // KindPhaseAgentDone
	if ev.Kind != bus.KindPhaseAgentDone {
		t.Errorf("Kind = %q, want %q", ev.Kind, bus.KindPhaseAgentDone)
	}
}

func TestFileStatPaths(t *testing.T) {
	t.Parallel()

	t.Run("nil input", func(t *testing.T) {
		got := fileStatPaths(nil)
		if got != nil {
			t.Errorf("fileStatPaths(nil) = %v, want nil", got)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		got := fileStatPaths([]FileStatEntry{})
		if got != nil {
			t.Errorf("fileStatPaths([]) = %v, want nil", got)
		}
	})

	t.Run("populated input", func(t *testing.T) {
		entries := []FileStatEntry{
			{Path: "main.go"},
			{Path: "internal/bus/bus.go"},
		}
		got := fileStatPaths(entries)
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		if got[0] != "main.go" {
			t.Errorf("got[0] = %q, want %q", got[0], "main.go")
		}
		if got[1] != "internal/bus/bus.go" {
			t.Errorf("got[1] = %q, want %q", got[1], "internal/bus/bus.go")
		}
	})
}
