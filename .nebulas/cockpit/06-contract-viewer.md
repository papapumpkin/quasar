+++
id = "entanglement-viewer"
title = "Entanglements tab view"
type = "feature"
priority = 2
depends_on = ["tab-system"]
scope = ["internal/tui/entanglementview.go", "internal/tui/entanglementview_test.go"]
+++

## Problem

When multiple phases work in parallel, they share entanglements — interface agreements (function signatures, struct definitions) that one task produces and another consumes. The cockpit mockup shows an "Entanglements" tab that displays these agreements from the fabric, so the operator can see at a glance what's been agreed upon, what's pending, and whether any are disputed.

Currently there is no TUI representation of cross-phase entanglements.

## Solution

Create an `EntanglementView` component that renders in the `TabEntanglements` tab. It displays a scrollable list of entanglement cards, each showing:

1. **Entanglement ID** as the card title in `colorBlueshift`
2. **Parties**: producer `→` consumer (or `→ *` if consumer is NULL/any downstream) in `colorAccent`
3. **Status**: `pending` (yellow), `fulfilled` (green), or `disputed` (red)
4. **Interface body**: the Go type/function signature rendered as monospace code

Each entanglement is rendered in a bordered box using `lipgloss.RoundedBorder()` with `colorMuted` borders.

```go
type EntanglementView struct {
    entanglements []fabric.Entanglement
    cursor        int
    viewport      viewport.Model
    width         int
    height        int
}
```

**Data source**: Populated from `MsgEntanglementUpdate` messages (defined in the fabric nebula's cockpit-wiring phase). The fabric periodically emits the full entanglement list, and this view re-renders on each update. The view is a pure consumer — it never writes entanglement data.

**Navigation**: Arrow keys scroll through entanglements. The viewport supports scrolling for long interface definitions. `Esc` returns to the tab bar.

**Grouping**: Entanglements are grouped by producer phase, with a section header for each. Within each group, sorted by status (disputed first, then pending, then fulfilled).

**Future enhancement** (not in this phase): Inline editing of entanglements directly in the TUI, with changes propagated to quasars at their next checkpoint. For now, the view is read-only.

## Files

- `internal/tui/entanglementview.go` — `EntanglementView` component with entanglement cards and scrollable viewport
- `internal/tui/entanglementview_test.go` — Tests for entanglement rendering, status styling, grouping, and cursor navigation

## Acceptance Criteria

- [ ] Entanglement cards render with ID, producer→consumer, status, and interface body
- [ ] Status colors match: `pending` = yellow, `fulfilled` = green, `disputed` = red
- [ ] Interface body renders as monospace code text
- [ ] Entanglements grouped by producer, sorted by status within groups
- [ ] Cursor navigation scrolls through entanglements
- [ ] View integrates with the tab system (appears on `TabEntanglements`)
- [ ] Consumes `MsgEntanglementUpdate` messages from the fabric bridge
- [ ] View handles zero entanglements gracefully (shows "No entanglements" placeholder)
- [ ] `go test ./internal/tui/...` passes
- [ ] `go vet ./...` clean
