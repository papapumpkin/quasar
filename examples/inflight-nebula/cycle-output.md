+++
id = "cycle-output"
title = "Print structured cycle summaries to stderr after each coder/reviewer phase"
type = "task"
priority = 1
depends_on = []
+++

## Problem

During `quasar run` execution, developers see minimal output between cycles. They can't tell what the coder did, why the reviewer approved/rejected, or how much budget was consumed.

## Solution

Add a `CycleSummary` method to `internal/ui/printer.go` that prints a structured summary after each coder and reviewer phase. The data is available from `CycleState` and `ReviewReport` in `internal/loop/loop.go`.

## Files to Modify

- `internal/ui/printer.go` — Add `CycleSummary(state CycleState)` method that outputs:
  - Cycle number / max cycles
  - Phase completed (coder or reviewer)
  - Cost for this phase and running total
  - Duration
  - For reviewer: whether approved or number of issues found
- `internal/loop/loop.go` — Call `l.UI.CycleSummary(state)` after each `AgentDone` call (lines ~122 and ~159)

## Acceptance Criteria

- [ ] Each cycle prints a human-readable summary to stderr
- [ ] Summary includes: cycle number, cost, duration, outcome
- [ ] Output uses ANSI colors via existing `Printer` patterns (bold headers, dim details)
- [ ] All output goes to stderr (not stdout)
- [ ] Add test for `CycleSummary` output format
