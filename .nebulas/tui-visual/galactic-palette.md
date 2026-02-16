+++
id = "galactic-palette"
title = "Galactic-themed color palette for the TUI"
type = "feature"
priority = 1
depends_on = []
+++

## Problem

The current TUI color palette is functional but generic — standard cyan, gold, gray tones that don't connect to Quasar's cosmic identity. A galactic-themed palette would make the TUI feel distinctive and immersive while maintaining readability.

## Current State

`internal/tui/styles.go` defines these semantic colors:
- `colorPrimary` = `#00BFFF` (cyan)
- `colorAccent` = `#FFD700` (gold)
- `colorSuccess` = `#00E676` (green) — **KEEP AS-IS** (user likes green for done)
- `colorDanger` = `#FF5252` (red)
- `colorMuted` = `#636363` (gray)
- `colorMutedLight` = `#8C8C8C` (lighter gray)
- `colorWhite` = `#EEEEEE` (off-white)
- `colorBrightWhite` = `#FFFFFF` (pure white)
- `colorSurface` = `#1E1E2E` (dark surface)
- `colorSurfaceBright` = `#2A2A3C` (lighter surface)
- `colorSurfaceDim` = `#181825` (darkest surface)
- `colorBlue` = `#5B8DEF` (blue — working/active)

These are used across status bar, row styles, breadcrumbs, gate prompts, footer, and detail panel.

## Solution

### New Galactic Palette

Replace the generic colors with a space-themed palette. **Do NOT change `colorSuccess` (#00E676 green)** — done phases stay green.

**Deep space backgrounds** (surfaces):
- `colorSurface` → deep space navy `#0D1117` or `#0B0E14` (near-black with blue undertone)
- `colorSurfaceBright` → nebula dust `#161B22` or `#1A1F2E` (subtle blue-purple tint)
- `colorSurfaceDim` → void black `#080B10` (darkest, for footer)

**Primary — starlight blue**:
- `colorPrimary` → `#58A6FF` or `#79C0FF` (bright stellar blue, like a hot star)

**Accent — star yellow / supernova orange**:
- `colorAccent` → `#FFA657` (supernova orange) for gates, attention, budget
- Add `colorStarYellow` → `#E3B341` or `#F0D060` for highlights, sparkle accents

**Purple — nebula purple**:
- Add `colorNebula` → `#BC8CFF` or `#D2A8FF` (soft purple) for breadcrumbs, secondary UI elements
- Add `colorNebulaDeep` → `#8B5CF6` (deeper purple) for selected state backgrounds or borders

**Working/active — light blue**:
- `colorBlue` → `#79C0FF` (lighter stellar blue for active/working states)

**Muted tones — cosmic dust**:
- `colorMuted` → `#484F58` (space dust gray with slight blue)
- `colorMutedLight` → `#8B949E` (lighter cosmic dust)

**Text**:
- `colorWhite` → `#E6EDF3` (slightly warm white, like distant starlight)
- `colorBrightWhite` → `#FFFFFF` (keep pure white for emphasis)

**Danger — stays red** (supernovae are also red):
- `colorDanger` → `#FF7B72` (slightly warmer red, supernova tinge)

### Where Colors Apply

Update the style variables that reference these colors. The style structure stays the same — only the color values change:

- **Status bar**: Deep space navy background, starlight blue logo, stellar white text
- **Breadcrumb**: Nebula purple text on slightly brighter surface
- **Phase tree**: Green done (unchanged), light blue working, supernova orange gates, cosmic dust waiting
- **Selection indicator**: Starlight blue `▎`
- **Detail panel border**: Cosmic dust gray, or subtle nebula purple
- **Gate prompt**: Supernova orange border and action text
- **Footer**: Cosmic dust background, starlight blue keys, muted descriptions

### What NOT to Change

- `colorSuccess` (#00E676 green) — done phases stay green
- Status icon characters (✓, ✗, ◎, ·, ⊘, –) — keep as-is
- Style structure (which styles exist, what they apply to) — keep as-is
- Selection indicator character (▎) — keep as-is

## Files to Modify

- `internal/tui/styles.go` — Update color constants with galactic palette; add `colorStarYellow`, `colorNebula`, `colorNebulaDeep`

## Acceptance Criteria

- [ ] All color constants updated to galactic-themed values
- [ ] `colorSuccess` remains green (#00E676) — done phases unchanged
- [ ] Deep blues and purples visible in surfaces and secondary UI
- [ ] Star yellow and supernova orange used for accents and attention states
- [ ] Text remains highly readable on dark backgrounds
- [ ] Existing style structure preserved (no style renames or removals)
- [ ] `go build` and `go test ./internal/tui/...` pass
