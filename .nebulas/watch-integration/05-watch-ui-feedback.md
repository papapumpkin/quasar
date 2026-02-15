+++
id = "watch-ui-feedback"
title = "Report watch events via UI printer"
type = "task"
priority = 2
depends_on = ["handle-modified-tasks", "handle-added-tasks", "handle-removed-tasks"]
+++

## Problem

When watch events trigger changes (task reloaded, task added, task skipped), the user has no visibility into what happened.

## Solution

Add an `OnWatchEvent` callback to `WorkerGroup` (similar to `OnProgress`). The callback receives a structured event describing what happened. Wire it up in `cmd/nebula.go` to call a new `Printer` method.

### `WorkerGroup` addition

```go
type WatchEventKind int

const (
    WatchTaskReloaded WatchEventKind = iota
    WatchTaskAdded
    WatchTaskSkipped
)

type WatchEvent struct {
    Kind   WatchEventKind
    TaskID string
    Detail string // human-readable explanation
}

type WatchEventFunc func(event WatchEvent)
```

### UI output

- `[watch] reloaded task "deploy-api" (description updated)`
- `[watch] added new task "cleanup-logs"`
- `[watch] skipped task "old-migration" (file removed)`

All output to stderr via `ui.Printer`.

## Files to Modify

- `internal/nebula/worker.go` — Add `WatchEvent` types and `OnWatchEvent` callback field
- `internal/ui/ui.go` — Add `WatchEvent` printer method
- `cmd/nebula.go` — Wire `OnWatchEvent` to printer

## Acceptance Criteria

- [ ] Watch events are reported to stderr with `[watch]` prefix
- [ ] Each event kind (reloaded, added, skipped) has a distinct message
- [ ] UI output follows project convention (stderr, ANSI colors via `ui.Printer`)
- [ ] `OnWatchEvent` is nil-safe (no crash if not set)
