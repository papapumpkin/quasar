+++
id = "progress-indicators"
title = "Animated progress bars and enhanced spinners"
type = "feature"
priority = 2
depends_on = ["color-theme"]
+++

## Problem

The current TUI uses a basic `spinner.Dot` for working agents. There's no visual progress bar for nebula completion, no elapsed time per agent, and no budget consumption indicator. Per-phase LoopViews each have their own spinner but they're all the same plain dot style.

## Current State

The model receives `MsgNebulaProgress{Completed, Total, OpenBeads, ClosedBeads, TotalCostUSD}` and stores counts in `StatusBar`. Each `LoopView` (both the top-level one and per-phase ones in `PhaseLoops`) has a `Spinner spinner.Model`. The `StatusBar` struct has `Cycle`, `MaxCycles`, `CostUSD`, `MaxBudget` fields. Agent elapsed time is not tracked — `AgentEntry` has `DurationMs` (set on completion) but no start time.

## Solution

### Nebula Progress Bar
- Add a `progress.Model` (from `charmbracelet/bubbles`) to the status bar area
- Shows completion percentage: filled bar with phase count label
- Color transitions: blue → green as progress increases
- Update on every `MsgNebulaProgress`

### Agent Spinners
- Use `spinner.MiniDot` or `spinner.Pulse` for a more refined look
- Color the spinner to match the agent role (blue for coder, yellow for reviewer)
- Track agent start time: add `StartedAt time.Time` to `AgentEntry`, set on `StartAgent()`
- Show elapsed time next to the spinner that ticks up: `working... 12s`
- This applies to both top-level `LoopView` and per-phase `PhaseLoops` entries

### Budget Bar
- Small inline progress indicator in the status bar showing budget consumption
- Format: `$1.24 ━━━━━━░░░░ $10.00` (filled proportional to spend/budget)
- Changes color from green → yellow → red as budget approaches limit

### Cycle Progress (Loop Mode)
- Show `cycle 2/5` with a mini bar in the status bar: `[██░░░]`

## Files to Modify

- `internal/tui/statusbar.go` — Add progress bar rendering for budget and cycle/phase progress
- `internal/tui/loopview.go` — Add `StartedAt` to `AgentEntry`, enhanced spinner with elapsed time, colored per role
- `internal/tui/nebulaview.go` — Enhanced spinner with elapsed time for working phases
- `internal/tui/model.go` — Track elapsed time per agent (set `StartedAt` on `MsgAgentStart` and `MsgPhaseAgentStart`); pass `MsgTick` to per-phase LoopViews for time updates

## Acceptance Criteria

- [ ] Nebula mode shows a visual progress bar in the status bar area
- [ ] Budget consumption is visualized inline
- [ ] Working agents show elapsed time alongside spinner (both in loop and per-phase views)
- [ ] Spinners are colored per agent role
- [ ] Loop mode shows cycle progress mini-bar
- [ ] All indicators update smoothly without flicker
- [ ] Tests for progress bar string formatting
- [ ] `go build` and `go test ./internal/tui/...` pass
