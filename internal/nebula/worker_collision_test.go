package nebula

import (
	"context"
	"testing"

	"github.com/papapumpkin/quasar/internal/bus"
)

// TestPublishCollisions verifies that publishCollisions surfaces a scope
// conflict between a pending phase and an in-flight phase as a bus event the
// TUI can render.
func TestPublishCollisions(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "a", Scope: []string{"internal/runtime/**"}},
		{ID: "b", Scope: []string{"internal/runtime/engine.go"}}, // overlaps a
		{ID: "c", Scope: []string{"internal/ui/**"}},             // no overlap
	}
	state := &State{Phases: map[string]*PhaseState{}}

	memBus := bus.NewMemoryBus()
	defer memBus.Close()
	sub := memBus.Subscribe("test", 8)
	defer sub.Unsubscribe()

	wg := &WorkerGroup{Nebula: &Nebula{Phases: phases}, State: state, Bus: memBus}
	wg.tracker = NewPhaseTracker(phases, state)
	wg.tracker.inFlight["a"] = true // a is running; b collides, c does not

	wg.publishCollisions(context.Background())

	ev := <-sub.Events()
	if ev.Kind != bus.KindEntanglementUpdate {
		t.Fatalf("event kind = %q, want %q", ev.Kind, bus.KindEntanglementUpdate)
	}
	if len(ev.Collisions) != 1 {
		t.Fatalf("got %d collisions, want 1: %+v", len(ev.Collisions), ev.Collisions)
	}
	c := ev.Collisions[0]
	if c.PhaseID != "b" || c.OtherPhaseID != "a" {
		t.Errorf("collision = {Phase:%q Other:%q}, want {b a}", c.PhaseID, c.OtherPhaseID)
	}
	if c.Scope == "" {
		t.Error("collision Scope is empty")
	}
}

// TestPublishCollisions_NilBus confirms a nil bus is a safe no-op.
func TestPublishCollisions_NilBus(t *testing.T) {
	t.Parallel()

	state := &State{Phases: map[string]*PhaseState{}}
	wg := &WorkerGroup{Nebula: &Nebula{}, State: state} // Bus is nil
	wg.tracker = NewPhaseTracker(nil, state)

	wg.publishCollisions(context.Background()) // must not panic
}

// TestPublishCollisions_EmptyIsNonNil verifies that when no phases collide, the
// emitted slice is non-nil so the TUI clears any stale collision warning rather
// than leaving it on screen.
func TestPublishCollisions_EmptyIsNonNil(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{{ID: "a", Scope: []string{"x/**"}}}
	state := &State{Phases: map[string]*PhaseState{}}

	memBus := bus.NewMemoryBus()
	defer memBus.Close()
	sub := memBus.Subscribe("test", 8)
	defer sub.Unsubscribe()

	wg := &WorkerGroup{Nebula: &Nebula{Phases: phases}, State: state, Bus: memBus}
	wg.tracker = NewPhaseTracker(phases, state) // nothing in-flight → no collisions

	wg.publishCollisions(context.Background())

	ev := <-sub.Events()
	if ev.Collisions == nil {
		t.Error("Collisions is nil; want non-nil empty slice to clear stale TUI warnings")
	}
	if len(ev.Collisions) != 0 {
		t.Errorf("got %d collisions, want 0", len(ev.Collisions))
	}
}
