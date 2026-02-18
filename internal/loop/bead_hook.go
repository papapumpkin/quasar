package loop

import (
	"context"
	"fmt"

	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/ui"
)

// BeadHook translates loop lifecycle events into bead operations.
// It satisfies Hook, TaskCreator, and FindingCreator.
type BeadHook struct {
	Beads beads.Client
	UI    ui.UI
}

// Compile-time interface checks.
var (
	_ Hook           = (*BeadHook)(nil)
	_ TaskCreator    = (*BeadHook)(nil)
	_ FindingCreator = (*BeadHook)(nil)
)

// CreateTask creates a new task bead and returns its ID.
func (h *BeadHook) CreateTask(ctx context.Context, description string) (string, error) {
	return h.Beads.Create(ctx, description, beads.CreateOpts{
		Type:        "task",
		Labels:      []string{"quasar"},
		Description: description,
	})
}

// OnEvent dispatches a lifecycle event to the appropriate bead operation.
func (h *BeadHook) OnEvent(ctx context.Context, event Event) {
	switch event.Kind {
	case EventCycleStart:
		h.beadUpdate(ctx, event.BeadID, beads.UpdateOpts{
			Status:   "in_progress",
			Assignee: "quasar-coder",
		})

	case EventAgentDone:
		h.beadComment(ctx, event.BeadID, event.Message)

	case EventRefactored:
		h.beadComment(ctx, event.BeadID, event.Message)

	case EventReviewComplete:
		h.beadUpdate(ctx, event.BeadID, beads.UpdateOpts{Assignee: "quasar-coder"})

	case EventTaskSuccess:
		h.beadClose(ctx, event.BeadID, "Approved by reviewer")
		if event.Report != nil {
			h.beadComment(ctx, event.BeadID, FormatReportComment(event.Report))
		}

	case EventTaskFailed:
		h.beadComment(ctx, event.BeadID, event.Message)
	}
}

// CreateFindingChildIDs creates a child bead for each review finding and returns
// the IDs of successfully created beads.
func (h *BeadHook) CreateFindingChildIDs(ctx context.Context, parentBeadID string, findings []ReviewFinding) []string {
	var ids []string
	for _, f := range findings {
		childID, err := h.Beads.Create(ctx,
			fmt.Sprintf("[%s] %s", f.Severity, truncate(f.Description, 80)),
			beads.CreateOpts{
				Type:        "bug",
				Labels:      []string{"quasar", "review-finding"},
				Parent:      parentBeadID,
				Description: f.Description,
			},
		)
		if err != nil {
			h.UI.Error(fmt.Sprintf("failed to create child bead: %v", err))
			continue
		}
		ids = append(ids, childID)
	}
	return ids
}

// beadComment logs a comment on the bead, logging any error.
func (h *BeadHook) beadComment(ctx context.Context, beadID, body string) {
	if err := h.Beads.AddComment(ctx, beadID, body); err != nil {
		h.UI.Error(fmt.Sprintf("failed to add bead comment: %v", err))
	}
}

// beadUpdate updates the bead, logging any error.
func (h *BeadHook) beadUpdate(ctx context.Context, beadID string, opts beads.UpdateOpts) {
	if err := h.Beads.Update(ctx, beadID, opts); err != nil {
		h.UI.Error(fmt.Sprintf("failed to update bead: %v", err))
	}
}

// beadClose closes the bead with a reason, logging any error.
func (h *BeadHook) beadClose(ctx context.Context, beadID, reason string) {
	if err := h.Beads.Close(ctx, beadID, reason); err != nil {
		h.UI.Error(fmt.Sprintf("failed to close bead: %v", err))
	}
}
