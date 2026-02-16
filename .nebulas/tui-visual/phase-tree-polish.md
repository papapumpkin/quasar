+++
id = "phase-tree-polish"
title = "Enhance phase tree and cycle timeline visual hierarchy"
type = "feature"
priority = 2
depends_on = ["status-bar-redesign"]
+++

## Problem

The phase tree (nebula view) and cycle timeline (loop view) display the right information but could benefit from better visual hierarchy and polish. The user explicitly likes the current structure and information layout — this phase is about making it look better, not changing what's shown.

## Current State

**Nebula view** (`internal/tui/nebulaview.go`):
- Renders phase rows with: selection indicator (▎), status icon (✓/✗/◎/·/⊘/–), phase ID (left-aligned 24 chars), detail text
- Status icons use styled colors (green done, blue working, red failed, gold gate, gray waiting)
- Selected row gets `styleRowSelected` (bright white bold)
- Detail text shows wave/cost/cycles for done, "working..." for active, "blocked: X" for waiting

**Cycle timeline** (`internal/tui/loopview.go`):
- Renders cycle headers ("Cycle N") and indented agent entries
- Agent entries show: icon, role, duration, cost, issue count
- Working agents show spinner

**Styles** (`internal/tui/styles.go`):
- Selection indicator: `▎` in primary cyan
- Row styles: normal (muted light), selected (bright white), done (green), working (blue), failed (red bold), gate (gold bold), waiting (gray)

## Solution

### 1. Phase Table Alignment

Make the nebula view a proper aligned table with consistent column widths:
- Column 1: Selection indicator + status icon (fixed 4 chars)
- Column 2: Phase ID (fixed width, truncated if needed)
- Column 3: Status detail (wave, cost, cycles) — right-aligned or left-aligned consistently
- Use `lipgloss.Width()` to measure and pad columns

### 2. Tree Connector Lines

For the cycle timeline, add subtle tree-drawing characters to show hierarchy:
```
  Cycle 1
  ├── ✓ coder      12.3s  $0.45
  └── ✓ reviewer    8.1s  $0.32  → 2 issues
  Cycle 2
  ├── ◎ coder      working…  ⠋
```

Use `colorMuted` for the connector characters (├──, └──) so they're visible but not distracting. This makes the parent-child relationship between cycles and agents visually obvious.

### 3. Progress Bar for Active Phases

For phases in `PhaseWorking` state, show a subtle inline progress hint using the cycle count:
```
  ◎ auth-middleware   W2  cycle 2/5  ⠋
```
This gives more context than just "working..." without adding a full progress bar.

### 4. Phase Grouping by Wave

Add subtle wave separators in the nebula view when the wave number changes:
```
  ── Wave 1 ──
  ✓ setup-models         $0.15  2 cycles
  ── Wave 2 ──
  ◎ auth-middleware       working…
  · integration-tests    blocked: auth-middleware
```

Use `colorMuted` for the wave headers and `─` characters.

### 5. Improved Done State

For completed phases, show a more informative summary:
```
  ✓ setup-models         $0.15  2 cycles  12.3s
```
Add elapsed time if available (would need to track start/end time in `PhaseEntry`).

## Files to Modify

- `internal/tui/nebulaview.go` — Column alignment, wave grouping, richer detail text
- `internal/tui/loopview.go` — Tree connector characters (├──, └──)
- `internal/tui/styles.go` — Add `styleTreeConnector` for muted connector characters; add `styleWaveHeader` for wave separators

## Acceptance Criteria

- [ ] Cycle timeline shows tree connector lines (├──, └──)
- [ ] Nebula view has consistent column alignment
- [ ] Wave transitions have subtle separators
- [ ] Working phases show cycle progress ("cycle 2/5") instead of bare "working..."
- [ ] Existing information and selection behavior are preserved
- [ ] `go build` and `go test ./internal/tui/...` pass
