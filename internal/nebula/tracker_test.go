package nebula

import (
	"testing"

	"github.com/papapumpkin/quasar/internal/dag"
)

// buildTestDAG constructs a *dag.DAG from phase specs, mirroring the
// dependency structure that the scheduler would build.
func buildTestDAG(phases []PhaseSpec) *dag.DAG {
	d := dag.New()
	for _, p := range phases {
		d.AddNodeIdempotent(p.ID, p.Priority)
	}
	for _, p := range phases {
		for _, dep := range p.DependsOn {
			_ = d.AddEdge(p.ID, dep)
		}
	}
	return d
}

func TestFilterEligible_ScopeConflictWithInFlight(t *testing.T) {
	t.Parallel()

	phases := []PhaseSpec{
		{ID: "a", Scope: []string{"src/api/**"}},
		{ID: "b", Scope: []string{"src/api/handlers/**"}}, // overlaps with a
		{ID: "c", Scope: []string{"src/ui/**"}},           // no overlap
		{ID: "d"},                                         // no scope
	}
	state := &State{Phases: map[string]*PhaseState{}}
	pt := NewPhaseTracker(phases, state)
	graph := buildTestDAG(phases)

	// Mark "a" as in-flight.
	pt.inFlight["a"] = true

	ready := []string{"b", "c", "d"}
	eligible := pt.FilterEligible(ready, graph)

	// b overlaps with in-flight a, so should be excluded.
	// c and d should remain eligible.
	if len(eligible) != 2 {
		t.Fatalf("expected 2 eligible, got %d: %v", len(eligible), eligible)
	}
	found := map[string]bool{}
	for _, id := range eligible {
		found[id] = true
	}
	if found["b"] {
		t.Error("phase b should be excluded due to scope conflict with in-flight a")
	}
	if !found["c"] {
		t.Error("phase c should be eligible (no scope overlap)")
	}
	if !found["d"] {
		t.Error("phase d should be eligible (no scope)")
	}
}

func TestFilterEligible_ScopeConflictBetweenEligible(t *testing.T) {
	t.Parallel()

	// Two independent phases with overlapping scopes — the first (higher impact)
	// should be dispatched; the second deferred.
	phases := []PhaseSpec{
		{ID: "a", Scope: []string{"src/**"}},
		{ID: "b", Scope: []string{"src/core/**"}}, // overlaps with a
	}
	state := &State{Phases: map[string]*PhaseState{}}
	pt := NewPhaseTracker(phases, state)
	graph := buildTestDAG(phases)

	ready := []string{"a", "b"} // a comes first (higher impact)
	eligible := pt.FilterEligible(ready, graph)

	if len(eligible) != 1 {
		t.Fatalf("expected 1 eligible, got %d: %v", len(eligible), eligible)
	}
	if eligible[0] != "a" {
		t.Errorf("expected phase a to be eligible, got %q", eligible[0])
	}
}

func TestFilterEligible_AllowScopeOverlapBypass(t *testing.T) {
	t.Parallel()

	// Phase b opts out of scope overlap checking.
	phases := []PhaseSpec{
		{ID: "a", Scope: []string{"src/**"}},
		{ID: "b", Scope: []string{"src/**"}, AllowScopeOverlap: true},
	}
	state := &State{Phases: map[string]*PhaseState{}}
	pt := NewPhaseTracker(phases, state)
	graph := buildTestDAG(phases)

	// Mark a as in-flight.
	pt.inFlight["a"] = true

	ready := []string{"b"}
	eligible := pt.FilterEligible(ready, graph)

	// b has AllowScopeOverlap, so it should still be eligible.
	if len(eligible) != 1 || eligible[0] != "b" {
		t.Errorf("expected [b] eligible (AllowScopeOverlap), got %v", eligible)
	}
}

func TestFilterEligible_NoScopeNoConflict(t *testing.T) {
	t.Parallel()

	// Phases with no scope should never conflict.
	phases := []PhaseSpec{
		{ID: "a"},
		{ID: "b"},
	}
	state := &State{Phases: map[string]*PhaseState{}}
	pt := NewPhaseTracker(phases, state)
	graph := buildTestDAG(phases)

	pt.inFlight["a"] = true

	ready := []string{"b"}
	eligible := pt.FilterEligible(ready, graph)

	if len(eligible) != 1 || eligible[0] != "b" {
		t.Errorf("expected [b] eligible (no scopes), got %v", eligible)
	}
}
