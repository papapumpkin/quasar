+++
id = "stats-bar-upgrade"
title = "Enhanced stats bar with progress blocks"
type = "feature"
priority = 2
depends_on = []
scope = ["internal/tui/statusbar.go"]
+++

## Problem

The current `StatusBar` shows elapsed time, phase progress as a fraction (`3/8`), total cost, and CPU/memory usage. The cockpit mockup envisions a richer bottom-pinned stats bar with:
- A visual progress bar using filled/empty block characters
- Token count alongside cost
- More prominent wall-clock elapsed time

The existing status bar is at the *top*. The cockpit places aggregate stats at the *bottom* alongside keybinding hints (which the existing footer already handles).

## Solution

Enhance the existing `StatusBar` rather than replacing it. Add a `BottomBar` rendering method that produces the cockpit-style stats line, pinned to the bottom of the terminal above the footer keybinds.

The bottom bar renders a single line:
```
 tokens 284.3k | cost $1.42 | elapsed 4m 32s | progress █████░░░ 5/8
```

**Progress bar**: Use `█` (filled) and `░` (empty) characters. Bar width scales with terminal width (min 5, max 20 blocks). Fill ratio = `completedPhases / totalPhases`. Color: filled blocks in `colorSuccess`, empty in `colorMuted`.

**Token counter**: New field `TotalTokens int` on `StatusBar`. Populated from `MsgPhaseAgentDone` (which carries cost data). Format as `k` suffix for thousands (e.g., `284.3k`).

**Cost**: Already tracked. Format as `$X.XX`.

**Elapsed**: Already tracked via `MsgTick`. Format as `Xm Xs` or `Xh Xm` for longer runs.

The top status bar remains as-is (logo, phase count, resource indicators). The bottom bar adds the detailed stats the cockpit mockup shows. Both bars use `colorMuted` for labels and `colorWhite` for values.

Keep the existing keybinding footer (`internal/tui/footer.go`) below the bottom bar. The footer already adapts its keybinds to context — extend it with cockpit-specific hints when the board view is active: `[tab] contracts  [d] detail  [r] retry  [enter] respond  [q] quit`.

## Files

- `internal/tui/statusbar.go` — Add `BottomBar()` rendering method, `TotalTokens` field, token formatting

## Acceptance Criteria

- [ ] Bottom bar renders below the main content area, above the footer
- [ ] Progress bar uses block characters with correct fill ratio
- [ ] Token count formatted with `k` suffix and updates on `MsgPhaseAgentDone`
- [ ] Cost and elapsed time match existing StatusBar data
- [ ] Top status bar remains unchanged (logo, resources, phase fraction)
- [ ] Existing tests continue to pass, new tests for bottom bar rendering
- [ ] `go vet ./...` clean
