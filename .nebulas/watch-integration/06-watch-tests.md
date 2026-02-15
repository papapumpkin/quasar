+++
id = "watch-tests"
title = "Add tests for watch integration"
type = "task"
priority = 2
depends_on = ["handle-modified-tasks", "handle-added-tasks", "handle-removed-tasks"]
+++

## Problem

The watch integration adds dynamic behavior to the worker loop that needs test coverage. Without tests, regressions in task reloading, addition, and removal could go undetected.

## Solution

Add test cases to `internal/nebula/nebula_test.go` (or a new `internal/nebula/watch_test.go` if the file is already large) covering:

### Test cases

1. **TestWorkerGroup_WatchModifyPending** — Modify a pending task's description mid-run, verify the updated description is used when the task executes.
2. **TestWorkerGroup_WatchModifyInFlight** — Modify an in-flight task, verify it completes with the original description (not the updated one).
3. **TestWorkerGroup_WatchAddTask** — Add a new task file during execution, verify it gets picked up and executed.
4. **TestWorkerGroup_WatchAddWithDeps** — Add a task that depends on an already-completed task, verify it executes immediately.
5. **TestWorkerGroup_WatchRemovePending** — Remove a pending task's file, verify it's skipped.
6. **TestWorkerGroup_WatchRemoveInFlight** — Remove an in-flight task's file, verify it completes normally.
7. **TestWorkerGroup_WatchNilWatcher** — Verify `Run` works identically when `Watcher` is nil.

### Test approach

Use a mock watcher: create a `Change` channel directly and feed events into it. Don't depend on real filesystem watching in unit tests.

## Files to Create/Modify

- `internal/nebula/watch_test.go` — New test file for watch integration tests

## Acceptance Criteria

- [ ] All 7 test cases pass
- [ ] Tests use `t.Parallel()` where safe
- [ ] No real filesystem watching — mock the `Changes` channel
- [ ] Tests pass under `-race`
- [ ] `go test ./internal/nebula/...` completes within 10 seconds
