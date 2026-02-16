+++
id = "status-bar-redesign"
title = "Redesign status bar for single-line layout and visual distinction"
type = "feature"
priority = 1
depends_on = ["quasar-logo"]
+++

## Problem

The status bar has three issues the user identified:
1. **Not on one line** — the content wraps when the terminal is narrower than the combined left+right text
2. **Too monochrome** — all text is roughly the same white/cyan, making it hard to scan
3. **Text looks too similar to the phase tree** — the status bar doesn't stand out as a distinct UI element

The current rendering:
```
 QUASAR  nebula: TUI Polish & Interactivity  0/6 done  $0.00  5m36s
```
uses `styleStatusLabel` (cyan bold), `styleStatusValue` (white), `styleStatusCost` (gold) all on a `colorSurface` (#1E1E2E) background. The problem is this is too subtle — the background is nearly indistinguishable from the terminal background on many dark themes.

## Current State

`internal/tui/statusbar.go`:
- `StatusBar.View()` builds a `left` section (logo + mode info) and `right` section (cost + elapsed)
- Pads with spaces to fill the width
- Uses `styleStatusBar.Width(s.Width).Render(line)` for the final output

`internal/tui/styles.go`:
- `styleStatusBar` has `Background(colorSurface)` (#1E1E2E), `Foreground(colorWhite)`, `Bold(true)`, `Padding(0, 1)`
- `colorSurface` is very dark (#1E1E2E) — barely visible as a distinct band

## Solution

### 1. Stronger Background

Change `styleStatusBar` to use a more visible background color — something that clearly reads as a "bar" even on dark terminals:
- Option A: Deeper indigo/navy (#1A1A40 or #2D2B55) — dark but clearly tinted
- Option B: Use the primary color as background with dark text (inverted) — very prominent
- The status bar should be the most visually dominant element in the TUI

### 2. Multi-Color Segments

Break the status bar into visually distinct colored segments instead of monochrome text:
- **Logo**: Primary cyan, bold (from quasar-logo phase)
- **Mode label** ("nebula:" or "task"): Dimmer, secondary color
- **Name**: Bright white, the most readable text
- **Progress** ("4/10 done"): Success green when some are done, muted when 0/N
- **Cost** ("$1.24 / $10.00"): Gold/amber accent
- **Elapsed** ("5m36s"): Muted gray — informational but not attention-grabbing

Each segment should have its own Lip Gloss style to enable this color differentiation.

### 3. Truncation Instead of Wrapping

When the terminal is too narrow, truncate the name with "..." rather than wrapping:
- Calculate available space: `width - logo - mode - progress - cost - elapsed - padding`
- If name exceeds available space, truncate to `available - 3` + "..."
- Priority order for dropping segments: elapsed first, then cost, then progress

### 4. Visual Separation from Tree

Add visual weight differentiation:
- Status bar: solid background, bold text, padding
- Phase tree below: no background, lighter text weight
- The contrast between "solid bar" and "open content" should be immediately obvious

## Files to Modify

- `internal/tui/styles.go` — Add per-segment styles (`styleStatusMode`, `styleStatusProgress`, `styleStatusElapsed`); strengthen `styleStatusBar` background
- `internal/tui/statusbar.go` — Rewrite `View()` to use per-segment styles and add truncation logic

## Acceptance Criteria

- [ ] Status bar content always fits on one line (truncates name if needed)
- [ ] At least 4 distinct colors visible in the status bar (logo, name, progress, cost)
- [ ] Status bar background is clearly visible as a distinct band on dark terminals
- [ ] Status bar is visually heavier/more prominent than the phase tree content below
- [ ] Progress text turns green when completed > 0
- [ ] `go build` and `go test ./internal/tui/...` pass
