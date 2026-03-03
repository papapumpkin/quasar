package telemetry

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/bus"
)

// newTestHarness creates a MemoryBus, an Emitter writing to a temp file,
// and a BusSubscriber. It starts the subscriber and returns a cleanup
// function that stops the subscriber, closes the emitter, and returns
// the decoded telemetry events from the file.
func newTestHarness(t *testing.T) (bus.Bus, func() []Event) {
	t.Helper()
	b := bus.NewMemoryBus()
	path := filepath.Join(t.TempDir(), "telemetry.jsonl")
	em, err := NewEmitter(path)
	if err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}

	sub := NewBusSubscriber(em, b, "test-epoch")
	sub.Start()

	return b, func() []Event {
		// Close the bus first: this closes subscriber channels, allowing
		// the run goroutine to drain remaining events and exit.
		if err := b.Close(); err != nil {
			t.Fatalf("Bus.Close: %v", err)
		}
		sub.Stop()
		if err := em.Close(); err != nil {
			t.Fatalf("Emitter.Close: %v", err)
		}

		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open telemetry file: %v", err)
		}
		defer f.Close()

		var events []Event
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var evt Event
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				t.Fatalf("invalid JSONL line: %v\nline: %s", err, line)
			}
			events = append(events, evt)
		}
		if err := scanner.Err(); err != nil {
			t.Fatalf("scanner: %v", err)
		}
		return events
	}
}

func TestBusSubscriberMapsAgentStart(t *testing.T) {
	t.Parallel()
	b, collect := newTestHarness(t)

	ev := bus.NewPhase(bus.KindPhaseAgentStart, "phase-1")
	ev.Role = "coder"
	if err := b.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	events := collect()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	got := events[0]
	if got.Kind != KindAgentStart {
		t.Errorf("kind = %q, want %q", got.Kind, KindAgentStart)
	}
	if got.EpochID != "test-epoch" {
		t.Errorf("epoch = %q, want %q", got.EpochID, "test-epoch")
	}
	if got.TaskID != "phase-1" {
		t.Errorf("task = %q, want %q", got.TaskID, "phase-1")
	}
	data, ok := got.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", got.Data)
	}
	if data["role"] != "coder" {
		t.Errorf("data[role] = %v, want %q", data["role"], "coder")
	}
}

func TestBusSubscriberMapsAgentDone(t *testing.T) {
	t.Parallel()
	b, collect := newTestHarness(t)

	ev := bus.NewPhase(bus.KindPhaseAgentDone, "phase-2")
	ev.Role = "reviewer"
	ev.CostUSD = 0.042
	ev.DurationMs = 1500
	ev.Tokens = 3200
	if err := b.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	events := collect()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	got := events[0]
	if got.Kind != KindAgentDone {
		t.Errorf("kind = %q, want %q", got.Kind, KindAgentDone)
	}
	data, ok := got.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", got.Data)
	}
	if v, _ := data["cost_usd"].(float64); v != 0.042 {
		t.Errorf("data[cost_usd] = %v, want 0.042", data["cost_usd"])
	}
	if v, _ := data["duration_ms"].(float64); v != 1500 {
		t.Errorf("data[duration_ms] = %v, want 1500", data["duration_ms"])
	}
	if v, _ := data["tokens"].(float64); v != 3200 {
		t.Errorf("data[tokens] = %v, want 3200", data["tokens"])
	}
}

func TestBusSubscriberMapsCycleSummary(t *testing.T) {
	t.Parallel()
	b, collect := newTestHarness(t)

	ev := bus.NewPhase(bus.KindPhaseCycleSummary, "phase-3")
	ev.CycleSummary = &bus.CycleSummaryPayload{
		Cycle:      2,
		CostUSD:    0.15,
		Approved:   true,
		IssueCount: 0,
	}
	if err := b.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	events := collect()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	got := events[0]
	if got.Kind != KindCycleDone {
		t.Errorf("kind = %q, want %q", got.Kind, KindCycleDone)
	}
	data, ok := got.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", got.Data)
	}
	if v, _ := data["approved"].(bool); !v {
		t.Errorf("data[approved] = %v, want true", data["approved"])
	}
	if v, _ := data["cycle"].(float64); v != 2 {
		t.Errorf("data[cycle] = %v, want 2", data["cycle"])
	}
}

func TestBusSubscriberSkipsUnmapped(t *testing.T) {
	t.Parallel()
	b, collect := newTestHarness(t)

	// KindPhaseInfo has no telemetry mapping.
	ev := bus.NewPhase(bus.KindPhaseInfo, "phase-x")
	ev.Message = "some info"
	if err := b.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	events := collect()
	if len(events) != 0 {
		t.Fatalf("expected 0 events for unmapped kind, got %d", len(events))
	}
}

func TestBusSubscriberStop(t *testing.T) {
	t.Parallel()
	b := bus.NewMemoryBus()
	path := filepath.Join(t.TempDir(), "drain.jsonl")
	em, err := NewEmitter(path)
	if err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}
	defer em.Close()

	sub := NewBusSubscriber(em, b, "drain-epoch")
	sub.Start()

	// Publish several events before stopping.
	for range 5 {
		ev := bus.NewPhase(bus.KindPhaseAgentStart, "drain-phase")
		ev.Role = "coder"
		if err := b.Publish(context.Background(), ev); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}

	// Close the bus first: this closes the subscriber channel, allowing
	// run() to drain buffered events and exit.
	if err := b.Close(); err != nil {
		t.Fatalf("Bus.Close: %v", err)
	}

	// Stop waits for the run goroutine to finish draining.
	sub.Stop()

	// Read back the file — all 5 events should have been written.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	var count int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if scanner.Text() != "" {
			count++
		}
	}
	if count != 5 {
		t.Errorf("expected 5 drained events, got %d", count)
	}
}

func TestBusSubscriberMapsTaskStarted(t *testing.T) {
	t.Parallel()
	b, collect := newTestHarness(t)

	ev := bus.NewPhase(bus.KindPhaseTaskStarted, "phase-task")
	ev.BeadID = "bead-123"
	ev.Title = "Implement feature"
	if err := b.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	events := collect()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != KindEpochStart {
		t.Errorf("kind = %q, want %q", events[0].Kind, KindEpochStart)
	}
}

func TestBusSubscriberMapsHealingAttempt(t *testing.T) {
	t.Parallel()
	b, collect := newTestHarness(t)

	ev := bus.New(bus.KindHealingAttempt)
	ev.Healing = &bus.HealingPayload{
		FailedPhaseID:    "failed-1",
		FailureKind:      "compile_error",
		RemediationID:    "rem-1",
		RemediationTitle: "Fix compilation",
	}
	if err := b.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	events := collect()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	got := events[0]
	if got.Kind != KindHealingStart {
		t.Errorf("kind = %q, want %q", got.Kind, KindHealingStart)
	}
	data, ok := got.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", got.Data)
	}
	if data["failed_phase_id"] != "failed-1" {
		t.Errorf("data[failed_phase_id] = %v, want %q", data["failed_phase_id"], "failed-1")
	}
}

func TestBusSubscriberSkipsHealingWithNilPayload(t *testing.T) {
	t.Parallel()
	b, collect := newTestHarness(t)

	ev := bus.New(bus.KindHealingAttempt)
	// ev.Healing is nil — should be skipped.
	if err := b.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	events := collect()
	if len(events) != 0 {
		t.Fatalf("expected 0 events for nil healing, got %d", len(events))
	}
}

func TestBusSubscriberMapsNebulaDone(t *testing.T) {
	t.Parallel()
	b, collect := newTestHarness(t)

	ev := bus.New(bus.KindNebulaDone)
	ev.DoneResults = &bus.DonePayload{
		Err: errors.New("something failed"),
	}
	if err := b.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	events := collect()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	got := events[0]
	if got.Kind != KindEpochDone {
		t.Errorf("kind = %q, want %q", got.Kind, KindEpochDone)
	}
	data, ok := got.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", got.Data)
	}
	if data["error"] != "something failed" {
		t.Errorf("data[error] = %v, want %q", data["error"], "something failed")
	}
}

func TestBusSubscriberMapsCycleStart(t *testing.T) {
	t.Parallel()
	b, collect := newTestHarness(t)

	ev := bus.NewPhase(bus.KindPhaseCycleStart, "phase-cycle")
	ev.Cycle = 3
	ev.MaxCycles = 5
	if err := b.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	events := collect()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	got := events[0]
	if got.Kind != KindCycleStart {
		t.Errorf("kind = %q, want %q", got.Kind, KindCycleStart)
	}
	data, ok := got.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", got.Data)
	}
	if v, _ := data["cycle"].(float64); v != 3 {
		t.Errorf("data[cycle] = %v, want 3", data["cycle"])
	}
	if v, _ := data["max_cycles"].(float64); v != 5 {
		t.Errorf("data[max_cycles] = %v, want 5", data["max_cycles"])
	}
}

func TestBusSubscriberPreservesTimestamp(t *testing.T) {
	t.Parallel()
	b, collect := newTestHarness(t)

	ev := bus.Event{
		Kind:      bus.KindPhaseAgentStart,
		Timestamp: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
		PhaseID:   "ts-phase",
		Role:      "coder",
	}
	if err := b.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	events := collect()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if !events[0].Timestamp.Equal(time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("timestamp = %v, want 2025-06-15T12:00:00Z", events[0].Timestamp)
	}
}
