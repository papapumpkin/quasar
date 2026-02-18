+++
id = "esc-escape-hatch"
title = "Esc should be a universal escape hatch across all TUI views"
type = "bug"
priority = 1
depends_on = ["diff-nav-bugs"]
+++

## Problem

Esc does not consistently work as a back/dismiss action across all TUI overlays and modal views. Users expect Esc to always be an escape hatch — dismissing whatever is currently on screen and returning to the previous state. Several views intercept all key input but silently swallow Esc.

Affected views:

1. **Gate prompt**: `handleGateKey()` intercepts all keys when a gate is active but has no Esc handler. Users are forced to pick an action (a/x/r/s) with no way to defer or dismiss the prompt.

2. **Completion overlay**: The overlay block in `handleKey()` only responds to `q`/`ctrl+c` (quit) and arrow/enter (navigation). Esc is swallowed — users must know to press `q`.

## Root Cause

The key dispatch in `model.go` routes keys to overlay-specific handlers early (before reaching the main switch). Each overlay handler is a closed scope — if it doesn't explicitly handle Esc, the key is consumed and dropped.

The `drillUp()` function (the main Esc handler) is only reached at the end of the dispatch chain, so it never fires when an overlay is active.

## Solution

### Gate prompt (`handleGateKey`)
Add an Esc case that dismisses the gate prompt without selecting an action. Determine the appropriate behavior:
- Option A: Dismiss the gate entirely and let the phase continue without a decision (if the system supports deferred gating).
- Option B: Treat Esc as equivalent to "skip" (`GateActionSkip`), which is the least destructive option.
- Option C: Do nothing but show a hint ("press a/x/r/s to respond") so Esc at least doesn't silently vanish.

Recommendation: Option B (skip) unless the orchestrator requires an explicit decision, in which case Option C.

### Completion overlay
Add an Esc case to the overlay key handler in `handleKey()`:
- If a nebula picker is shown, Esc should dismiss the picker and quit (same as `q`).
- Alternatively, if there's a "back" state to return to, Esc returns there.

## Files

- `internal/tui/model.go` — `handleGateKey()` (~line 1130), completion overlay block (~line 568), `handleKey()` dispatch
- `internal/tui/keys.go` — `Back` keybinding definition (already maps to Esc)

## Acceptance Criteria

- [ ] Esc dismisses or backs out of the gate prompt
- [ ] Esc dismisses the completion overlay
- [ ] No overlay or modal view silently swallows Esc
- [ ] `go build` and `go test ./internal/tui/...` pass