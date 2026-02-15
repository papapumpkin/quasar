+++
id = "refactor-runloop"
title = "Break runLoop into small, focused helper methods (~20 lines each)"
type = "task"
priority = 2
depends_on = ["extract-ui-interface", "context-in-beads"]
+++

## Problem

`Loop.runLoop` in `internal/loop/loop.go` is ~180 lines. It interleaves coder invocation, reviewer invocation, budget checking, bead management, finding parsing, and child bead creation. This makes it hard to read, test, and extend.

## Solution

Extract each phase into a focused method on `*Loop`:

- `runCoderPhase(ctx, state, perAgentBudget) error` — build agent, invoke, record cost, add comment
- `runReviewerPhase(ctx, state, perAgentBudget) error` — build agent, invoke, record cost, add comment
- `checkBudget(state, beadID) error` — returns `ErrBudgetExceeded` or nil
- `handleApproval(state, beadID) (*TaskResult, error)` — parse report, close bead, return result
- `createFindingBeads(state, beadID) []string` — create child beads for each finding

Keep `runLoop` as a thin orchestrator that calls these helpers in sequence.

## Files to Modify

- `internal/loop/loop.go` — Extract helper methods, slim `runLoop` to ~30 lines

## Acceptance Criteria

- [ ] `runLoop` is under 40 lines and reads as a clear sequence of phases
- [ ] Each extracted method is under 25 lines
- [ ] No behavior changes — same coder/reviewer loop, same bead comments, same error returns
- [ ] `go test ./internal/loop/...` passes
