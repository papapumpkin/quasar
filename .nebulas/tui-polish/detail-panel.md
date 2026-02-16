+++
id = "detail-panel"
title = "Rich detail panel with formatted output and scroll indicators"
type = "feature"
priority = 2
depends_on = ["color-theme"]
+++

## Problem

The detail panel is a plain viewport that dumps raw agent output text. There's no formatting, no scroll position indicator, and no contextual information about the selected item.

## Solution

### Contextual Headers
- When a loop agent row is selected: show role, cycle number, duration, cost, and issue count as a formatted header above the output
- When a nebula phase is selected: show phase ID, title, wave number, dependencies, status, cost, cycles used

### Output Formatting
- Wrap agent output in a styled box with the role as title
- Highlight key patterns in output: `APPROVED` in green, `ISSUE:` blocks in yellow, `SEVERITY: critical` in red
- Truncate very long outputs with a "(truncated — N lines)" indicator

### Scroll Indicators
- Show scroll position: `↑ 12 more` at top, `↓ 34 more` at bottom when content overflows
- Use the viewport's built-in scroll percentage or compute from `YOffset` and content height
- Style the indicators dimly so they don't distract

### Empty State
- When no item is selected or detail is collapsed, show a hint: "Press enter to expand details"
- When an agent is still working, show "Output will appear when the agent completes"

## Files to Modify

- `internal/tui/detailpanel.go` — Contextual headers, output formatting, scroll indicators, empty states
- `internal/tui/model.go` — Pass richer context to detail panel on selection change
- `internal/tui/loopview.go` — Expose selected entry context (role, cycle, cost, etc.)
- `internal/tui/nebulaview.go` — Expose selected phase context

## Acceptance Criteria

- [ ] Detail panel shows contextual header for selected item
- [ ] Agent output has key patterns highlighted (APPROVED, ISSUE, SEVERITY)
- [ ] Scroll indicators show when content overflows
- [ ] Empty states provide helpful hints
- [ ] Long output is truncated with indicator
- [ ] Tests for output formatting helpers
- [ ] `go build` and `go test ./internal/tui/...` pass
