package constellations

import (
	"context"
	"testing"

	"github.com/papapumpkin/quasar/internal/artifacts"
)

// TestDispatchPhaseIterator_NoPhasesIsSuccess confirms a nebula with zero
// phases reports all_phases_complete=true with phases_executed=0. This is the
// terminal state for a seed that the architect declined to decompose; the
// nebula-lifecycle edge guard still needs to evaluate truthy so master_review
// can fire.
func TestDispatchPhaseIterator_NoPhasesIsSuccess(t *testing.T) {
	ctx := context.Background()
	child := &artifacts.Constellation{
		Name: "child",
		Nodes: []artifacts.ConstellationNode{
			builtinNode("inner", "notify_human"),
		},
		Edges: []artifacts.ConstellationEdge{
			{From: "inner", To: artifacts.TermDone},
		},
	}
	parent := &artifacts.Constellation{
		Name: "parent",
		Nodes: []artifacts.ConstellationNode{
			{
				ID:   "iter",
				Type: artifacts.NodePhaseIterator,
				Inputs: map[string]artifacts.Expression{
					"sub_constellation": mustExpr(t, "'child'"),
				},
			},
		},
		Edges: []artifacts.ConstellationEdge{
			{From: "iter", To: artifacts.TermDone},
		},
	}
	loader := &fakeLoader{cons: map[string]*artifacts.Constellation{
		"parent": parent,
		"child":  child,
	}}
	rt, nebID := newTestRuntime(t, loader, nil)

	runID, err := rt.Fire(ctx, "parent", nebID, "", 0)
	if err != nil {
		t.Fatalf("Fire parent: %v", err)
	}
	state, err := rt.Step(ctx, runID)
	if err != nil {
		t.Fatalf("Step parent: %v", err)
	}
	if state != StateDone {
		t.Fatalf("parent state = %q, want %q (empty-phase nebula should still mark done)", state, StateDone)
	}

	// The iter node's outputs must carry all_phases_complete=true so a
	// downstream edge guard `iter.all_phases_complete` would fire.
	parentRun, err := rt.runStore.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	st, err := UnmarshalState(parentRun.DAGStateTOML)
	if err != nil {
		t.Fatalf("UnmarshalState: %v", err)
	}
	out := st.Nodes["iter"]
	if got, _ := out["all_phases_complete"].(bool); !got {
		t.Errorf("iter.all_phases_complete = %v, want true", out["all_phases_complete"])
	}
	if got, _ := out["phases_executed"].(int64); got != 0 {
		// TOML round-trip turns int into int64 in the persisted state.
		t.Errorf("iter.phases_executed = %v, want 0", out["phases_executed"])
	}
}

// TestDispatchPhaseIterator_RejectsMissingSubConstellation confirms the
// dispatcher fails fast on the most common authoring error (omitting the
// sub_constellation input) rather than silently no-op'ing.
func TestDispatchPhaseIterator_RejectsMissingSubConstellation(t *testing.T) {
	ctx := context.Background()
	parent := &artifacts.Constellation{
		Name: "parent",
		Nodes: []artifacts.ConstellationNode{
			{ID: "iter", Type: artifacts.NodePhaseIterator},
		},
		Edges: []artifacts.ConstellationEdge{
			{From: "iter", To: artifacts.TermDone},
		},
	}
	loader := &fakeLoader{cons: map[string]*artifacts.Constellation{"parent": parent}}
	rt, nebID := newTestRuntime(t, loader, nil)
	runID, err := rt.Fire(ctx, "parent", nebID, "", 0)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	state, stepErr := rt.Step(ctx, runID)
	if state != StateFailed {
		t.Fatalf("state = %q, want %q (missing sub_constellation should fail the run)", state, StateFailed)
	}
	if stepErr == nil {
		t.Fatalf("expected error mentioning sub_constellation, got nil")
	}
}

// TestPhaseExecSnapshotsIntoExprState confirms that when the dispatcher seeds
// CurrentPhase, the child's ExprState resolves `nebula.current_phase.body` to
// the seeded value. This is the load-bearing binding for coder-reviewer.toml.
func TestPhaseExecSnapshotsIntoExprState(t *testing.T) {
	t.Parallel()
	st := NewState(NebulaSnapshot{
		ID:   "neb-1",
		Name: "demo",
		CurrentPhase: &PhaseExec{
			ID:    "p1",
			Title: "Wire the thing",
			Body:  "Implement X by editing Y.",
		},
	}, 0)
	expr := st.ExprState()
	nebula, _ := expr["nebula"].(map[string]any)
	if nebula == nil {
		t.Fatal("ExprState has no nebula key")
	}
	cp, _ := nebula["current_phase"].(map[string]any)
	if cp == nil {
		t.Fatal("nebula.current_phase missing")
	}
	if got, _ := cp["body"].(string); got != "Implement X by editing Y." {
		t.Errorf("current_phase.body = %q, want the seeded value", got)
	}
	if got, _ := cp["title"].(string); got != "Wire the thing" {
		t.Errorf("current_phase.title = %q", got)
	}
}

// TestPhaseExecAbsentWhenNotSet confirms that without a phase_iterator
// dispatch, current_phase is NOT projected. This keeps existing tests
// against the expression engine deterministic.
func TestPhaseExecAbsentWhenNotSet(t *testing.T) {
	t.Parallel()
	st := NewState(NebulaSnapshot{ID: "neb-1", Name: "demo"}, 0)
	expr := st.ExprState()
	nebula, _ := expr["nebula"].(map[string]any)
	if _, ok := nebula["current_phase"]; ok {
		t.Error("current_phase present in ExprState despite CurrentPhase=nil")
	}
}
