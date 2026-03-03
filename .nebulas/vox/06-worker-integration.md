+++
id = "worker-integration"
title = "Workers check feedback queue between coder-reviewer cycles"
type = "feature"
priority = 1
depends_on = ["advisor-agent"]
scope = ["internal/nebula/worker.go", "internal/nebula/worker_options.go", "internal/nebula/worker_feedback.go"]
allow_scope_overlap = true
+++

## Problem

The `FeedbackQueue` and `Advisor` exist but workers don't know about them. The `WorkerGroup.Run` loop executes phases sequentially within each worker, calling `PhaseRunner.RunExistingPhase` which internally runs coder-reviewer cycles. Between cycles (or between phases), there is currently no check for pending feedback.

Workers need to poll the feedback queue at natural breakpoints — after a coder or reviewer cycle completes, before starting the next — and apply any pending actions. Some actions (refactor-phase, add-constraint) modify the phase description and are handled via the existing `HotReloader` machinery. Others (pause, skip, adjust-budget) require direct state changes.

## Solution

### Add feedback queue to WorkerGroup

```go
// In internal/nebula/worker_options.go:

// WithFeedbackQueue sets the feedback queue for mid-execution feedback.
func WithFeedbackQueue(q *feedback.FeedbackQueue) Option {
    return func(wg *WorkerGroup) {
        wg.feedbackQueue = q
    }
}
```

Add the field to `WorkerGroup`:

```go
// In worker.go WorkerGroup struct:
feedbackQueue *feedback.FeedbackQueue
```

### FeedbackApplier

Create `internal/nebula/worker_feedback.go` with the action application logic:

```go
// internal/nebula/worker_feedback.go

package nebula

import (
    "context"
    "fmt"
    "strconv"

    "github.com/aaronsalm/quasar/internal/feedback"
)

// FeedbackApplier processes feedback actions for a running phase.
// It bridges the feedback system with the nebula execution machinery.
type FeedbackApplier struct {
    queue      *feedback.FeedbackQueue
    hotReloader *HotReloader
    nebula     *Nebula
    state      *State
    logger     io.Writer
}

// NewFeedbackApplier creates an applier connected to the given queue
// and hot-reloader.
func NewFeedbackApplier(q *feedback.FeedbackQueue, hr *HotReloader, n *Nebula, s *State, logger io.Writer) *FeedbackApplier {
    return &FeedbackApplier{
        queue:       q,
        hotReloader: hr,
        nebula:      n,
        state:       s,
        logger:      logger,
    }
}

// DrainAndApply checks the feedback queue for pending actions targeting
// the given phase (or nebula-wide actions) and applies them. Returns
// true if the phase should be skipped, false to continue.
//
// Called by workers between cycles. Non-blocking — only processes
// actions already in the queue.
func (fa *FeedbackApplier) DrainAndApply(ctx context.Context, phaseID string) (skip bool) {
    if fa.queue == nil {
        return false
    }

    for {
        action, ok := fa.queue.TryDequeue()
        if !ok {
            break
        }

        // Filter: only apply actions targeting this phase or nebula-wide.
        if action.PhaseID != "" && action.PhaseID != phaseID {
            // Re-enqueue actions targeting other phases.
            fa.queue.Enqueue(action)
            continue
        }

        applied := fa.applyAction(ctx, phaseID, action)
        if applied && action.Kind == feedback.ActionSkip {
            return true
        }
    }
    return false
}
```

### Action handlers

```go
func (fa *FeedbackApplier) applyAction(ctx context.Context, phaseID string, action feedback.FeedbackAction) bool {
    switch action.Kind {

    case feedback.ActionRefactorPhase:
        // Inject new instructions into the phase description via HotReloader.
        // This triggers the existing refactor channel mechanism.
        if fa.hotReloader != nil {
            phase := fa.findPhase(phaseID)
            if phase != nil {
                phase.Body += "\n\n## Additional instructions (from feedback)\n\n" + action.Payload
                fa.hotReloader.NotifyPhaseModified(phaseID)
            }
        }
        fmt.Fprintf(fa.logger, "[feedback] refactored phase %s\n", phaseID)
        return true

    case feedback.ActionAdjustPriority:
        priority, err := strconv.Atoi(action.Payload)
        if err == nil {
            phase := fa.findPhase(phaseID)
            if phase != nil {
                phase.Priority = priority
            }
        }
        fmt.Fprintf(fa.logger, "[feedback] adjusted priority for %s to %s\n", phaseID, action.Payload)
        return true

    case feedback.ActionAdjustBudget:
        budget, err := strconv.ParseFloat(action.Payload, 64)
        if err == nil {
            phase := fa.findPhase(phaseID)
            if phase != nil {
                phase.MaxBudgetUSD = budget
            }
        }
        fmt.Fprintf(fa.logger, "[feedback] adjusted budget for %s to %s\n", phaseID, action.Payload)
        return true

    case feedback.ActionAddConstraint:
        phase := fa.findPhase(phaseID)
        if phase != nil {
            phase.Body += "\n\n**Constraint (from feedback):** " + action.Payload
            if fa.hotReloader != nil {
                fa.hotReloader.NotifyPhaseModified(phaseID)
            }
        }
        fmt.Fprintf(fa.logger, "[feedback] added constraint to %s\n", phaseID)
        return true

    case feedback.ActionPause:
        fmt.Fprintf(fa.logger, "[feedback] pausing %s\n", phaseID)
        // Pause is handled by the worker loop checking a pause flag.
        return true

    case feedback.ActionSkip:
        fmt.Fprintf(fa.logger, "[feedback] skipping %s\n", phaseID)
        return true

    case feedback.ActionAnnotate:
        fmt.Fprintf(fa.logger, "[feedback] annotation for %s: %s\n", phaseID, action.Payload)
        return true

    case feedback.ActionAddPhase:
        // Delegate to HotReloader's hot-add machinery.
        if fa.hotReloader != nil {
            // action.Payload contains the phase spec as TOML+markdown.
            // Write to a temp file in the nebula dir for HotReloader to pick up.
            fa.hotAddFromPayload(ctx, action.Payload)
        }
        return true

    default:
        fmt.Fprintf(fa.logger, "[feedback] unknown action kind: %s\n", action.Kind)
        return false
    }
}

func (fa *FeedbackApplier) findPhase(id string) *PhaseSpec {
    for _, p := range fa.nebula.Phases {
        if p.ID == id {
            return p
        }
    }
    return nil
}
```

### Worker loop integration

In `WorkerGroup.Run`, after each phase runner cycle returns, check the feedback queue:

```go
// In the worker goroutine, around the RunExistingPhase call:
// The PhaseRunner internally runs coder-reviewer cycles. Between phases
// (not between cycles within a phase), we check feedback.
if wg.feedbackApplier != nil {
    if skip := wg.feedbackApplier.DrainAndApply(ctx, phaseID); skip {
        // Mark phase as skipped and continue to next.
        result := &WorkerResult{PhaseID: phaseID, BeadID: beadID}
        continue
    }
}
```

For feedback between _cycles_ (within a single phase), the `loop.Loop` needs a hook. Add an optional `BetweenCycles` callback:

```go
// In internal/loop/loop.go Loop struct:
// BetweenCycles is called after each coder-reviewer cycle completes.
// If it returns true, the loop should stop (phase skipped/paused).
BetweenCycles func(ctx context.Context) bool
```

The worker sets this callback to call `DrainAndApply`:

```go
l := &loop.Loop{
    // ... existing fields ...
    BetweenCycles: func(ctx context.Context) bool {
        return wg.feedbackApplier.DrainAndApply(ctx, phaseID)
    },
}
```

## Files

- `internal/nebula/worker_feedback.go` — `FeedbackApplier`, `NewFeedbackApplier`, `DrainAndApply`, per-action handlers
- `internal/nebula/worker_feedback_test.go` — tests for: skip action returns true, refactor modifies phase body, adjust-budget updates phase, unknown action returns false, empty queue returns false, re-enqueue for wrong phase
- `internal/nebula/worker_options.go` — add `WithFeedbackQueue` option
- `internal/nebula/worker.go` — add `feedbackQueue` and `feedbackApplier` fields, integrate `DrainAndApply` between phases
- `internal/loop/loop.go` — add optional `BetweenCycles` callback, call it between coder-reviewer iterations

## Acceptance Criteria

- [ ] Workers poll the feedback queue between phases (non-blocking)
- [ ] Workers poll the feedback queue between coder-reviewer cycles via `BetweenCycles`
- [ ] `ActionSkip` causes the current phase to be skipped
- [ ] `ActionRefactorPhase` modifies the phase body and triggers hot-reload
- [ ] `ActionAddConstraint` appends to the phase body
- [ ] `ActionAdjustBudget` updates the phase's max budget
- [ ] `ActionAdjustPriority` updates the phase's priority
- [ ] `ActionAddPhase` delegates to HotReloader's hot-add
- [ ] Actions targeting a different phase are re-enqueued (not consumed)
- [ ] Nil feedback queue is safely ignored (no crash, no poll)
- [ ] Existing `WorkerGroup.Run` semantics are unchanged when no queue is provided
- [ ] `go test ./internal/nebula/...` passes
- [ ] `go test ./internal/loop/...` passes
- [ ] `go vet ./...` passes
