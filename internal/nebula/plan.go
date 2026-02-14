package nebula

import (
	"fmt"

	"github.com/aaronsalm/quasar/internal/beads"
)

// BuildPlan diffs the desired nebula state against actual beads state,
// producing a plan of create/update/skip/close actions.
func BuildPlan(n *Nebula, state *State, client beads.BeadsClient) (*Plan, error) {
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
