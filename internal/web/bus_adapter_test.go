package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/bus"
)

func TestBusAdapter_Subscribe(t *testing.T) {
	t.Parallel()

	eventBus := bus.NewMemoryBus()
	defer eventBus.Close()

	adapter := NewBusAdapter(eventBus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, unsub := adapter.Subscribe(ctx)
	defer unsub()

	// Publish a bus event.
	ev := bus.NewPhase(bus.KindPhaseInfo, "test-phase")
	ev.Message = "hello adapter"
	if err := eventBus.Publish(ctx, ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Read the translated web event.
	select {
	case webEv := <-events:
		if webEv.Type != string(bus.KindPhaseInfo) {
			t.Errorf("Type = %q, want %q", webEv.Type, string(bus.KindPhaseInfo))
		}
		if !strings.Contains(webEv.Data, "test-phase") {
			t.Errorf("Data = %q, want to contain 'test-phase'", webEv.Data)
		}
		if !strings.Contains(webEv.Data, "hello adapter") {
			t.Errorf("Data = %q, want to contain 'hello adapter'", webEv.Data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestBusAdapter_CancelClosesChannel(t *testing.T) {
	t.Parallel()

	eventBus := bus.NewMemoryBus()
	defer eventBus.Close()

	adapter := NewBusAdapter(eventBus)

	ctx, cancel := context.WithCancel(context.Background())
	events, unsub := adapter.Subscribe(ctx)
	defer unsub()

	// Cancel the context.
	cancel()

	// The events channel should be closed.
	select {
	case _, ok := <-events:
		if ok {
			// Might get a buffered event, drain it.
			select {
			case _, ok2 := <-events:
				if ok2 {
					t.Error("expected channel to close after cancel")
				}
			case <-time.After(time.Second):
				t.Error("timed out waiting for channel close")
			}
		}
		// ok == false means channel was closed, which is correct.
	case <-time.After(time.Second):
		t.Error("timed out waiting for channel close")
	}
}

func TestBusAdapter_MultipleSubscribers(t *testing.T) {
	t.Parallel()

	eventBus := bus.NewMemoryBus()
	defer eventBus.Close()

	adapter := NewBusAdapter(eventBus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events1, unsub1 := adapter.Subscribe(ctx)
	defer unsub1()
	events2, unsub2 := adapter.Subscribe(ctx)
	defer unsub2()

	ev := bus.NewPhase(bus.KindPhaseTaskStarted, "p1")
	ev.Message = "started"
	if err := eventBus.Publish(ctx, ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Both subscribers should receive the event.
	for i, ch := range []<-chan Event{events1, events2} {
		select {
		case webEv := <-ch:
			if webEv.Type != string(bus.KindPhaseTaskStarted) {
				t.Errorf("subscriber %d: Type = %q, want %q", i, webEv.Type, string(bus.KindPhaseTaskStarted))
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("subscriber %d: timed out", i)
		}
	}
}

// TestBusEventToAccumulator_AgentOutput verifies that agent output in a
// bus.Event.Message flows through busEventToWebEvent into the accumulator's
// Output field. This is an integration-style test catching serialization
// mismatches between the bus adapter and the accumulator.
func TestBusEventToAccumulator_AgentOutput(t *testing.T) {
	t.Parallel()

	acc := NewPhaseAccumulator()

	// Simulate the sequence: task started → cycle start → agent start → agent output.
	started := bus.NewPhase(bus.KindPhaseTaskStarted, "p1")
	started.Title = "Output Integration"
	acc.handle(busEventToWebEvent(started))

	cycleStart := bus.NewPhase(bus.KindPhaseCycleStart, "p1")
	cycleStart.Cycle = 1
	acc.handle(busEventToWebEvent(cycleStart))

	agentStart := bus.NewPhase(bus.KindPhaseAgentStart, "p1")
	agentStart.Role = "coder"
	acc.handle(busEventToWebEvent(agentStart))

	// In real bus events, agent output is carried in ev.Message.
	agentOutput := bus.NewPhase(bus.KindPhaseAgentOutput, "p1")
	agentOutput.Role = "coder"
	agentOutput.Cycle = 1
	agentOutput.Message = "implemented the feature"
	acc.handle(busEventToWebEvent(agentOutput))

	pd := acc.Get("p1")
	if pd == nil {
		t.Fatal("expected PhaseDetail for p1")
	}
	if len(pd.Cycles) == 0 || len(pd.Cycles[0].Agents) == 0 {
		t.Fatal("expected cycle with agent")
	}
	agent := pd.Cycles[0].Agents[0]
	if agent.Output != "implemented the feature" {
		t.Errorf("agent output = %q, want %q", agent.Output, "implemented the feature")
	}
}

// TestBusEventToWebEvent_PhaseID verifies that PhaseID is populated on the
// web event for internal routing.
func TestBusEventToWebEvent_PhaseID(t *testing.T) {
	t.Parallel()

	ev := bus.NewPhase(bus.KindPhaseAgentDone, "my-phase")
	ev.Role = "coder"
	ev.CostUSD = 0.05
	webEv := busEventToWebEvent(ev)

	if webEv.PhaseID != "my-phase" {
		t.Errorf("PhaseID = %q, want %q", webEv.PhaseID, "my-phase")
	}
	if webEv.Type != string(bus.KindPhaseAgentDone) {
		t.Errorf("Type = %q, want %q", webEv.Type, string(bus.KindPhaseAgentDone))
	}
}

func TestJsonString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", `""`},
		{"plain", "hello", `"hello"`},
		{"quotes", `say "hi"`, `"say \"hi\""`},
		{"backslash", `path\to`, `"path\\to"`},
		{"newline", "line1\nline2", `"line1\nline2"`},
		{"tab", "col1\tcol2", `"col1\tcol2"`},
		{"carriage return", "a\rb", `"a\rb"`},
		{"null byte", "a\x00b", `"a\u0000b"`},
		{"formfeed", "a\fb", "\"a\\fb\""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := jsonString(tt.in)
			if got != tt.want {
				t.Errorf("jsonString(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
