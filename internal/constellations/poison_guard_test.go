package constellations

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/artifacts"
)

// countingInvoker records how many times Invoke was called so a test can assert
// a node was (or was not) dispatched.
type countingInvoker struct{ calls int }

func (c *countingInvoker) Invoke(_ context.Context, _ agent.Agent, _, _ string) (agent.InvocationResult, error) {
	c.calls++
	return agent.InvocationResult{ResultText: "{}"}, nil
}
func (c *countingInvoker) Validate() error { return nil }

// panicInvoker panics on Invoke, standing in for a node whose dispatch blows up
// (a bug in an operator/builtin, or malformed state).
type panicInvoker struct{}

func (panicInvoker) Invoke(_ context.Context, _ agent.Agent, _, _ string) (agent.InvocationResult, error) {
	panic("boom in node dispatch")
}
func (panicInvoker) Validate() error { return nil }

// singleStarCon is a one-node constellation: a star that routes straight to
// _done. Enough to exercise Step's pre-dispatch guards.
func singleStarCon(name, star string) *artifacts.Constellation {
	return &artifacts.Constellation{
		Name:  name,
		Nodes: []artifacts.ConstellationNode{{ID: "work", Type: artifacts.NodeStar, Star: star}},
		Edges: []artifacts.ConstellationEdge{{From: "work", To: artifacts.TermDone}},
	}
}

// TestStepPoisonGuardFailsRunPastCap verifies that once a run's attempt counter
// exceeds maxStepAttempts (simulating a node that crash-looped across restarts),
// the next Step fails the run WITHOUT dispatching the node — so a poison node
// can't take the fleet down on every boot.
func TestStepPoisonGuardFailsRunPastCap(t *testing.T) {
	ctx := context.Background()
	con := singleStarCon("poison", "worker")
	loader := &fakeLoader{
		cons:  map[string]*artifacts.Constellation{"poison": con},
		stars: map[string]*artifacts.Star{"worker": {Name: "worker", Model: "sonnet", Prompt: "work"}},
	}
	inv := &countingInvoker{}
	rt, nebID := newTestRuntime(t, loader, inv)

	runID, err := rt.Fire(ctx, "poison", nebID, "", 0)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}

	// Simulate maxStepAttempts crash-restarts that each re-entered the node
	// without completing (the autocommitted bump survives a crash).
	for i := 0; i < maxStepAttempts; i++ {
		if _, err := rt.runStore.BumpStepAttempt(ctx, runID); err != nil {
			t.Fatalf("BumpStepAttempt: %v", err)
		}
	}

	// Step fails the run and returns the cause (the driver logs it); the run is
	// terminal. The key guarantees: state is failed and the node never ran.
	state, err := rt.Step(ctx, runID)
	if state != StateFailed {
		t.Errorf("state = %q, want failed", state)
	}
	if !errors.Is(err, ErrMaxStepAttempts) {
		t.Errorf("err = %v, want wrap of ErrMaxStepAttempts", err)
	}
	if inv.calls != 0 {
		t.Errorf("invoker called %d times; the guard must fail the run before dispatch", inv.calls)
	}

	run, _ := rt.runStore.GetRun(ctx, runID)
	st, _ := UnmarshalState(run.DAGStateTOML)
	if msg, _ := st.Nodes["_error"]["message"].(string); !strings.Contains(msg, "max step attempts") {
		t.Errorf("_error message = %q, want max-step-attempts cause", msg)
	}
}

// TestStepRecoversNodePanic verifies a panicking node fails its run instead of
// crashing the process — the test itself would panic if recovery were absent.
func TestStepRecoversNodePanic(t *testing.T) {
	ctx := context.Background()
	con := singleStarCon("boom", "panicker")
	loader := &fakeLoader{
		cons:  map[string]*artifacts.Constellation{"boom": con},
		stars: map[string]*artifacts.Star{"panicker": {Name: "panicker", Model: "sonnet", Prompt: "x"}},
	}
	rt, nebID := newTestRuntime(t, loader, panicInvoker{})

	runID, err := rt.Fire(ctx, "boom", nebID, "", 0)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}

	// The point: Step returns normally (the test process would have crashed if
	// the panic escaped). The run is failed and the cause wraps ErrNodePanic;
	// Step returns the cause for the driver to log, as with any node failure.
	state, err := rt.Step(ctx, runID)
	if state != StateFailed {
		t.Errorf("state = %q, want failed", state)
	}
	if !errors.Is(err, ErrNodePanic) {
		t.Errorf("err = %v, want wrap of ErrNodePanic", err)
	}

	run, _ := rt.runStore.GetRun(ctx, runID)
	st, _ := UnmarshalState(run.DAGStateTOML)
	msg, _ := st.Nodes["_error"]["message"].(string)
	if !strings.Contains(msg, "panicked") {
		t.Errorf("_error message = %q, want a panic cause", msg)
	}
}
