+++
id = "header-solid-bg"
title = "Make header background solid full-width dusk purple with no gaps"
type = "bug"
priority = 1
+++

## Problem

The dusk purple background color (`colorSurface` = `#1A1A40`) on the top status bar is patchy — it doesn't extend uniformly across the full terminal width. There are visible gaps where the background doesn't render, making it look fragmented rather than a solid bar. The goal is to make it look like a single HTML `<div>` with a uniform background — solid edge-to-edge with no breaks.

## Current State

`internal/tui/statusbar.go`:
- `StatusBar.View()` builds left/right sections and pads between them
- Uses `styleStatusBar.Width(s.Width).Render(line)` to render the final bar
- The `styleStatusBar` has `Background(colorSurface)` but individual segment styles may override or not inherit the background, creating gaps

`internal/tui/styles.go`:
- `styleStatusBar` sets `Background(colorSurface)`, `Foreground(colorWhite)`, `Padding(0, 1)`
- Individual segment styles (e.g., `styleStatusMode`, `styleStatusName`, `styleStatusProgress`, `styleStatusCost`, `styleStatusElapsed`) may not explicitly set `Background(colorSurface)`, causing their background to show through as the terminal default

## Solution

### 1. Ensure All Segment Styles Inherit Background

Every style used within the status bar must explicitly set `Background(colorSurface)` so there are no gaps where the terminal background shows through:

- `styleStatusMode` — add `Background(colorSurface)`
- `styleStatusName` — add `Background(colorSurface)`
- `styleStatusProgress` — add `Background(colorSurface)`
- `styleStatusCost` — add `Background(colorSurface)`
- `styleStatusElapsed` — add `Background(colorSurface)`
- `styleStatusPaused` — add `Background(colorSurface)`
- `styleStatusStopping` — add `Background(colorSurface)`
- The logo style used inside the status bar should also carry `Background(colorSurface)` for the jets

### 2. Ensure Padding Fills Correctly

The space-padding between left and right segments must also carry the background color. Verify that `styleStatusBar.Width(s.Width)` correctly fills the entire width with the background, including padding characters.

### 3. No Background If Impossible

If lipgloss cannot render a truly gap-free background across all terminal emulators, remove the background color entirely rather than showing a patchy bar. However, lipgloss's `Width()` combined with explicit background on all child styles should achieve this.

## Files to Modify

- `internal/tui/styles.go` — Add `Background(colorSurface)` to every status bar segment style
- `internal/tui/statusbar.go` — Verify the padding/fill logic produces no gaps
- `internal/tui/logo.go` — Ensure logo jet/core styles carry the background when rendered inside the status bar

## Acceptance Criteria

- [ ] The status bar renders as a solid, unbroken band of `colorSurface` (#1A1A40) across the full terminal width
- [ ] No terminal-default background shows through between segments
- [ ] Resizing the terminal preserves the solid background (no gaps appear at any width)
- [ ] `go build` and `go vet ./...` pass