+++
id = "phase-progress-color"
title = "Color-code nebula phase progress to indicate doneness ratio"
type = "feature"
priority = 2
depends_on = ["status-bar-colors"]
+++

## Problem

The nebula phase status in the status bar shows progress like "4/10 done" but doesn't use color to convey how much is complete vs. in-progress. The progress counter should visually communicate the completion ratio through color, making it immediately obvious whether a nebula is just starting, mid-way, or almost done.

## Current State

`internal/tui/statusbar.go`:
- Progress segment renders as a simple text counter (e.g., "4/10 done")
- Uses a progress bar with `━` (filled) and `░` (empty) characters
- All in uniform `colorMutedLight`

`internal/tui/nebulaview.go`:
- Individual phase rows already have status-based colors (green for done, blue for working, etc.)
- But the aggregate progress in the status bar doesn't reflect this

## Solution

### 1. Progress Counter Color Gradient

Color the progress counter based on the completion ratio:

- **0% complete** (0/N): `colorMuted` (#484F58) — hasn't started
- **1-49% complete**: `colorBlue` (#79C0FF) — in progress, early
- **50-99% complete**: Blend toward `colorSuccess` (#00E676) — making good progress
- **100% complete** (N/N): `colorSuccess` (#00E676) bold — all done

### 2. Progress Bar Color

The `━` filled characters in the progress bar should also reflect completion:
- Filled segments: `colorSuccess` (green) for completed, `colorBlue` for in-progress portion
- Empty segments: `colorMuted` (gray)

### 3. In-Progress Count

If any phases are currently working, show the in-progress count in `colorBlue`:
- e.g., "4/10 done · 2 active" where "2 active" is in stellar blue

## Files to Modify

- `internal/tui/styles.go` — Add progress-level styles (or use inline styling based on ratio)
- `internal/tui/statusbar.go` — Apply dynamic coloring to the progress counter and bar based on completion ratio

## Acceptance Criteria

- [ ] Progress counter color changes based on completion percentage (gray → blue → green)
- [ ] Progress bar filled segments use green for done phases
- [ ] 100% completion renders in bold green
- [ ] In-progress phase count shown in blue when phases are actively working
- [ ] `go build` and `go vet ./...` pass