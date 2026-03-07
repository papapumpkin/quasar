+++
id = "work-summary"
title = "Structured work summaries per phase and cycle"
type = "feature"
priority = 1
depends_on = []
+++

## Problem

When a cycle completes, the developer sees raw agent output and a diff. There's no structured summary answering: "What did the agent do this cycle? What decisions did it make? What's left?" The reviewer's `ReviewReport` has `summary` and `satisfaction` but it's buried in the data model and not surfaced prominently. The developer has to read full output to understand what happened.

## Solution

Generate and display structured work summaries at two levels: per-cycle and per-phase.

1. **Cycle summary card.** After each `EventAgentDone`, extract a structured summary from the agent output. The coder's output already contains reasoning — parse the last few lines or use a heuristic to extract the "what I did" section. Display this as a compact card in the loop view cycle row: 2-3 lines summarizing the action taken, files touched, and key decisions.

2. **Reviewer verdict display.** Surface the `ReviewReport` fields prominently in the cycle row: satisfaction level (as a colored badge: green/yellow/red), risk assessment, and the summary text. Currently `AgentEntry` stores `IssueCount` — also show the reviewer's one-line summary and whether human review was flagged.

3. **Phase completion summary.** When a phase finishes (done/failed), compose a summary panel accessible from the board view. Include: total cycles, final diff stat, reviewer's last assessment, list of files modified, cost breakdown, and duration. This is the "what happened" view for completed work.

4. **Summary in the detail panel header.** When drilling into a phase from the board, show the summary card at the top of the detail panel before the full output, so developers get context before diving into raw logs.

## Files

- `internal/tui/loopview.go` — add summary card rendering per cycle entry
- `internal/tui/nebulaview.go` — add completion summary data to `PhaseEntry`
- `internal/tui/detailpanel.go` — render summary header before body content
- `internal/tui/msg.go` — extend `MsgAgentDone` / add `MsgPhaseSummary` with structured fields
- `internal/tui/bridge.go` — extract summary from agent output when forwarding events
- `internal/tui/boardview.go` — show summary snippet on completed phase cards

## Acceptance Criteria

- [ ] Each cycle row in loop view shows a 2-3 line summary of what the agent did
- [ ] Reviewer satisfaction badge (green/yellow/red) and summary text visible per cycle
- [ ] Completed phases have a summary panel with diff stat, cycles, cost, and final assessment
- [ ] Detail panel shows summary header before raw output
- [ ] Summaries are concise (max 3 lines) and extracted without additional API calls
