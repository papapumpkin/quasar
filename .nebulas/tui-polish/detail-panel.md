+++
id = "detail-panel"
title = "Rich detail panel with formatted output and scroll indicators"
type = "feature"
priority = 2
depends_on = ["color-theme"]
+++

## Problem

The detail panel is a plain viewport that dumps raw agent output text. There's no formatting, no scroll position indicator, and no contextual information about the selected item. The panel only appears at `DepthAgentOutput` — it could also be useful at `DepthPhaseLoop` to show phase summary info.

## Current State

The TUI has a three-level drill-down: `DepthPhases` (phase table) → `DepthPhaseLoop` (per-phase cycle timeline) → `DepthAgentOutput` (agent output in detail panel). The detail panel (`DetailPanel` in `detailpanel.go`) wraps a `viewport.Model` with `SetContent(title, content)`. The `updateDetailFromSelection()` method in `model.go` populates it:
- In loop mode: shows `agent.Output` from the selected `AgentEntry`
- In nebula mode at `DepthPhaseLoop`/`DepthAgentOutput`: shows agent output from `PhaseLoops[FocusedPhase].SelectedAgent()`
- Currently only visible when `showDetailPanel()` returns true (at `DepthAgentOutput`)

## Solution

### Contextual Headers
- When a loop agent row is selected: show role, cycle number, duration, cost, and issue count as a formatted header above the output
- When drilled into a nebula phase (`DepthPhaseLoop`): show phase ID, title, dependencies, status, cost, cycles used as a summary header — even before drilling into agent output
- When at `DepthAgentOutput` inside a phase: show both phase context and agent context

### Output Formatting
- Wrap agent output in a styled box with the role as title
- Highlight key patterns in output: `APPROVED` in green, `ISSUE:` blocks in yellow, `SEVERITY: critical` in red
- Truncate very long outputs with a "(truncated -- N lines)" indicator

### Scroll Indicators
- Show scroll position: `up 12 more` at top, `down 34 more` at bottom when content overflows
- Use the viewport's built-in scroll percentage or compute from `YOffset` and content height
- Style the indicators dimly so they don't distract

### Empty State
- When no item is selected or detail is collapsed, show a hint: "Press enter to expand details"
- When an agent is still working, show "Output will appear when the agent completes" (already done in `updateDetailFromSelection`)

### Show Detail at Phase Level
- Optionally show the detail panel at `DepthPhaseLoop` too (not just `DepthAgentOutput`) to display a phase summary card. Update `showDetailPanel()` accordingly.

## Files to Modify

- `internal/tui/detailpanel.go` — Contextual headers, output formatting helpers, scroll indicators, empty states
- `internal/tui/model.go` — Update `updateDetailFromSelection()` to build richer context; optionally show detail at `DepthPhaseLoop`; update `showDetailPanel()`
- `internal/tui/loopview.go` — Expose selected entry context (role, cycle, cost, duration via `SelectedAgent()`)
- `internal/tui/nebulaview.go` — Expose selected phase context via `SelectedPhase()` (already exists, returns `*PhaseEntry`)

## Acceptance Criteria

- [ ] Detail panel shows contextual header for selected item
- [ ] Phase summary shown when drilled into a phase's cycle timeline
- [ ] Agent output has key patterns highlighted (APPROVED, ISSUE, SEVERITY)
- [ ] Scroll indicators show when content overflows
- [ ] Empty states provide helpful hints
- [ ] Long output is truncated with indicator
- [ ] Tests for output formatting helpers
- [ ] `go build` and `go test ./internal/tui/...` pass
