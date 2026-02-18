+++
id = "logo-no-bg"
title = "Remove background color from QUASAR logo text"
type = "bug"
priority = 1
depends_on = ["header-solid-bg"]
+++

## Problem

The `Q U A S A R` logo text on the left of the status bar currently gets highlighted with the dusk purple background color (`colorSurface`). The logo text itself should have **no explicit background** — it should simply inherit whatever background the status bar provides. This way the logo text blends seamlessly into the solid bar rather than appearing as a separately-highlighted block.

## Current State

`internal/tui/logo.go`:
- `Logo()` renders `━━╋━━ QUASAR ━━╋━━` using `styleLogoJet` and `styleLogoCore`
- Both styles currently set `Background(colorSurface)` explicitly (styles.go lines ~7-8)

`internal/tui/styles.go`:
- `styleLogoJet` = `NewStyle().Foreground(colorMutedLight).Background(colorSurface)`
- `styleLogoCore` = `NewStyle().Foreground(colorMutedLight).Background(colorSurface).Bold(true)`

## Solution

Remove the explicit `Background(colorSurface)` from `styleLogoJet` and `styleLogoCore`. The logo will then inherit the background from the parent `styleStatusBar` container, which already sets `Background(colorSurface)`. This eliminates any double-background or mismatched-background artifacts.

The logo foreground colors should stay as they are (or be enhanced in the status-bar-colors phase).

## Files to Modify

- `internal/tui/styles.go` — Remove `.Background(colorSurface)` from `styleLogoJet` and `styleLogoCore`

## Acceptance Criteria

- [ ] The QUASAR logo text has no explicit background style — it inherits from the status bar
- [ ] The logo renders cleanly within the solid status bar without any separate highlight block
- [ ] The logo is still readable with its foreground color against the bar background
- [ ] `go build` and `go vet ./...` pass