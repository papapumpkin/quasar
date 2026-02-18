+++
id = "unify-bar-color"
title = "Make the entire status bar one uniform color scheme"
type = "bug"
priority = 1
depends_on = []
+++

## Problem

The status bar is visually patchy because individual segments each use a different foreground color on the same `colorSurface` background:

- **Logo jets** (`styleLogoJet`): orange (`colorAccent`) — `logo.go:6`
- **Logo "QUASAR"** (`styleLogoCore`): blue (`colorPrimary`) — `logo.go:7`
- **Mode label** (`styleStatusMode`): muted light gray (`colorMutedLight`) — `styles.go:50`
- **Name** (`styleStatusName`): bright white + bold — `styles.go:55`
- **Progress** (`styleStatusProgress`): muted gray or green — `styles.go:61,66`
- **Cost** (`styleStatusCost`): orange (`colorAccent`) — `styles.go:70`
- **Elapsed** (`styleStatusElapsed`): muted gray — `styles.go:75`
- **Resources** (`styleResourceNormal/Warning/Danger`): green/orange/red — `styles.go:334-347`
- **Progress bars** (`renderProgressBar`, `renderBudgetBar`, `renderCycleBar`): inline styles with varying foreground colors — `statusbar.go:293-336`

The result is a rainbow of colors across the bar rather than a clean, unified look.

Additionally, the "QUASAR" text in the logo is highlighted in a prominent blue/purple that the user did not ask for — it draws the eye away from the actual task information.

## Solution

1. **Unify the bar foreground to a single muted/dim color** for all text segments. Use `colorWhite` or `colorMutedLight` as the universal foreground on `colorSurface` background. Remove bold from everything except possibly the task name.

2. **Tone down the logo**: Change `styleLogoCore` and `styleLogoJet` to use the same muted foreground as the rest of the bar (e.g. `colorMutedLight`), so the logo blends into the bar rather than standing out. The jets and "QUASAR" text should be the same color.

3. **Remove per-segment color overrides**: Update `styleStatusMode`, `styleStatusCost`, `styleStatusElapsed`, and the resource styles to all use the same foreground color when rendered inside the status bar. The only exception should be actual semantic indicators (the STOPPING/PAUSED labels in red/orange are fine since they are alerts).

4. **Simplify progress/budget/cycle bar colors**: Use a single dim foreground for the filled portion and `colorMuted` for empty, instead of the multi-color gradient logic in `progressColor()` and `budgetColor()`.

## Files

- `internal/tui/logo.go` — `styleLogoJet`, `styleLogoCore` color definitions
- `internal/tui/styles.go` — all `styleStatus*` and `styleResource*` definitions
- `internal/tui/statusbar.go` — inline styles in `renderProgressBar`, `renderBudgetBar`, `renderCycleBar`

## Acceptance Criteria

- [ ] The entire status bar renders with a single uniform foreground color on `colorSurface` background
- [ ] The "QUASAR" logo text is the same color as surrounding bar text (not highlighted purple/blue)
- [ ] Logo jets are the same color as the logo text
- [ ] STOPPING and PAUSED indicators may retain their alert colors as an exception
- [ ] `go build` and `go test ./internal/tui/...` pass
- [ ] No visual regressions in content below the status bar