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
	// EventStruggleDetected is emitted when the struggle detector triggers,
	// signaling that the phase should be decomposed.
	EventStruggleDetected
	// EventFilterFixAttempt is emitted at each inner fix loop iteration.
	EventFilterFixAttempt
	// EventFilterFixResult is emitted when the inner fix loop concludes,
	// whether by success or retry exhaustion.
	EventFilterFixResult
	// EventCacheMetrics is emitted after each agent invocation with prompt
	// size and hash information for tracking prompt cache effectiveness.
	EventCacheMetrics
	// EventResumed is emitted when the loop resumes from a checkpoint,
	// before re-entering the coder-reviewer cycle.
	EventResumed
)

// Event represents a lifecycle event in the coder-reviewer loop.
type Event struct {
	Kind      EventKind
	Cycle     int
	Agent     string // "coder" or "reviewer"
	BeadID    string
	Result    *agent.InvocationResult
	Findings  []ReviewFinding
	Report    *agent.ReviewReport
	Message   string         // Free-form message (e.g., refactor comment, max-cycles note).
	FilterFix *FilterFixData // Populated for EventFilterFixAttempt and EventFilterFixResult.
}

// FilterFixData carries metadata about a filter fix attempt or result.
type FilterFixData struct {
	CheckName   string  // Which filter check failed ("build", "vet", "lint", "test").
	Attempt     int     // 1-based attempt number.
	MaxAttempts int     // Maximum attempts configured.
	Fixed       bool    // True if the check passed after this attempt (or overall for result).
	CostUSD     float64 // Cost of this fix invocation (attempt) or total fix cost (result).
	ErrorCount  int     // Number of structured errors parsed from the check output.
	DurationMs  int64   // Wall-clock time for this fix attempt.
}

// Hook receives lifecycle events from the loop. Implementations must not block.
type Hook interface {
	OnEvent(ctx context.Context, event Event)
}

// HookFunc adapts a plain function to the Hook interface.
type HookFunc func(ctx context.Context, event Event)

// OnEvent calls the wrapped function.
func (f HookFunc) OnEvent(ctx context.Context, event Event) { f(ctx, event) }

// TaskCreator creates a new task bead and returns its ID. This is separate
// from Hook because the loop needs the return value to proceed.
type TaskCreator interface {
	CreateTask(ctx context.Context, description string) (string, error)
}

// FindingCreator creates child beads for review findings and returns
// their IDs. This is separate from Hook because the loop needs the return values.
type FindingCreator interface {
	CreateFindingChildIDs(ctx context.Context, parentBeadID string, findings []ReviewFinding) []string
}
