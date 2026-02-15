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
		if err := applyAction(ctx, action, tasksByID, n.Dir, state, client); err != nil {
			return err
		}
	}
	return nil
}

// applyAction dispatches a single plan action to the appropriate handler.
func applyAction(ctx context.Context, action Action, tasksByID map[string]*TaskSpec, dir string, state *State, client beads.BeadsClient) error {
	switch action.Type {
	case ActionSkip:
		return nil
	case ActionCreate, ActionRetry:
		task := tasksByID[action.TaskID]
		if task == nil {
			return nil
		}
		return applyCreateBead(ctx, client, task, state, dir)
	case ActionUpdate:
		return applyUpdateBead(ctx, client, tasksByID[action.TaskID], state, dir)
	case ActionClose:
		return applyCloseBead(ctx, client, action, state, dir)
	}
	return nil
}

// applyCreateBead creates a new bead for a task and persists state.
// Used for both ActionCreate and ActionRetry.
func applyCreateBead(ctx context.Context, client beads.BeadsClient, task *TaskSpec, state *State, dir string) error {
	beadID, err := client.Create(ctx, task.Title, beads.CreateOpts{
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
	if err := SaveState(dir, state); err != nil {
		return fmt.Errorf("saving state after creating %q: %w", task.ID, err)
	}
	return nil
}

// applyUpdateBead updates an existing bead's assignee and persists state.
func applyUpdateBead(ctx context.Context, client beads.BeadsClient, task *TaskSpec, state *State, dir string) error {
	if task == nil {
		return nil
	}
	ts := state.Tasks[task.ID]
	if ts == nil || ts.BeadID == "" {
		return nil
	}
	if task.Assignee != "" {
		if err := client.Update(ctx, ts.BeadID, beads.UpdateOpts{Assignee: task.Assignee}); err != nil {
			return fmt.Errorf("updating bead %s for task %q: %w", ts.BeadID, task.ID, err)
		}
	}
	state.SetTaskState(task.ID, ts.BeadID, ts.Status)
	if err := SaveState(dir, state); err != nil {
		return fmt.Errorf("saving state after updating %q: %w", task.ID, err)
	}
	return nil
}

// applyCloseBead closes an existing bead and persists state.
func applyCloseBead(ctx context.Context, client beads.BeadsClient, action Action, state *State, dir string) error {
	ts := state.Tasks[action.TaskID]
	if ts == nil || ts.BeadID == "" {
		return nil
	}
	if err := client.Close(ctx, ts.BeadID, action.Reason); err != nil {
		return fmt.Errorf("closing bead %s for task %q: %w", ts.BeadID, action.TaskID, err)
	}
	state.SetTaskState(action.TaskID, ts.BeadID, TaskStatusDone)
	if err := SaveState(dir, state); err != nil {
		return fmt.Errorf("saving state after closing %q: %w", action.TaskID, err)
	}
	return nil
}

func priorityStr(p int) string {
	if p == 0 {
		return ""
	}
	return strconv.Itoa(p)
}
