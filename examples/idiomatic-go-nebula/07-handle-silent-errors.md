+++
id = "handle-silent-errors"
title = "Replace silent error discards (_ = expr) with explicit logging"
type = "task"
priority = 2
depends_on = ["extract-ui-interface"]
+++

## Problem

`internal/loop/loop.go` has many silent error discards:

```go
_ = l.Beads.AddComment(beadID, ...)
_ = l.Beads.Close(beadID, ...)
```

These hide operational failures. If a bead comment fails to post, the developer has no visibility into what went wrong. Go best practice is to handle every error or at minimum log it.

## Solution

For non-fatal bead operations (comments, status updates) that should not abort the loop, log the error via `l.UI.Error(...)` instead of discarding it. This preserves the non-fatal behavior while giving visibility.

Pattern:
```go
if err := l.Beads.AddComment(beadID, msg); err != nil {
    l.UI.Error(fmt.Sprintf("failed to add bead comment: %v", err))
}
```

## Files to Modify

- `internal/loop/loop.go` — Replace all `_ = l.Beads.*` with error-checked calls that log on failure
- `internal/nebula/worker.go` — Replace `_ = SaveState(...)` with logged error handling

## Acceptance Criteria

- [ ] No `_ = ` error discards remain in `loop.go` or `worker.go`
- [ ] Non-fatal errors are logged via the UI, not silently swallowed
- [ ] Fatal errors still return `fmt.Errorf(...: %w)` as before
- [ ] `go vet ./...` passes
