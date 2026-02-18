+++
id = "header-consistency"
title = "Unify purple color and spacing in the header bar"
type = "task"
priority = 2
depends_on = []
+++

## Problem

The purple/nebula color is not consistent across all parts of the header bar. Some segments render in the expected purple while others fall back to a different shade or style, creating a patchy appearance.

Additionally, there is insufficient vertical spacing between the header bar and the content area below it, making the UI feel cramped.

## Solution

1. **Color consistency**: Audit `internal/tui/statusbar.go` and `internal/tui/styles.go`. Ensure every segment of the header bar (logo, title, status indicators, timer) uses the same background and foreground color values. Look for places where a style is constructed inline instead of using the shared style constants.

2. **Spacing**: Add a blank line or padding between the status bar output and the phase tree / content area. This can be done in the layout assembly in `internal/tui/layout.go` or wherever the status bar and content views are concatenated.

## Files

- `internal/tui/statusbar.go` — header bar rendering
- `internal/tui/styles.go` — color constants and style definitions
- `internal/tui/layout.go` — view composition and spacing

## Acceptance Criteria

- [ ] Purple/nebula color is uniform across the entire header bar
- [ ] Visible spacing between header and content area
- [ ] No regressions in header content or functionality
- [ ] `go build` and `go test ./internal/tui/...` pass
