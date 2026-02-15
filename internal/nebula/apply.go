package nebula

import (
	"context"
	"fmt"
	"strconv"

	"github.com/aaronsalm/quasar/internal/beads"
)

// Apply executes a plan's actions, creating/updating/closing beads and
// persisting state after each successful action.
func Apply(ctx context.Context, plan *Plan, n *Nebula, state *State, client beads.BeadsClient) error {
	state.NebulaName = plan.NebulaName

	tasksByID := make(map[string]*TaskSpec)
	for i := range n.Tasks {
		tasksByID[n.Tasks[i].ID] = &n.Tasks[i]
	}

	for _, action := range plan.Actions {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		switch action.Type {
		case ActionSkip:
			continue

		case ActionCreate:
			task := tasksByID[action.TaskID]
			if task == nil {
				continue
			}

			beadID, err := client.Create(task.Title, beads.CreateOpts{
				Description: task.Body,
				Type:        task.Type,
				Labels:      task.Labels,
				Assignee:    task.Assignee,
				Priority:    priorityStr(task.Priority),
			})
			if err != nil {
				return fmt.Errorf("creating bead for task %q: %w", task.ID, err)
			}

			state.SetTaskState(task.ID, beadID, TaskStatusCreated)
			if err := SaveState(n.Dir, state); err != nil {
				return fmt.Errorf("saving state after creating %q: %w", task.ID, err)
			}

		case ActionUpdate:
			task := tasksByID[action.TaskID]
			if task == nil {
				continue
			}
			ts := state.Tasks[task.ID]
			if ts == nil || ts.BeadID == "" {
				continue
			}

			// Update bead assignee if changed.
			if task.Assignee != "" {
				if err := client.Update(ts.BeadID, beads.UpdateOpts{Assignee: task.Assignee}); err != nil {
					return fmt.Errorf("updating bead %s for task %q: %w", ts.BeadID, task.ID, err)
				}
			}

			state.SetTaskState(task.ID, ts.BeadID, ts.Status)
			if err := SaveState(n.Dir, state); err != nil {
				return fmt.Errorf("saving state after updating %q: %w", task.ID, err)
			}

		case ActionRetry:
			task := tasksByID[action.TaskID]
			if task == nil {
				continue
			}

			// Create a new bead for the retry (don't reuse the failed bead).
			beadID, err := client.Create(task.Title, beads.CreateOpts{
				Description: task.Body,
				Type:        task.Type,
				Labels:      task.Labels,
				Assignee:    task.Assignee,
				Priority:    priorityStr(task.Priority),
			})
			if err != nil {
				return fmt.Errorf("creating retry bead for task %q: %w", task.ID, err)
			}

			state.SetTaskState(task.ID, beadID, TaskStatusCreated)
			if err := SaveState(n.Dir, state); err != nil {
				return fmt.Errorf("saving state after retrying %q: %w", task.ID, err)
			}

		case ActionClose:
			ts := state.Tasks[action.TaskID]
			if ts == nil || ts.BeadID == "" {
				continue
			}

			if err := client.Close(ts.BeadID, action.Reason); err != nil {
				return fmt.Errorf("closing bead %s for removed task %q: %w", ts.BeadID, action.TaskID, err)
			}

			state.SetTaskState(action.TaskID, ts.BeadID, TaskStatusDone)
			if err := SaveState(n.Dir, state); err != nil {
				return fmt.Errorf("saving state after closing %q: %w", action.TaskID, err)
			}
		}
	}

	return nil
}

func priorityStr(p int) string {
	if p == 0 {
		return ""
	}
	return strconv.Itoa(p)
}
