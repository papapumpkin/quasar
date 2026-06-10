package constellations

import (
	"context"
	"sync"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/artifacts"
)

// fakeSink records the event types the runtime emits.
type fakeSink struct {
	mu  sync.Mutex
	got []string
}

func (f *fakeSink) Emit(_, typ string, _ map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, typ)
}

func (f *fakeSink) seen() map[string]bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := make(map[string]bool, len(f.got))
	for _, t := range f.got {
		m[t] = true
	}
	return m
}

// TestRuntimeEmitsLiveEvents drives a star→builtin→terminal constellation and
// asserts the runtime emits step_started (dispatchStar), step_completed
// (persistTransition), and nebula_status_changed (terminate) to its EventSink.
func TestRuntimeEmitsLiveEvents(t *testing.T) {
	ctx := context.Background()
	con := &artifacts.Constellation{
		Name: "emit-flow",
		Nodes: []artifacts.ConstellationNode{
			{ID: "work", Type: artifacts.NodeStar, Star: "coder"},
			builtinNode("notify", "notify_human"),
		},
		Edges: []artifacts.ConstellationEdge{
			{From: "work", To: "notify"},
			{From: "notify", To: artifacts.TermAwaitingHuman},
		},
	}
	loader := &fakeLoader{
		cons:  map[string]*artifacts.Constellation{"emit-flow": con},
		stars: map[string]*artifacts.Star{"coder": {Name: "coder", Model: "sonnet", Prompt: "x"}},
	}
	inv := &fakeInvoker{result: agent.InvocationResult{ResultText: "{}"}}
	rt, nebID := newTestRuntime(t, loader, inv)

	sink := &fakeSink{}
	rt.events = sink // in-package: inject the sink directly

	runID, err := rt.Fire(ctx, "emit-flow", nebID, "", 0)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	driveToTerminal(ctx, t, rt, runID)

	seen := sink.seen()
	for _, want := range []string{"step_started", "step_completed", "nebula_status_changed"} {
		if !seen[want] {
			t.Errorf("runtime did not emit %q; emitted: %v", want, seen)
		}
	}
}

// TestRuntimeEmitNilSafe confirms a runtime with no EventSink emits nothing and
// does not panic.
func TestRuntimeEmitNilSafe(t *testing.T) {
	rt := &Runtime{} // events is nil
	rt.emit("runs", "step_started", map[string]any{"run_id": "x"})
}
