+++
id = "banner-art"
title = "Banner component with all ASCII art variants and Doppler coloring"
type = "feature"
priority = 1
scope = ["internal/tui/banner.go", "internal/tui/styles.go"]
+++

## Problem

The TUI has no visual identity beyond a single-line inline logo (`━━╋━━ QUASAR ━━╋━━`). We need a `Banner` component that holds all four size variants of the quasar ASCII art and applies red/blue Doppler shift coloring via lipgloss.

## ASCII Art — All Variants

### XS-A · Pill (34 cols × 6 rows) — narrow terminals

```
       .·::··::···::··::·.
    .::\|/::··::··::\|/::·:.
   ::---@---::····::---@---::
    .::/|\ ::··::··::/|\::·:.
       .·::··::···::··::·.
       ···· Q U A S A R ····
```

### S-A · Wide Ellipse (52 cols × 11 rows) — medium terminals, top banner

```
            ..·::··::····::··::·..
        .::··::··:.\|/.··./|\.::··::··::.
     .::··::··::·.·\|/.··./|\·.::··::··::.
   .::··::··::·.·---@---··---@---·.::··::··::.
  ::··::··::··::.·./|\.··.\|\.·.::··::··::··::
   .::··::··::·::··/|\ ··::\|\ ··::·::··::··::.
     .::··::··::·.·.|.·.··.|.·.::··::··::.
        .::··::··::··::····::··::··::··::.
            ..·::··::····::··::·..

                Q    U    A    S    A    R
```

### S-B · Tall Ellipse (44 cols × 19 rows) — wide terminals, left side panel

```
           ..·::····::·..
        .::··:::\|/:::··::.
      .::··::.··\|/··.::··::.
    .::··::. ·::\|/::· .::··::.
   ::··::. ··::.·|·.::·· .::··::
  ::··::. ··::---@---::·· .::··::
   ::··::. ··::.·|·.::·· .::··::
    .::··::. ·::/|\::· .::··::.
      .::··::..·/|\··..::··::.
    .::··::. ·::\|/::· .::··::.
   ::··::. ··::.·|·.::·· .::··::
  ::··::. ··::---@---::·· .::··::
   ::··::. ··::.·|·.::·· .::··::
    .::··::. ·::/|\::· .::··::.
      .::··::..·/|\··..::··::.
        .::··:::/|\:::··::.
           ..·::····::·..

          Q    U    A    S    A    R
```

### XL · Full Bleed (90 cols × 27 rows) — splash screen and README

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

### New file: `internal/tui/banner.go`

```go
// BannerSize represents the size variant of the quasar ASCII art.
type BannerSize int

const (
    BannerNone BannerSize = iota // too narrow, hide entirely
    BannerXS                     // XS-A Pill: 34×6
    BannerS                      // S-A Wide Ellipse: 52×11
    BannerSB                     // S-B Tall Ellipse: 44×19 (side panel)
    BannerXL                     // XL Full Bleed: 90×27 (splash only)
)
```

Store each art variant as a `[]string` (one string per line). Each line is the raw ASCII — no ANSI codes baked in.

#### Doppler coloring function

For each line of art, split at the visual center column. Apply lipgloss styles:

- **Left half**: red/magenta gradient — use `colorDanger` (#FF7B72) for the outer edge fading to a deeper red/magenta toward center
- **Right half**: blue/cyan gradient — use `colorPrimary` (#58A6FF) for the outer edge fading to `colorBlue` (#79C0FF) toward center
- **Core** (`@` characters): bright gold using `colorStarYellow` (#E3B341) or `colorAccent` (#FFA657)
- **`Q U A S A R` text**: use existing logo styles (`styleLogoJet` + `styleLogoCore`)
- **Dim particles** (`.`, `·`, `:`): use `colorMuted` for the outermost dots to create a fade-to-black effect at the edges

Add a few new color constants to `styles.go` if needed:
```go
colorRedshift  = lipgloss.Color("#FF6B6B") // warm red for left jet
colorBlueshift = lipgloss.Color("#4FC3F7") // cool cyan for right jet
```

#### Banner struct and methods

```go
type Banner struct {
    Width  int
    Height int
}

// Size returns the appropriate BannerSize for the current dimensions.
func (b Banner) Size() BannerSize

// View returns the styled ASCII art, centered horizontally. Returns "" if size is BannerNone.
func (b Banner) View() string

// SidePanelWidth returns the width of the side panel column (including padding), or 0 if no side panel.
func (b Banner) SidePanelWidth() int

// SidePanelView returns the S-B art rendered for a side panel, vertically centered within the given height.
func (b Banner) SidePanelView(height int) string

// SplashView returns the XL art styled for the splash screen (always XL regardless of terminal size).
func (b Banner) SplashView() string
```

**Size breakpoints:**
- Width >= 120 → `BannerSB` (side panel mode)
- Width 90-119 → `BannerS` (top banner)
- Width 60-89 → `BannerXS` (top banner)
- Width < 60 → `BannerNone`

**`View()`** returns the top-banner version (XS-A or S-A), centered. Returns `""` for BannerSB (side panel mode uses `SidePanelView` instead) and BannerNone.

**`SidePanelView(height)`** returns S-B art vertically centered within `height` rows. Pad with empty lines above/below. The panel is a fixed 48 cols wide (44 art + 2 padding each side).

**`SplashView()`** always returns the XL variant, centered, regardless of terminal width. Used only during the 1.5s launch splash.

### Color application approach

Render each line character-by-character or in spans. For efficiency, pre-compute the styled lines once (they're static). Cache the rendered output keyed by `(BannerSize, width)` so `View()` is a simple string return on subsequent calls.

## Files to Create/Modify

- `internal/tui/banner.go` — **NEW**: all art data, `Banner` struct, `View()`, `SidePanelView()`, `SplashView()`, Doppler coloring
- `internal/tui/styles.go` — Add `colorRedshift`, `colorBlueshift` if the existing palette doesn't cover the gradient

## Acceptance Criteria

- [ ] All four art variants stored as `[]string` line arrays
- [ ] `Size()` returns correct variant for each width breakpoint
- [ ] `View()` returns centered, styled XS-A or S-A art (or "" for side panel / none)
- [ ] `SidePanelView(height)` returns vertically centered S-B art within given height
- [ ] `SplashView()` returns XL art
- [ ] Red/magenta on left half, blue/cyan on right half, gold `@` cores
- [ ] `Q U A S A R` text uses logo-style coloring
- [ ] Rendered output is cached (no per-frame recomputation)
- [ ] `go build` passes
- [ ] `go test ./internal/tui/...` passes
