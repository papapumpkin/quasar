+++
id = "decision-overlay"
title = "Inline human-decision overlay"
type = "feature"
priority = 2
depends_on = ["board-view"]
scope = ["internal/tui/decisionoverlay.go", "internal/tui/decisionoverlay_test.go"]
+++

## Problem

When a worker hits `HUMAN_DECISION_REQUIRED`, the current `GatePrompt` presents an overlay with approve/reject/skip choices. The cockpit mockup envisions a richer decision overlay that:
- Floats above the board columns (not replacing them)
- Shows full task context (phase name, worker ID, cycle count)
- Displays the question/dilemma with multiple-choice options
- Accepts free-text input inline (not just button selection)
- Visually interrupts with a red-bordered box

## Solution

Create a `DecisionOverlay` component that augments the existing `GatePrompt` for the board view context. When a `MsgGatePrompt` or `MsgGateModePrompt` arrives while the board view is active, the overlay renders on top of the board.

Layout (the mockup's `HUMAN DECISION REQUIRED` box):
```
┌─ ⚠  HUMAN DECISION REQUIRED ──────────────────────────┐
│                                                         │
│ task: db-migrations                                     │
│ worker: w-2  cycle: 3/5                                 │
│                                                         │
│ <question text from the gate prompt>                    │
│                                                         │
│   a) <option 1>                                         │
│   b) <option 2>                                         │
│   c) <option 3>                                         │
│                                                         │
│ ▸ _                                                     │
└─────────────────────────────────────────────────────────┘
```

The overlay:
- Uses `lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorDanger)` for the red border
- Header shows `⚠  HUMAN DECISION REQUIRED` in `colorDanger` bold
- Task context in `colorAccent`/`colorNebula`
- Options labeled `a)`, `b)`, `c)` in `colorBlueshift`
- Input line uses a `bubbles/textinput` for free-text entry
- User types a letter to select an option, or types free text and hits Enter
- On submission, sends the response through the existing `Gater.Prompt()` response channel

The overlay is centered horizontally and vertically over the board. The board content behind it is dimmed (render the board first, then overlay a semi-transparent dim by reducing the board's opacity — in practice, just don't render the board behind the overlay area; Lip Gloss `Place` handles this).

When no decision is pending, the overlay is nil and the board renders normally.

The existing `GatePrompt` continues to work for non-board views (loop mode, table mode). The `DecisionOverlay` is board-specific.

## Files

- `internal/tui/decisionoverlay.go` — `DecisionOverlay` component with text input, option rendering, and response submission
- `internal/tui/decisionoverlay_test.go` — Tests for overlay rendering, option selection, and text input handling

## Acceptance Criteria

- [ ] Overlay renders centered over the board with a red rounded border
- [ ] Shows task name, worker ID, and cycle count from the gate prompt context
- [ ] Displays question text and labeled options (a/b/c)
- [ ] Text input accepts free-text responses via `bubbles/textinput`
- [ ] Selecting an option or pressing Enter on text input sends the response through the existing gater channel
- [ ] Overlay disappears after response is submitted
- [ ] Board content is visible but dimmed behind the overlay
- [ ] `go test ./internal/tui/...` passes
- [ ] `go vet ./...` clean
