+++
id = "loop-extract-bead-error-helper"
title = "Extract repeated bead error-logging pattern into a helper"
type = "task"
priority = 2
depends_on = ["loop-extract-prompt-builders"]
scope = ["internal/loop/loop.go"]
+++

## Problem

`internal/loop/loop.go` repeats the same error-logging pattern for non-fatal bead operations at least 6 times:

```go
if err := l.Beads.AddComment(ctx, state.TaskBeadID, msg); err != nil {
    l.UI.Error(fmt.Sprintf("failed to add bead comment: %v", err))
}
```

```go
if err := l.Beads.Update(ctx, state.TaskBeadID, beads.UpdateOpts{Status: "in_progress"}); err != nil {
    l.UI.Error(fmt.Sprintf("failed to update bead: %v", err))
}
```

Each instance follows the same pattern: call a bead method, check error, log via `l.UI.Error`. The boilerplate obscures the actual loop logic.

## Solution

Add helper methods on `Loop` for the common bead operations:

```go
// beadComment logs a comment on the task bead, logging any error.
func (l *Loop) beadComment(ctx context.Context, beadID, body string) {
    if err := l.Beads.AddComment(ctx, beadID, body); err != nil {
        l.UI.Error(fmt.Sprintf("failed to add bead comment: %v", err))
    }
}

// beadUpdate updates the task bead status, logging any error.
func (l *Loop) beadUpdate(ctx context.Context, beadID string, opts beads.UpdateOpts) {
    if err := l.Beads.Update(ctx, beadID, opts); err != nil {
        l.UI.Error(fmt.Sprintf("failed to update bead: %v", err))
    }
}
```

Then replace all 6+ call sites with `l.beadComment(ctx, state.TaskBeadID, msg)` and `l.beadUpdate(ctx, state.TaskBeadID, opts)`.

## Files

- `internal/loop/loop.go` — add helpers, replace all inline bead error patterns

## Acceptance Criteria

- [ ] No inline bead error-check-and-log patterns remain in `loop.go`
- [ ] Helper methods are clean, small, and well-documented
- [ ] `go test ./internal/loop/...` passes
