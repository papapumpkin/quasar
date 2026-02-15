+++
id = "quasar-state"
title = "Handle failed tasks with retry action instead of skipping"
type = "task"
priority = 2
depends_on = ["cycle-output"]
+++

## Problem

When a nebula task fails (status `failed` in `nebula.state.toml`), the current `BuildPlan` logic in the nebula planner skips it entirely. The developer has to manually reset state to retry. There's no graceful recovery path.

## Solution

In `BuildPlan`, treat `TaskStatusFailed` as retriable: set the action to `ActionRetry` instead of `ActionSkip`. This means re-running `nebula apply` will automatically retry failed tasks rather than ignoring them.

## Files to Modify

- Nebula planner (where `BuildPlan` is defined) — Change the `TaskStatusFailed` case to produce `ActionRetry`
- `nebula.state.toml` handling — Ensure retry resets the task status to `pending` before re-execution
- `internal/loop/loop.go` — `RunExistingTask` already supports resuming an existing bead; ensure it handles retry bead IDs

## Acceptance Criteria

- [ ] Failed tasks in `nebula.state.toml` are retried on next `nebula apply`
- [ ] Retry creates a new bead (not reusing the failed bead)
- [ ] State file updated to reflect retry status transitions
- [ ] `nebula validate` shows failed tasks as "will retry" in plan output
- [ ] Add test for `BuildPlan` producing `ActionRetry` on failed tasks
