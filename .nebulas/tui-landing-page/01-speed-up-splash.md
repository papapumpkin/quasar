+++
id = "speed-up-splash"
title = "Speed up the splash loading animation"
type = "task"
priority = 1
+++

## Problem

The splash animation on startup takes too long. It runs for 2.0 full orbit spins at 30 FPS, which means `2.0 * 120 = 240 frames` — roughly 8 seconds of animation before the user can interact with the TUI. This needs to be cut in half.

## Current State

`internal/tui/splash.go`:
- `DefaultSplashConfig()` returns `Spins: 2.0` — the number of orbital revolutions
- `totalFrames` is calculated as `int(cfg.Spins * 120)` in `NewSplash()`
- At 30 FPS, 2 spins = 240 frames = 8 seconds
- The splash is used by both `NewNebulaProgram` and will be used by the new landing page

## Solution

Reduce `Spins` from `2.0` to `1.0` in `DefaultSplashConfig()`. This cuts the animation to ~4 seconds — still enough for the visual effect but much snappier.

```go
func DefaultSplashConfig() SplashConfig {
    return SplashConfig{
        // ... other fields unchanged ...
        Spins:     1.0,  // was 2.0
        // ...
    }
}
```

## Files to Modify

- `internal/tui/splash.go` — Change `Spins: 2.0` to `Spins: 1.0` in `DefaultSplashConfig()`

## Acceptance Criteria

- [ ] `DefaultSplashConfig()` returns `Spins: 1.0`
- [ ] Splash animation completes in ~4 seconds instead of ~8
- [ ] Existing splash tests pass (update expected frame counts if needed)
- [ ] `go build` and `go vet ./...` pass