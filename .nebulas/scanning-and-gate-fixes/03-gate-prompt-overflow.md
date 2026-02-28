+++
id = "gate-prompt-overflow"
title = "Fix gate review prompt overflowing the right edge of the terminal"
type = "bug"
priority = 1
depends_on = []
labels = ["quasar", "tui"]
scope = ["internal/tui/gateprompt.go"]
+++

## Problem

When a running phase completes and triggers a gate review prompt, the overlay can spill past the right edge of the terminal. This happens because:

1. **`styleGateOverlay`** has `Border(lipgloss.DoubleBorder())` + `Padding(1, 2)` but no `MaxWidth` constraint. The overlay grows as wide as its widest content line.

2. **File paths are unconstrained** — in `detailBody()`, file change lines are rendered as:
   ```go
   fmt.Sprintf("  %s %s%s\n", icon, fc.Path, lineInfo)
   ```
   Long file paths (e.g., `internal/nebula/worker_fabric_integration_test.go +42 -18`) push the overlay past the terminal width.

3. **Width accounting mismatch** — `Gate.Width` is set to `m.Width` (full terminal width) at line 461 of `model.go`, but the gate is rendered inside the `middle` section which has `contentWidth` (potentially reduced by the side panel). The review summary wrapping uses `maxWidth = g.Width - 8` which may exceed the available content area.

4. **The overlay has no outer width clamp** — Even though `wrapText` handles the review summary, the `styleGateOverlay.Render()` call in `View()` doesn't constrain the rendered output to fit within the terminal.

## Solution

Constrain the gate overlay to fit within the available content width.

### Changes

1. **`internal/tui/gateprompt.go`** — In `View()`, apply a `Width` constraint to `styleGateOverlay`:
   ```go
   maxWidth := g.Width
   if maxWidth > 0 {
       return styleGateOverlay.Width(maxWidth - 4).Render(out.String())
       // -4 accounts for the double border (2 chars each side)
   }
   return styleGateOverlay.Render(out.String())
   ```

2. **`internal/tui/gateprompt.go`** — In `detailBody()`, truncate file paths that exceed the available width:
   ```go
   maxPathWidth := g.Width - 12 // border + padding + icon + spacing
   path := fc.Path
   if len(path) > maxPathWidth {
       path = "..." + path[len(path)-maxPathWidth+3:]
   }
   ```

3. **`internal/tui/model.go`** — Set `Gate.Width` to `m.contentWidth()` instead of `m.Width` so the overlay respects the side panel (if active):
   ```go
   m.Gate.Width = m.contentWidth()
   ```
   Note: After the `cockpit-ui-polish` nebula removes the side panel, `contentWidth() == m.Width`, but this is still the correct field to use for forward-compatibility.

### Width Budget

For a 120-col terminal (no side panel):
- Double border: 2 + 2 = 4 chars
- Padding: 2 + 2 = 4 chars
- Available content inside overlay: 120 - 8 = 112 chars
- File paths should be capped at ~104 chars (leaving room for icon + line info)

## Files

- `internal/tui/gateprompt.go` — Add width constraint to overlay rendering, truncate long file paths
- `internal/tui/model.go` — Use `contentWidth()` for `Gate.Width` instead of `m.Width`

## Acceptance Criteria

- [ ] Gate review prompt does not extend past the right edge of the terminal at any width >= MinWidth
- [ ] Long file paths are truncated with leading `...` rather than overflowing
- [ ] Review summary text wraps correctly within the overlay bounds
- [ ] Option bar ([a]ccept, [x] reject, etc.) fits within the overlay
- [ ] Gate prompt remains readable at narrow widths (60-80 cols)
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] All existing tests pass (`go test ./internal/tui/...`)
