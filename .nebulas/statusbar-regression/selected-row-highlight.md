+++
id = "selected-row-highlight"
title = "Change selected row highlight from invisible black to a visible colored background"
type = "bug"
priority = 2
depends_on = []
+++

## Problem

The selected/focused row in the phase list has no background highlight — `styleRowSelected` in `styles.go:102` only sets `Foreground(colorBrightWhite).Bold(true)`. On the dark terminal background the selection is barely distinguishable from unselected rows, appearing effectively "black" with just a bold white font difference.

The left-edge selection indicator (`styleSelectionIndicator`, `styles.go:136`) uses `colorPrimary` (blue) which helps, but the row itself has no colored background to make the current level visually impactful.

## Solution

Add a `Background()` to `styleRowSelected` using a subtle but visible color. Good candidates:

- `colorNebulaDeep` (`#8B5CF6`) — deep purple, consistent with the nebula theme
- `colorSurfaceBright` (`#161B22`) — very subtle dark highlight
- A new dim surface color (e.g., `#2D2D5E`) that's clearly tinted but not overpowering

The background should be applied to the entire selected row (indicator + phase ID + detail text), not just the phase ID. This may require adjusting `renderPhaseRow` in `nebulaview.go:197` to apply the background style to the full row string.

Also check `loopview.go` for the equivalent loop mode selection style, ensuring consistency.

## Files

- `internal/tui/styles.go` — `styleRowSelected` definition (line 102)
- `internal/tui/nebulaview.go` — `renderPhaseRow` (line 197)
- `internal/tui/loopview.go` — equivalent row rendering for loop mode

## Acceptance Criteria

- [ ] Selected row has a visible, non-black background highlight
- [ ] Background color is consistent with the overall color theme
- [ ] Highlight applies across the full row width, not just the text
- [ ] Both nebula and loop mode selections are styled consistently
- [ ] `go build` and `go test ./internal/tui/...` pass