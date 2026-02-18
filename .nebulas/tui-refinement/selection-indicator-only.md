+++
id = "selection-indicator-only"
title = "Remove full-width selection background, keep only blue indicator bar"
type = "bug"
priority = 2
+++

## Problem

When scrolling through the phase list, the selected row gets highlighted with a full-width dusk purple background (`colorSelectionBg` = `#2D2D5E`). This highlights both the phase name and its status text in purple, which is visually heavy and distracting. The user prefers **only the blue indicator bar** (`▎` in `colorPrimary`) on the left edge of the selected row — no full-width background color change.

## Current State

`internal/tui/nebulaview.go`:
- Selected rows use `padToWidth()` to fill the terminal width with `colorSelectionBg` (#2D2D5E)
- The selection indicator `▎` (U+258E) is rendered in `colorPrimary` (starlight blue) on the left edge
- Both the indicator AND the full-width background are applied simultaneously

`internal/tui/styles.go`:
- `styleRowSelected` = bright white on `colorSelectionBg` (#2D2D5E)
- `styleRowNormal` = `colorMutedLight` with no background
- `colorSelectionBg` = `#2D2D5E` (dim nebula tint)

## Solution

### 1. Remove Full-Width Background from Selected Rows

Change `styleRowSelected` to NOT set a background color. The selected row text should use `colorBrightWhite` (or `colorWhite`) for emphasis but without any background fill.

### 2. Keep the Blue Indicator Bar

The `▎` indicator in `colorPrimary` on the left edge should remain — this is the only visual cue for the selected row.

### 3. Update padToWidth

Remove the `colorSelectionBg` background fill from `padToWidth()` for selected rows. The padding should use no background (terminal default).

### 4. Ensure Readability

Selected row text should still be slightly brighter than non-selected rows to provide a secondary visual cue:
- Selected: `colorBrightWhite` (#FFFFFF) or `colorWhite` (#E6EDF3) — brighter text
- Non-selected: `colorMutedLight` (#8B949E) — standard muted text

## Files to Modify

- `internal/tui/styles.go` — Remove `Background(colorSelectionBg)` from `styleRowSelected`
- `internal/tui/nebulaview.go` — Remove `colorSelectionBg` background from `padToWidth()` and selected row rendering

## Acceptance Criteria

- [ ] Selected rows show only the blue `▎` indicator on the left — no full-width background color
- [ ] Selected row text is brighter than non-selected rows for readability
- [ ] Non-selected rows remain unchanged (muted text, no indicator)
- [ ] The blue indicator bar is clearly visible as the selection cursor
- [ ] `go build` and `go vet ./...` pass