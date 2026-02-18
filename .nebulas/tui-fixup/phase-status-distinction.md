+++
id = "phase-status-distinction"
title = "Better visual distinction between phases and their status bars"
type = "task"
priority = 3
depends_on = ["header-consistency"]
+++

## Problem

Phase rows and their associated status/progress bars blend together visually, making it hard to quickly scan which phase is which and what state each is in. The phase name and the status indicator lack enough visual separation.

## Solution

Add subtle visual cues to distinguish phase rows from their status information:

1. **Indentation or grouping**: Ensure the status bar for each phase is visually subordinate to the phase name — either slightly indented, rendered in a muted style, or separated by a thin rule.

2. **Typography**: Use bolder or brighter text for the phase name and a lighter/muted style for status details (cycle count, timing, etc.).

3. Keep changes minimal — do not redesign the tree structure. Just enough to make scanning easier.

## Files

- `internal/tui/nebulaview.go` — phase row rendering
- `internal/tui/styles.go` — style definitions
- `internal/tui/loopview.go` — loop mode phase rendering (if applicable)

## Acceptance Criteria

- [ ] Phase names are visually distinct from status information at a glance
- [ ] Changes are subtle — no major layout redesign
- [ ] `go build` and `go test ./internal/tui/...` pass
