package loop

import (
	"context"

	"github.com/papapumpkin/quasar/internal/agent"
)

// EventKind identifies the type of lifecycle event in the coder-reviewer loop.
type EventKind int

const (
	// EventCycleStart is emitted at the beginning of each coder-reviewer cycle.
	EventCycleStart EventKind = iota
	// EventAgentDone is emitted after an agent (coder or reviewer) completes.
	EventAgentDone
	// EventReviewComplete is emitted after findings are parsed and child beads created.
	EventReviewComplete
	// EventTaskSuccess is emitted when the reviewer approves the changes.
	EventTaskSuccess
	// EventTaskFailed is emitted when the loop terminates without approval.
	EventTaskFailed
	// EventRefactored is emitted when a mid-run phase edit is applied.
	EventRefactored
)

// Event represents a lifecycle event in the coder-reviewer loop.
type Event struct {
	Kind     EventKind
	Cycle    int
	Agent    string // "coder" or "reviewer"
	BeadID   string
	Result   *agent.InvocationResult
	Findings []ReviewFinding
	Report   *agent.ReviewReport
	Message  string // Free-form message (e.g., refactor comment, max-cycles note).
}

// Hook receives lifecycle events from the loop. Implementations must not block.
type Hook interface {
	OnEvent(ctx context.Context, event Event)
}

// HookFunc adapts a plain function to the Hook interface.
type HookFunc func(ctx context.Context, event Event)

// OnEvent calls the wrapped function.
func (f HookFunc) OnEvent(ctx context.Context, event Event) { f(ctx, event) }

// FindingCreator creates child beads for review findings and returns
// their IDs. This is separate from Hook because the loop needs the return values.
type FindingCreator interface {
	CreateFindingChildIDs(ctx context.Context, parentBeadID string, findings []ReviewFinding) []string
}
