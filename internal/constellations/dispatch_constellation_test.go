package constellations

import (
	"context"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/artifacts"
)

// TestDispatchConstellation_NestedRunToDone confirms that a parent
// constellation node of type NodeConstellation drives its child to a terminal
// 'done' state, projects the child's [outputs] block into the parent's view of
// the node, and lets the parent terminate cleanly.
func TestDispatchConstellation_NestedRunToDone(t *testing.T) {
	ctx := context.Background()
	child := &artifacts.Constellation{
		Name: "child",
		Nodes: []artifacts.ConstellationNode{
			builtinNode("inner", "notify_human"),
		},
		Edges: []artifacts.ConstellationEdge{
			{From: "inner", To: artifacts.TermDone},
		},
		Outputs: map[string]artifacts.Expression{
			"label": mustExpr(t, "'shipped-from-child'"),
		},
	}
	parent := &artifacts.Constellation{
		Name: "parent",
		Nodes: []artifacts.ConstellationNode{
			{ID: "delegate", Type: artifacts.NodeConstellation, Ref: "child"},
		},
		Edges: []artifacts.ConstellationEdge{
			{From: "delegate", To: artifacts.TermDone},
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

	// One Step on the parent fires the NodeConstellation node, which runs the
	// child synchronously to terminal, then transitions the parent to _done.
	state, err := rt.Step(ctx, runID)
	if err != nil {
		t.Fatalf("Step parent: %v", err)
	}
	if state != StateDone {
		t.Fatalf("parent state = %q, want %q", state, StateDone)
	}

	// Subsequent Step on a terminal run yields ErrTerminal.
	if _, err := rt.Step(ctx, runID); err != ErrTerminal {
		t.Errorf("Step on terminal parent: err = %v, want ErrTerminal", err)
	}

	// Inspect the parent's persisted state to confirm the child's outputs
	// were projected under the parent's delegate node.
	parentRun, err := rt.runStore.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	st, err := UnmarshalState(parentRun.DAGStateTOML)
	if err != nil {
		t.Fatalf("UnmarshalState parent: %v", err)
	}
	delegateOut := st.Nodes["delegate"]
	if delegateOut == nil {
		t.Fatalf("parent state has no delegate node outputs: %+v", st.Nodes)
	}
	if got, _ := delegateOut["state"].(string); got != StateDone {
		t.Errorf("delegate.state = %q, want %q", got, StateDone)
	}
	if got, _ := delegateOut["label"].(string); got != "shipped-from-child" {
		t.Errorf("delegate.label = %q (from child [outputs]), want %q", got, "shipped-from-child")
	}
	if _, ok := delegateOut["run_id"].(string); !ok {
		t.Errorf("delegate.run_id missing — child run ID should be in parent's view of the node")
	}
}

// TestDispatchConstellation_ChildFailurePropagates confirms that a child run
// that terminates 'failed' surfaces to the parent as a dispatch error, so the
// parent's failure-path edges fire rather than the parent silently treating
// the failure as a successful node completion.
func TestDispatchConstellation_ChildFailurePropagates(t *testing.T) {
	ctx := context.Background()
	// Child unconditionally fails: fail_run records reason+detail, then the
	// unconditional edge to _failed terminates the child run as 'failed'.
	child := &artifacts.Constellation{
		Name: "child",
		Nodes: []artifacts.ConstellationNode{
			{
				ID:   "give-up",
				Type: artifacts.NodeBuiltin,
				Op:   "fail_run",
				Inputs: map[string]artifacts.Expression{
					"reason": mustExpr(t, "'inner fail'"),
				},
			},
		},
		Edges: []artifacts.ConstellationEdge{
			{From: "give-up", To: artifacts.TermFailed},
		},
	}
	parent := &artifacts.Constellation{
		Name: "parent",
		Nodes: []artifacts.ConstellationNode{
			{ID: "delegate", Type: artifacts.NodeConstellation, Ref: "child"},
		},
		Edges: []artifacts.ConstellationEdge{
			{From: "delegate", To: artifacts.TermDone},
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
	// A child failure is a node failure for the parent, so Step returns
	// (StateFailed, cause) — the same shape every fail() path uses.
	state, err := rt.Step(ctx, runID)
	if state != StateFailed {
		t.Fatalf("parent state = %q, want %q (child failure should propagate)", state, StateFailed)
	}
	if err == nil || !strings.Contains(err.Error(), "child run") {
		t.Fatalf("expected error mentioning child run, got %v", err)
	}
	parentRun, err := rt.runStore.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	st, err := UnmarshalState(parentRun.DAGStateTOML)
	if err != nil {
		t.Fatalf("UnmarshalState parent: %v", err)
	}
	errNode := st.Nodes["_error"]
	if errNode == nil {
		t.Fatalf("parent _error not recorded: %+v", st.Nodes)
	}
	msg, _ := errNode["message"].(string)
	if !strings.Contains(msg, "child run") || !strings.Contains(msg, "failed") {
		t.Errorf("_error.message = %q, want it to mention child failure", msg)
	}
}

// TestDispatchConstellation_MissingRefRejected confirms that a constellation
// node with no Ref is a hard authoring error, not a silent no-op.
func TestDispatchConstellation_MissingRefRejected(t *testing.T) {
	ctx := context.Background()
	parent := &artifacts.Constellation{
		Name: "parent",
		Nodes: []artifacts.ConstellationNode{
			{ID: "delegate", Type: artifacts.NodeConstellation, Ref: ""},
		},
		Edges: []artifacts.ConstellationEdge{
			{From: "delegate", To: artifacts.TermDone},
		},
	}
	loader := &fakeLoader{cons: map[string]*artifacts.Constellation{"parent": parent}}
	rt, nebID := newTestRuntime(t, loader, nil)

	runID, err := rt.Fire(ctx, "parent", nebID, "", 0)
	if err != nil {
		t.Fatalf("Fire parent: %v", err)
	}
	state, err := rt.Step(ctx, runID)
	if state != StateFailed {
		t.Fatalf("parent state = %q, want %q (missing ref should fail the run)", state, StateFailed)
	}
	if err == nil || !strings.Contains(err.Error(), "missing ref") {
		t.Fatalf("expected error mentioning missing ref, got %v", err)
	}
}
