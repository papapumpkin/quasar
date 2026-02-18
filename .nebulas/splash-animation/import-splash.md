+++
id = "import-splash"
title = "Import and adapt the splash animation into internal/tui/"
type = "feature"
priority = 1
depends_on = []
+++

## Problem

A binary-star ASCII animation with Doppler color shifting exists as standalone code in `~/Downloads/files(2)/splash.go`. It needs to be adapted and integrated into the quasar codebase as a reusable Bubble Tea component.

## Source Code Reference

The animation (`~/Downloads/files(2)/splash.go`) implements:
- `SplashConfig` struct with width, height, orbit params, FPS, spin count, loop mode
- `DefaultSplashConfig()` — full splash: 62x19, 2 spins, settles to rest
- `SpinnerConfig()` — compact 36x11, loops forever for loading states
- `Splash` model implementing `tea.Model` with `Init()`, `Update()`, `View()`, `Done()`
- Doppler color shift: 9 precomputed color ramps from blueshift to redshift
- Nebula density rendering, diffraction spikes, dual star cores
- Ease-out deceleration curve for the splash mode

## Solution

1. Create `internal/tui/splash.go` — adapt the animation code:
   - Change package to `tui`
   - Rename exported types if they conflict (e.g., `SplashModel` to avoid ambiguity)
   - Keep the Doppler color ramps, density rendering, and spike stamping logic
   - Export `NewSplash(cfg SplashConfig) SplashModel` and `SplashModel.Done() bool`
   - Ensure it integrates cleanly as a Bubble Tea sub-model

2. Create `internal/tui/splash_test.go` — basic tests:
   - Config defaults are sane
   - Model produces non-empty view output
   - Done() returns false initially, true after enough ticks or keypress

## Files

- `internal/tui/splash.go` — new file, adapted from `~/Downloads/files(2)/splash.go`
- `internal/tui/splash_test.go` — new file

## Acceptance Criteria

- [ ] `SplashModel` is a valid `tea.Model`
- [ ] `DefaultSplashConfig()` produces the full 62x19 animation
- [ ] `SpinnerConfig()` produces a compact looping animation
- [ ] Doppler color shifting works (stars change color as they orbit)
- [ ] `Done()` transitions correctly in both splash and spinner modes
- [ ] `go build` and `go test ./internal/tui/...` pass
