+++
id = "decision-overlay"
title = "Inline hail overlay for human decisions"
type = "feature"
priority = 2
depends_on = ["board-view"]
scope = ["internal/tui/hailoverlay.go", "internal/tui/hailoverlay_test.go"]
+++

## Problem

When a quasar hits a blocker that requires human judgment, the current `GatePrompt` presents an overlay with approve/reject/skip choices. The cockpit mockup envisions a richer hail overlay that:
- Floats above the board columns (not replacing them)
- Shows full task context (phase name, quasar ID, cycle count)
- Displays the discovery detail with its kind
- Presents multiple-choice options extracted from the discovery
- Accepts free-text input inline (not just button selection)
- Visually interrupts with a red-bordered box marked `HAIL`

## Solution

Create a `HailOverlay` component that augments the existing `GatePrompt` for the board view context. When a `MsgHail` arrives (from the fabric nebula's cockpit-wiring phase) while the board view is active, the overlay renders on top of the board.

Layout (the mockup's `HUMAN DECISION REQUIRED` box, now using canonical terminology):
```
┌─ ⚠  HAIL ──────────────────────────────────────────────┐
│                                                          │
│ phase: db-migrations                                     │
│ quasar: q-2  cycle: 3/5                                  │
│ kind: requirements_ambiguity                             │
│                                                          │
│ <discovery detail text>                                  │
│                                                          │
│   a) <option 1>                                          │
│   b) <option 2>                                          │
│   c) <option 3>                                          │
│                                                          │
│ ▸ _                                                      │
└──────────────────────────────────────────────────────────┘
```

The overlay:
- Uses `lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorDanger)` for the red border
- Header shows `⚠  HAIL` in `colorDanger` bold
- Discovery kind displayed in `colorMuted`
- Task context in `colorAccent`/`colorNebula`
- Options labeled `a)`, `b)`, `c)` in `colorBlueshift`
- Input line uses a `bubbles/textinput` for free-text entry
- User types a letter to select an option, or types free text and hits Enter
- On submission: resolves the discovery on the fabric, unblocks the task, and injects the response as a constraint

The overlay is centered horizontally and vertically over the board. The board content behind it is dimmed.

When no hail is pending, the overlay is nil and the board renders normally.

The existing `GatePrompt` continues to work for non-board views (loop mode, table mode). The `HailOverlay` is board-specific and driven by `MsgHail` messages from the fabric.

## Files

- `internal/tui/hailoverlay.go` — `HailOverlay` component with text input, option rendering, and discovery resolution
- `internal/tui/hailoverlay_test.go` — Tests for overlay rendering, option selection, and text input handling

## Acceptance Criteria

- [ ] Overlay renders centered over the board with a red rounded border
- [ ] Shows phase name, quasar ID, cycle count, and discovery kind from the `MsgHail`
- [ ] Displays discovery detail text and labeled options (a/b/c)
- [ ] Text input accepts free-text responses via `bubbles/textinput`
- [ ] Selecting an option or pressing Enter resolves the discovery and unblocks the task
- [ ] Overlay disappears after response is submitted
- [ ] Board content is visible but dimmed behind the overlay
- [ ] `go test ./internal/tui/...` passes
- [ ] `go vet ./...` clean
