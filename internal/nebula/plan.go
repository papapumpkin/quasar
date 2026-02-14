package nebula

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/aaronsalm/quasar/internal/beads"
)

// CheckDependencies verifies that all external dependencies declared in the nebula are met.
// For requires_beads: each bead must exist and be closed.
// For requires_nebulae: each named nebula's state file must show all tasks done.
func CheckDependencies(deps Dependencies, nebulaDir string, client beads.BeadsClient) error {
	var unmet []string

	for _, beadID := range deps.RequiresBeads {
		b, err := client.Show(beadID)
		if err != nil {
			unmet = append(unmet, fmt.Sprintf("bead %q: %v", beadID, err))
			continue
		}
		if b.Status != "closed" {
			unmet = append(unmet, fmt.Sprintf("bead %q: status is %q, expected closed", beadID, b.Status))
		}
	}

	for _, name := range deps.RequiresNebulae {
		stateDir := filepath.Join(filepath.Dir(nebulaDir), name)
		st, err := LoadState(stateDir)
		if err != nil {
			unmet = append(unmet, fmt.Sprintf("nebula %q: cannot load state: %v", name, err))
			continue
		}
		if len(st.Tasks) == 0 {
			unmet = append(unmet, fmt.Sprintf("nebula %q: no tasks found in state", name))
			continue
		}
		for taskID, ts := range st.Tasks {
			if ts.Status != TaskStatusDone {
				unmet = append(unmet, fmt.Sprintf("nebula %q: task %q is %s, not done", name, taskID, ts.Status))
			}
		}
	}

	if len(unmet) > 0 {
		return fmt.Errorf("%w: %s", ErrUnmetDependency, strings.Join(unmet, "; "))
	}
	return nil
}

// BuildPlan diffs the desired nebula state against actual beads state,
// producing a plan of create/update/skip/close actions.
func BuildPlan(n *Nebula, state *State, client beads.BeadsClient) (*Plan, error) {
	// Check external dependencies before building the plan.
	deps := n.Manifest.Dependencies
	if len(deps.RequiresBeads) > 0 || len(deps.RequiresNebulae) > 0 {
		if err := CheckDependencies(deps, n.Dir, client); err != nil {
			return nil, err
		}
	}

	plan := &Plan{
		NebulaName: n.Manifest.Nebula.Name,
	}

	// Build set of desired task IDs.
	desired := make(map[string]bool)
	for _, t := range n.Tasks {
		desired[t.ID] = true
	}

	// Determine action for each task in the nebula.
	for _, t := range n.Tasks {
		ts, exists := state.Tasks[t.ID]

		if !exists {
			plan.Actions = append(plan.Actions, Action{
				TaskID: t.ID,
				Type:   ActionCreate,
				Reason: fmt.Sprintf("create bead for %q", t.Title),
			})
			continue
		}

		// Locked tasks (in_progress) are skipped.
		if ts.Status == TaskStatusInProgress {
			plan.Actions = append(plan.Actions, Action{
				TaskID: t.ID,
				Type:   ActionSkip,
				Reason: "locked (in_progress)",
			})
			continue
		}

		// Already done tasks are skipped.
		if ts.Status == TaskStatusDone {
			plan.Actions = append(plan.Actions, Action{
				TaskID: t.ID,
				Type:   ActionSkip,
				Reason: "already completed",
			})
			continue
		}

		// Task exists in state but bead may need updating.
		if ts.BeadID != "" {
			// Verify bead still exists.
			_, err := client.Show(ts.BeadID)
			if err != nil {
				// Bead missing — recreate.
				plan.Actions = append(plan.Actions, Action{
					TaskID: t.ID,
					Type:   ActionCreate,
					Reason: fmt.Sprintf("bead %s not found, recreating", ts.BeadID),
				})
				continue
			}

			plan.Actions = append(plan.Actions, Action{
				TaskID: t.ID,
				Type:   ActionUpdate,
				Reason: fmt.Sprintf("update bead %s", ts.BeadID),
			})
			continue
		}

		// State entry without bead ID — create.
		plan.Actions = append(plan.Actions, Action{
			TaskID: t.ID,
			Type:   ActionCreate,
			Reason: fmt.Sprintf("create bead for %q (no bead ID in state)", t.Title),
		})
	}

	// Tasks in state that are no longer in the nebula → close.
	for taskID, ts := range state.Tasks {
		if desired[taskID] {
			continue
		}
		if ts.Status == TaskStatusDone {
			continue
		}
		if ts.BeadID != "" {
			plan.Actions = append(plan.Actions, Action{
				TaskID: taskID,
				Type:   ActionClose,
				Reason: "task removed from nebula",
			})
		}
	}

	return plan, nil
}

// HasChanges returns true if the plan contains any non-skip actions.
func (p *Plan) HasChanges() bool {
	for _, a := range p.Actions {
		if a.Type != ActionSkip {
			return true
		}
	}
	return false
}
