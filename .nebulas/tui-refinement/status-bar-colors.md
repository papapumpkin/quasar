+++
id = "status-bar-colors"
title = "Restore per-segment coloring in status bar instead of uniform gray"
type = "bug"
priority = 1
depends_on = ["header-solid-bg"]
+++

## Problem

The status bar has a color regression: **all text is rendered in a single uniform color** (`colorMutedLight` = `#8B949E`). This makes it hard to visually scan — the mode label, nebula name, progress counter, cost, elapsed time, and resource stats all blend together into a monochrome gray bar.

Previously, different segments had distinct colors. The regression likely came from a refactor that applied `colorMutedLight` uniformly to all segment styles.

## Current State

`internal/tui/styles.go`:
- All status bar segment styles use `colorMutedLight` as foreground (lines ~44-88)
- Resource stats also render in `colorMutedLight` regardless of CPU/memory level (lines ~332-348)

`internal/tui/statusbar.go`:
- Segments: Logo, Mode ("nebula:"), Name, Progress ("4/10"), Cost ("$1.24"), Elapsed ("5m36s"), Resources ("◈2 48MB 3.2%")

`internal/tui/resources.go`:
- CPU/memory thresholds exist (50%/80% CPU, 1024MB/2048MB memory) but the color output is uniform

## Solution

### 1. Restore Per-Segment Foreground Colors

Update each segment style in `styles.go` to use distinct foreground colors:

- **Mode label** ("nebula:" / "task"): `colorMuted` (#484F58) — dim, de-emphasized
- **Name**: `colorWhite` (#E6EDF3) bold — the primary information, most readable
- **Progress** ("4/10 done"): `colorMutedLight` when 0 done, blended toward `colorSuccess` as phases complete
- **Cost** ("$1.24"): `colorAccent` (#FFA657) — gold/amber to stand out as a monetary value
- **Elapsed** ("5m36s"): `colorMutedLight` (#8B949E) — informational, not attention-grabbing

### 2. Resource Stats Color Coding

Wire up the existing CPU/memory thresholds to actual color changes:

- **Normal**: `colorMutedLight` — CPU < 50%, Memory < 1024MB
- **Warning**: `colorBudgetWarn` / `colorAccent` (#FFA657 orange) — CPU 50-80%, Memory 1024-2048MB
- **Danger**: `colorDanger` (#FF7B72 red) — CPU > 80%, Memory > 2048MB

The thresholds already exist in `resources.go`; the rendering just needs to pick the right style based on the current level.

### 3. Cost Budget Coloring

If a budget is set (`max_budget_usd > 0`):
- Under 50% spent: `colorAccent` (amber)
- 50-80% spent: `colorBudgetWarn` (orange)
- Over 80% spent: `colorDanger` (red)

## Files to Modify

- `internal/tui/styles.go` — Update foreground colors for each status bar segment style
- `internal/tui/statusbar.go` — Use the correct style per segment; add logic for dynamic cost/resource coloring
- `internal/tui/resources.go` — Expose threshold level (normal/warning/danger) so statusbar can pick the right style

## Acceptance Criteria

- [ ] At least 4 distinct colors visible in the status bar (mode, name, progress, cost, elapsed)
- [ ] Resource stats turn orange at warning thresholds and red at danger thresholds
- [ ] Cost display changes color based on budget usage percentage
- [ ] The overall bar is visually scannable — each segment is distinguishable at a glance
- [ ] `go build` and `go test ./internal/tui/...` pass