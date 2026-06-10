package constellations

import (
	"context"
	"errors"
	"fmt"

	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// dispatchPhaseIterator fans a child sub_constellation out over the nebula's
// phases. It is the dispatch site for `type = "phase_iterator"` nodes — the
// canonical `nebula-lifecycle` constellation's first node — and the
// load-bearing piece that turns an architect's persisted phase list into a
// chain of coder-reviewer runs.
//
// v1 is SEQUENTIAL: phases run one at a time. The node's `parallel` input
// is read but ignored. Parallel dispatch needs per-phase worktree isolation,
// concurrent Step drivers, and explicit termination ordering — those are
// follow-on work, not v1 scope. Sequential keeps the data model honest and
// matches how a single-coder operator wants the cost curve.
//
// Each iteration:
//  1. Fire a child run for sub_constellation against the parent's nebula
//  2. Seed the child's State.Nebula.CurrentPhase so coder-reviewer.toml's
//     `nebula.current_phase.body`/`.title` bindings resolve
//  3. Drive the child to terminal via repeated Step
//  4. If the child terminates non-done, fail the iteration and propagate
//
// On full success: returns {"all_phases_complete": true,
// "phases_executed": <N>}. The nebula-lifecycle edge guard
// `execute_phases.all_phases_complete` routes on that field.
func (r *Runtime) dispatchPhaseIterator(ctx context.Context, parent *fabric.RunRow, parentSt *State, node *artifacts.ConstellationNode) (map[string]any, error) {
	inputs, err := evalInputs(node, parentSt.ExprState())
	if err != nil {
		return nil, err
	}
	subName, _ := inputs["sub_constellation"].(string)
	if subName == "" {
		return nil, fmt.Errorf("phase_iterator: node %q missing sub_constellation input", node.ID)
	}

	neb, err := r.nebStore.Get(ctx, parent.NebulaID)
	if err != nil {
		return nil, fmt.Errorf("phase_iterator: load nebula %q: %w", parent.NebulaID, err)
	}
	if len(neb.Phases) == 0 {
		// A nebula with zero persisted phases is a valid (if unusual) terminal
		// state for this iterator — the architect declined to decompose the
		// seed. Downstream lifecycle nodes still need their edge guard to
		// evaluate truthy, so report all_phases_complete=true with zero
		// executed.
		return map[string]any{
			"all_phases_complete": true,
			"phases_executed":     0,
		}, nil
	}

	executed := 0
	var firstFailure string
	for _, p := range neb.Phases {
		childRunID, err := r.Fire(ctx, subName, parent.NebulaID, parent.ID, 0)
		if err != nil {
			return nil, fmt.Errorf("phase_iterator: fire %q for phase %q: %w", subName, p.ID, err)
		}
		if err := r.seedChildPhaseContext(ctx, childRunID, p); err != nil {
			return nil, fmt.Errorf("phase_iterator: seed phase context for %q: %w", p.ID, err)
		}
		if err := driveChildToTerminal(ctx, r, childRunID); err != nil {
			return nil, fmt.Errorf("phase_iterator: drive phase %q: %w", p.ID, err)
		}
		childRun, err := r.runStore.GetRun(ctx, childRunID)
		if err != nil {
			return nil, fmt.Errorf("phase_iterator: load child run for %q: %w", p.ID, err)
		}
		if childRun.State != StateDone {
			// Non-done terminal child = iteration failure. Record which phase
			// blocked the chain so the parent's _error node carries actionable
			// context.
			firstFailure = p.ID
			break
		}
		executed++
	}

	if firstFailure != "" {
		return nil, fmt.Errorf("phase_iterator: phase %q terminated non-done; %d of %d completed",
			firstFailure, executed, len(neb.Phases))
	}
	return map[string]any{
		"all_phases_complete": true,
		"phases_executed":     executed,
	}, nil
}

// driveChildToTerminal steps a child run until it reaches a terminal state or
// the context is canceled. It mirrors the loop in dispatchConstellation,
// extracted so both nested-constellation and phase_iterator dispatchers share
// the same drive semantics.
func driveChildToTerminal(ctx context.Context, r *Runtime, childRunID string) error {
	for {
		state, err := r.Step(ctx, childRunID)
		if err != nil {
			if errors.Is(err, ErrTerminal) {
				return nil
			}
			return err
		}
		if isTerminalState(state) {
			return nil
		}
	}
}

// seedChildPhaseContext sets the child run's State.Nebula.CurrentPhase to the
// given phase so the child's expression bindings (e.g. nebula.current_phase.
// body) resolve. Re-reads + re-marshals the child's DAG state in the same
// transaction-shape pattern as seedChildInputs.
func (r *Runtime) seedChildPhaseContext(ctx context.Context, runID string, p fabric.Phase) error {
	run, err := r.runStore.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	st, err := UnmarshalState(run.DAGStateTOML)
	if err != nil {
		return err
	}
	st.Nebula.CurrentPhase = &PhaseExec{
		ID:    p.ID,
		Title: p.Title,
		Body:  p.Body,
	}
	dag, err := MarshalState(st)
	if err != nil {
		return err
	}
	run.DAGStateTOML = dag
	return r.runStore.SaveProgress(ctx, run)
}
