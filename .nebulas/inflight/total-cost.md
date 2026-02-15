+++
id = "total-cost"
title = "Track and display cumulative cost across all nebula tasks"
type = "task"
priority = 2
depends_on = ["cycle-output"]
+++

## Problem

When running a multi-task nebula, there's no visibility into total dollars spent across all tasks. Each task tracks its own `TotalCostUSD` in `TaskResult` but there's no aggregation.

## Solution

Accumulate cost from each `TaskResult.TotalCostUSD` across tasks in the nebula runner, and display a running total in the cycle summary output.

## Files to Modify

- `internal/loop/loop.go` — `TaskResult` already has `TotalCostUSD`; no changes needed here
- `internal/ui/printer.go` — Add `NebulaProgress(completedTasks int, totalTasks int, totalCostUSD float64)` method
- Nebula runner (caller of `RunTask`) — Accumulate `TaskResult.TotalCostUSD` and call `NebulaProgress` after each task

## Acceptance Criteria

- [ ] After each nebula task completes, print running cost total to stderr
- [ ] Format: `[nebula] 3/7 tasks complete | $2.34 spent`
- [ ] Cost persisted in `nebula.state.toml` for resume scenarios
- [ ] Add test verifying cost accumulation across multiple TaskResults
