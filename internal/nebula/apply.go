package nebula

import (
	"context"
	"fmt"
)

// Apply executes a plan's actions, updating phase tracking state and persisting
// after each successful action.
func Apply(ctx context.Context, plan *Plan, n *Nebula, state *State) error {
	state.NebulaName = plan.NebulaName

	phasesByID := PhasesByID(n.Phases)

	for _, action := range plan.Actions {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := applyAction(action, phasesByID, n.Dir, state); err != nil {
			return err
		}
	}
	return nil
}

// applyAction dispatches a single plan action to the appropriate handler.
func applyAction(action Action, phasesByID map[string]*PhaseSpec, dir string, state *State) error {
	switch action.Type {
	case ActionSkip:
		return nil
	case ActionCreate, ActionRetry:
		phase := phasesByID[action.PhaseID]
		if phase == nil {
			return nil
		}
		return applyCreatePhase(phase, state, dir)
	case ActionUpdate:
		return applyUpdatePhase(phasesByID[action.PhaseID], state, dir)
	case ActionClose:
		return applyClosePhase(action, state, dir)
	}
	return nil
}

// applyCreatePhase records a new phase tracking entry and persists state.
// The phase's own ID is used as the tracking ID (no external bead system).
func applyCreatePhase(phase *PhaseSpec, state *State, dir string) error {
	state.SetPhaseState(phase.ID, phase.ID, PhaseStatusCreated)
	if err := SaveState(dir, state); err != nil {
		return fmt.Errorf("saving state after creating %q: %w", phase.ID, err)
	}
	return nil
}

// applyUpdatePhase persists the phase tracking entry without altering status.
// Retained for backward compatibility with existing plan flows.
func applyUpdatePhase(phase *PhaseSpec, state *State, dir string) error {
	if phase == nil {
		return nil
	}
	ps := state.Phases[phase.ID]
	if ps == nil {
		return nil
	}
	state.SetPhaseState(phase.ID, ps.BeadID, ps.Status)
	if err := SaveState(dir, state); err != nil {
		return fmt.Errorf("saving state after updating %q: %w", phase.ID, err)
	}
	return nil
}

// applyClosePhase marks a phase tracking entry as done and persists state.
func applyClosePhase(action Action, state *State, dir string) error {
	ps := state.Phases[action.PhaseID]
	if ps == nil {
		return nil
	}
	state.SetPhaseState(action.PhaseID, ps.BeadID, PhaseStatusDone)
	if err := SaveState(dir, state); err != nil {
		return fmt.Errorf("saving state after closing %q: %w", action.PhaseID, err)
	}
	return nil
}
