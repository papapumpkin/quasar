package nebula

import (
	"context"
	"fmt"
	"strconv"

	"github.com/aaronsalm/quasar/internal/beads"
)

// Apply executes a plan's actions, creating/updating/closing beads and
// persisting state after each successful action.
func Apply(ctx context.Context, plan *Plan, n *Nebula, state *State, client beads.Client) error {
	state.NebulaName = plan.NebulaName

	phasesByID := make(map[string]*PhaseSpec)
	for i := range n.Phases {
		phasesByID[n.Phases[i].ID] = &n.Phases[i]
	}

	for _, action := range plan.Actions {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := applyAction(ctx, action, phasesByID, n.Dir, state, client); err != nil {
			return err
		}
	}
	return nil
}

// applyAction dispatches a single plan action to the appropriate handler.
func applyAction(ctx context.Context, action Action, phasesByID map[string]*PhaseSpec, dir string, state *State, client beads.Client) error {
	switch action.Type {
	case ActionSkip:
		return nil
	case ActionCreate, ActionRetry:
		phase := phasesByID[action.PhaseID]
		if phase == nil {
			return nil
		}
		return applyCreateBead(ctx, client, phase, state, dir)
	case ActionUpdate:
		return applyUpdateBead(ctx, client, phasesByID[action.PhaseID], state, dir)
	case ActionClose:
		return applyCloseBead(ctx, client, action, state, dir)
	}
	return nil
}

// applyCreateBead creates a new bead for a phase and persists state.
// Used for both ActionCreate and ActionRetry.
func applyCreateBead(ctx context.Context, client beads.Client, phase *PhaseSpec, state *State, dir string) error {
	beadID, err := client.Create(ctx, phase.Title, beads.CreateOpts{
		Description: phase.Body,
		Type:        phase.Type,
		Labels:      phase.Labels,
		Assignee:    phase.Assignee,
		Priority:    priorityStr(phase.Priority),
	})
	if err != nil {
		return fmt.Errorf("creating bead for phase %q: %w", phase.ID, err)
	}
	state.SetPhaseState(phase.ID, beadID, PhaseStatusCreated)
	if err := SaveState(dir, state); err != nil {
		return fmt.Errorf("saving state after creating %q: %w", phase.ID, err)
	}
	return nil
}

// applyUpdateBead updates an existing bead's assignee and persists state.
func applyUpdateBead(ctx context.Context, client beads.Client, phase *PhaseSpec, state *State, dir string) error {
	if phase == nil {
		return nil
	}
	ps := state.Phases[phase.ID]
	if ps == nil || ps.BeadID == "" {
		return nil
	}
	if phase.Assignee != "" {
		if err := client.Update(ctx, ps.BeadID, beads.UpdateOpts{Assignee: phase.Assignee}); err != nil {
			return fmt.Errorf("updating bead %s for phase %q: %w", ps.BeadID, phase.ID, err)
		}
	}
	state.SetPhaseState(phase.ID, ps.BeadID, ps.Status)
	if err := SaveState(dir, state); err != nil {
		return fmt.Errorf("saving state after updating %q: %w", phase.ID, err)
	}
	return nil
}

// applyCloseBead closes an existing bead and persists state.
func applyCloseBead(ctx context.Context, client beads.Client, action Action, state *State, dir string) error {
	ps := state.Phases[action.PhaseID]
	if ps == nil || ps.BeadID == "" {
		return nil
	}
	if err := client.Close(ctx, ps.BeadID, action.Reason); err != nil {
		return fmt.Errorf("closing bead %s for phase %q: %w", ps.BeadID, action.PhaseID, err)
	}
	state.SetPhaseState(action.PhaseID, ps.BeadID, PhaseStatusDone)
	if err := SaveState(dir, state); err != nil {
		return fmt.Errorf("saving state after closing %q: %w", action.PhaseID, err)
	}
	return nil
}

func priorityStr(p int) string {
	if p == 0 {
		return ""
	}
	return strconv.Itoa(p)
}
