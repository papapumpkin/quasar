+++
id = "splash-screen"
title = "XL Full Bleed splash screen on TUI launch"
type = "feature"
priority = 2
depends_on = ["adaptive-layout"]
scope = ["internal/tui/model.go", "internal/tui/msg.go"]
+++

## Problem

When the TUI launches, it immediately shows the status bar and empty cycle timeline. There's no moment of visual impact. We want to show the XL · Full Bleed quasar art as a full-screen splash for ~1.5 seconds while the first agent starts up.

## XL · Full Bleed Art (90 cols × 27 rows)

```
                      .·::··::··::·.                    .·::··::··::·.
                  .::··::::::::::::··::.            .::··::::::::::::··::.
              .::··::::.    ··::||::··  ··::    ::··  ··::||::··    .::::··::.
           .::··:::.   ..··::::\||/::··    ··::··    ··::\||/::::··..   .:::··::.
        .::··:::.  ..··::::..   \||/  ..::··  ··::..  \||/   ..::::··..  .:::··::.
      .::··::. ..··::::..  ··::. \|/ .::··::····::··::. \|/ .::··  ..::::··.. .::··::.
    .::··::. .··::::. .··::.. ·:. | .:·::··:.  .::··::·. | .:· ..::··. .::::··. .::··::.
   .::·::. .··:::.  ··::.. ··::. | .::··  ::··::  ··::. | .::·· ..::··  .:::··. .::·::.
  .::·::. .··:::.  ··::. .··::.. | ..::··. .::··. .::··.. | ..::··. .::··  .:::··. .::·::.
  ::·::. .··:::.  ··::..··::. .  | . .::··:..::··:..::··. | . .::··..::··  .:::··. .::·::
  :·::..··:::.  ··::..··::..··:. | .::··..::····::..::··. | .:·..::··..::··  .:::··..::·:
  :·::.··:::. ··::..··::. ··::.. | ..::··. .:·::·:. .::··.. | ..::·· .::··..::·· .:::··.::·:
  :·:.··:::. ··::..··::..··::..---@---..::··..::::..::··..---@---..::··..::··..::·· .:::··.:·:
  :·::.··:::. ··::..··::. ··::.. | ..::··. .:·::·:. .::··.. | ..::·· .::··..::·· .:::··.::·:
  :·::..··:::.  ··::..··::..··:. | .::··..::····::..::··. | .:·..::··..::··  .:::··..::·:
  ::·::. .··:::.  ··::..··::. .  | . .::··:..::··:..::··. | . .::··..::··  .:::··. .::·::
  .::·::. .··:::.  ··::. .··::.. | ..::··. .::··. .::··.. | ..::··. .::··  .:::··. .::·::.
   .::·::. .··:::.  ··::.. ··::. | .::··  ::··::  ··::. | .::·· ..::··  .:::··. .::·::.
    .::··::. .··::::. .··::.. ·:. | .:·::··:.  .::··::·. | .:· ..::··. .::::··. .::··::.
      .::··::. ..··::::..  ··::. /|\ .::··::····::··::. /|\ .::··  ..::::··.. .::··::.
        .::··:::.  ..··::::..   /||\  ..::··  ··::..  /||\   ..::::··..  .:::··::.
           .::··:::.   ..··::::/||\ ::··    ··::··    ··::/||\::::··..   .:::··::.
              .::··::::.    ··::||::··  ··::    ::··  ··::||::··    .::::··::.
                  .::··::::::::::::··::.            .::··::::::::::::··::.
                      .·::··::··::·.                    .·::··::··::·.

                                   Q    U    A    S    A    R
```

## Solution

### Splash state on AppModel

Add a `Splash bool` field to `AppModel`. Set `true` in `NewAppModel()`.

In `Init()`, return a `tea.Tick(1500ms, ...)` that sends a `MsgSplashDone{}` message.

### Splash rendering in View()

When `m.Splash` is true, `View()` short-circuits to render ONLY the splash:

```go
if m.Splash {
    splash := m.Banner.SplashView()
    // Center both horizontally and vertically within m.Width × m.Height.
    return lipgloss.Place(m.Width, m.Height, lipgloss.Center, lipgloss.Center, splash)
}
```

No status bar, no footer, no main view — just the full XL quasar centered on a dark background.

### Transition

On `MsgSplashDone`:
```go
case MsgSplashDone:
    m.Splash = false
    return m, nil // normal View() kicks in on next render
```

### New message type

Add to `msg.go`:
```go
// MsgSplashDone signals that the splash screen timer has elapsed.
type MsgSplashDone struct{}
```

### Edge cases

- If the terminal is too narrow for XL (< 92 cols), fall back to whichever banner size fits, centered
- If the first `MsgAgentStarted` arrives before the splash timer, keep showing splash until the timer fires (don't cut it short — it's only 1.5s)
- Key press during splash: ignore navigation keys, but `q` should still quit

## Files to Modify

- `internal/tui/model.go` — Add `Splash` field, splash rendering in `View()`, handle `MsgSplashDone`, init tick
- `internal/tui/msg.go` — Add `MsgSplashDone`

## Acceptance Criteria

- [ ] TUI starts with XL quasar art centered on screen for ~1.5s
- [ ] Art has full Doppler coloring (red left, blue right, gold cores)
- [ ] After 1.5s, transitions to normal layout (status bar + content + footer)
- [ ] Falls back to smaller art if terminal is too narrow for XL
- [ ] `q` still quits during splash
- [ ] Other keys ignored during splash
- [ ] `go build` passes
- [ ] `go test ./internal/tui/...` passes
