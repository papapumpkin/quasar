+++
id = "test-race-fix"
title = "Fix pre-existing data race in mockRunner test mock"
type = "bug"
priority = 2
+++

## Bug

`mockRunner.RunExistingPhase()` at `internal/nebula/nebula_test.go:609` does `m.calls = append(m.calls, beadID)` with no synchronization. When parallel worker goroutines call this concurrently, the race detector flags it. Affects `TestWorkerGroup_WithMetrics_PhaseMetricsPopulated`, `TestWorkerGroup_NilMetrics_NoPanics`, and `TestHistoryRotation`.

## Root Cause

The `mockRunner` struct has no mutex protecting the `calls` slice. Multiple worker goroutines in `WorkerGroup.Run()` call `RunExistingPhase` concurrently, each appending to the shared `calls` slice.

## Fix

1. Add a `sync.Mutex` field (`mu`) to the `mockRunner` struct in `internal/nebula/nebula_test.go`
2. In `RunExistingPhase()`, lock `mu` around `m.calls = append(m.calls, beadID)` (lock, append, unlock — don't hold lock during `resultFunc` call)
3. In test assertions that read `runner.calls` (e.g., `len(runner.calls)`), lock `mu` around reads. Search for all `runner.calls` references in the file and protect them.
4. Add `"sync"` to the import block if not already present

## Files

- `internal/nebula/nebula_test.go` — add mutex to `mockRunner`, lock around `calls` access

## Verification

`go test -race ./internal/nebula/...` — no race warnings on the affected tests.
