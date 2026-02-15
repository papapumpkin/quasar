+++
id = "progress-bar"
title = "Add inline progress indicator for nebula execution"
type = "task"
priority = 2
depends_on = ["cycle-output"]
+++

## Problem

During long nebula runs, developers have no visual indicator of overall progress or whether the system is spiraling (creating beads faster than closing them).

## Solution

Add a `NebulaProgressBar` method to `internal/ui/printer.go` that uses `\r` carriage return to overwrite a single stderr line showing progress. No external dependencies.

## Files to Modify

- `internal/ui/printer.go` — Add `NebulaProgressBar(completed, total, openBeads, closedBeads int)` method
  - Format: `[██████░░░░] 3/7 tasks | 12 beads open, 8 closed`
  - Use `\r` to overwrite the line (no newline until task transitions)
- Nebula runner — Call `NebulaProgressBar` after each task status change

## Acceptance Criteria

- [ ] Progress bar visible on stderr during `nebula apply`
- [ ] Shows task completion ratio as visual bar
- [ ] Shows bead open/closed counts to detect spiraling
- [ ] Uses `\r` overwrite (no scrolling flood)
- [ ] No external dependencies (stdlib only)
- [ ] Add test for progress bar string formatting
