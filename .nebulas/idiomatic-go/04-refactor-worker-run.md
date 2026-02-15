+++
id = "refactor-worker-run"
title = "Break WorkerGroup.Run into smaller functions"
type = "task"
priority = 2
depends_on = []
+++

## Problem

`WorkerGroup.Run` in `internal/nebula/worker.go` is ~160 lines. It mixes graph initialization, state recovery, eligibility filtering, goroutine dispatch, and result collection. The nested goroutine closure is ~60 lines on its own.

## Solution

Extract focused helpers:

- `initTaskState() (tasksByID, done, failed)` — build maps from existing state
- `filterEligible(ready, inFlight, failed, graph) []string` — eligibility logic
- `executeTask(ctx, taskID, tasksByID, ...)` — the goroutine body as a named method
- Keep `Run` as the dispatch loop calling these helpers

## Files to Modify

- `internal/nebula/worker.go` — Extract methods, slim `Run` to ~40 lines

## Acceptance Criteria

- [ ] `Run` is under 50 lines and reads as a clear dispatch loop
- [ ] Each extracted helper is under 25 lines
- [ ] No behavior changes — same dependency ordering, same concurrency model
- [ ] `go test ./internal/nebula/...` passes
