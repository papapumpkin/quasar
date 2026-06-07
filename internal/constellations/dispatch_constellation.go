package constellations

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// dispatchConstellation runs a child constellation referenced by node.Ref to
// completion, then returns the child's declared [outputs] block evaluated
// against the child's final state. Nested constellations are the primitive
// that lets master-review call coder-reviewer as a real inner loop instead of
// routing within-cap fixes to _awaiting_human; the PLACEHOLDER edge in
// master-review.toml exists only because this dispatch did not.
//
// Synchronous: the parent's Step blocks until the child reaches a terminal
// run state. A child that ends 'failed' propagates as a parent-node error so
// the parent's failure-path edges fire; a child that ends 'done' or
// 'awaiting_human' returns its [outputs] block plus a `state` key holding the
// terminal state name and a `run_id` key holding the child's run ID, so parent
// edges can route on outputs.state and the TUI can drill from parent to child.
func (r *Runtime) dispatchConstellation(ctx context.Context, parent *fabric.RunRow, parentSt *State, node *artifacts.ConstellationNode) (map[string]any, error) {
	if node.Ref == "" {
		return nil, fmt.Errorf("constellations: constellation node %q missing ref", node.ID)
	}
	inputs, err := evalInputs(node, parentSt.ExprState())
	if err != nil {
		return nil, err
	}
	childRunID, err := r.Fire(ctx, node.Ref, parent.NebulaID, parent.ID, 0)
	if err != nil {
		return nil, fmt.Errorf("constellations: fire child %q: %w", node.Ref, err)
	}
	// Seed the child's State.Inputs so child node expressions can reference
	// inputs.<name>. We re-read/mutate/re-write the persisted state rather
	// than threading inputs through Fire's signature; that keeps Fire's API
	// stable and isolates child-only seeding here.
	if len(inputs) > 0 {
		if err := r.seedChildInputs(ctx, childRunID, inputs); err != nil {
			return nil, fmt.Errorf("constellations: seed child %q inputs: %w", childRunID, err)
		}
	}
	// Drive the child to terminal. Each Step fires one node; back-edges
	// increment the child's own cycle counter, so the child's max_cycles cap
	// eventually trips and routes through give-up → _failed if its loop never
	// converges. We re-check the run state after each step rather than
	// trusting Step's return value alone, so a child that transitions to
	// awaiting_human exits the loop on the same tick it transitioned.
	for {
		state, err := r.Step(ctx, childRunID)
		if err != nil {
			if errors.Is(err, ErrTerminal) {
				break
			}
			return nil, fmt.Errorf("constellations: step child %q: %w", childRunID, err)
		}
		if isTerminalState(state) {
			break
		}
	}
	// Project the child constellation's [outputs] block against the child's
	// final state. A failed-eval output is logged and dropped — outputs.state
	// is always present so the parent's edge guards still route on the
	// terminal state.
	childRun, err := r.runStore.GetRun(ctx, childRunID)
	if err != nil {
		return nil, err
	}
	childSt, err := UnmarshalState(childRun.DAGStateTOML)
	if err != nil {
		return nil, fmt.Errorf("constellations: unmarshal child state: %w", err)
	}
	childCon, err := r.loader.LoadConstellation(node.Ref)
	if err != nil {
		return nil, fmt.Errorf("constellations: reload child constellation %q: %w", node.Ref, err)
	}
	outputs := map[string]any{
		"state":  childRun.State,
		"run_id": childRunID,
	}
	exprSt := childSt.ExprState()
	for k, expr := range childCon.Outputs {
		v, err := expr.Eval(exprSt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "constellations: eval child output %q (run %s): %v\n", k, childRunID, err)
			continue
		}
		outputs[k] = v
	}
	if childRun.State == StateFailed {
		return nil, fmt.Errorf("constellations: child run %q (%s) failed", childRunID, node.Ref)
	}
	return outputs, nil
}

// seedChildInputs writes evaluated inputs into a child run's State.Inputs and
// persists the modified DAG state, so the child's first Step sees the seeded
// inputs through its ExprState. Called only by dispatchConstellation.
func (r *Runtime) seedChildInputs(ctx context.Context, runID string, inputs map[string]any) error {
	run, err := r.runStore.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	st, err := UnmarshalState(run.DAGStateTOML)
	if err != nil {
		return err
	}
	if st.Inputs == nil {
		st.Inputs = map[string]any{}
	}
	for k, v := range inputs {
		st.Inputs[k] = v
	}
	dag, err := MarshalState(st)
	if err != nil {
		return err
	}
	run.DAGStateTOML = dag
	return r.runStore.SaveProgress(ctx, run)
}
