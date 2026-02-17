+++
id = "detail-refresh"
title = "Fix detail panel not refreshing for focused phase updates"
type = "bug"
priority = 1
+++

## Bug

When drilled into a phase (`FocusedPhase` is set), `MsgPhaseAgentDone`, `MsgPhaseCycleSummary`, and `MsgPhaseIssuesFound` don't call `updateDetailFromSelection()`. The detail panel shows stale content until the user manually navigates.

## Root Cause

The pattern `if m.FocusedPhase == msg.PhaseID { m.updateDetailFromSelection() }` already exists for `MsgPhaseAgentOutput` (line 280-282) and `MsgPhaseAgentDiff` (line 287-289), but was not added to the three other phase-contextualized message handlers.

## Fix

Add `if m.FocusedPhase == msg.PhaseID { m.updateDetailFromSelection() }` to:
- `MsgPhaseAgentDone` handler (after `lv.FinishAgent()`)
- `MsgPhaseCycleSummary` handler (after cost update)
- `MsgPhaseIssuesFound` handler (after `lv.SetIssueCount()`)

## File

`internal/tui/model.go`
