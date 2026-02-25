+++
id = "split-msg"
title = "Split internal/tui/msg.go into msg.go and msg_phase.go"
type = "task"
priority = 2
scope = ["internal/tui/msg.go", "internal/tui/msg_phase.go"]
+++

## Problem

`internal/tui/msg.go` is 402 lines, exceeding the 400-line arch test limit. The file contains two distinct categories of message types: loop-level/system messages and phase-scoped/nebula/fabric messages.

## Solution

Split `msg.go` into two files along the natural boundary between loop-level and phase-scoped messages.

### `msg.go` keeps (loop-level, system/lifecycle, plan types):

**Loop-level messages:**
- `MsgTaskStarted`, `MsgTaskComplete`, `MsgCycleStart`, `MsgAgentStart`, `MsgAgentDone`
- `MsgCycleSummary`, `MsgIssuesFound`, `MsgApproved`, `MsgMaxCyclesReached`, `MsgBudgetExceeded`
- `MsgError`, `MsgInfo`, `MsgAgentOutput`, `MsgAgentDiff`

**System/lifecycle messages:**
- `MsgTick`, `MsgLoopDone`, `MsgNebulaDone`, `MsgGitPostCompletion`
- `MsgNebulaChoicesLoaded`, `MsgToastExpired`, `MsgResourceUpdate`, `MsgSplashDone`

**Plan types:**
- `PlanAction`, `MsgPlanReady`, `MsgPlanAction`, `MsgPlanError`

### `msg_phase.go` (new) gets:

**Phase-scoped messages:**
- `MsgPhaseTaskStarted`, `MsgPhaseTaskComplete`, `MsgPhaseCycleStart`, `MsgPhaseAgentStart`
- `MsgPhaseAgentDone`, `MsgPhaseAgentOutput`, `MsgPhaseAgentDiff`, `MsgPhaseCycleSummary`
- `MsgPhaseIssuesFound`, `MsgPhaseApproved`, `MsgPhaseError`, `MsgPhaseInfo`, `PhaseInfo`

**Nebula messages:**
- `MsgNebulaInit`, `MsgNebulaProgress`, `MsgGatePrompt`, `MsgGateResolved`
- `MsgPhaseRefactorPending`, `MsgPhaseRefactorApplied`, `MsgPhaseHotAdded`, `MsgPhaseScanning`

**Fabric messages:**
- `MsgEntanglementUpdate`, `MsgDiscoveryPosted`, `MsgHail`, `MsgHailReceived`
- `MsgHailResolved`, `MsgScratchpadEntry`, `MsgStaleWarning`

**Bead messages:**
- `BeadInfo`, `MsgBeadUpdate`, `MsgPhaseBeadUpdate`

### Steps

1. Create `internal/tui/msg_phase.go` with `package tui` and the same imports as needed
2. Move the phase/nebula/fabric/bead message types from `msg.go` to `msg_phase.go`
3. Remove moved types and any now-unused imports from `msg.go`
4. Verify both files compile: `go build ./internal/tui/...`

## Files

- `internal/tui/msg.go` — remove phase/nebula/fabric/bead messages
- `internal/tui/msg_phase.go` — new file with moved messages

## Acceptance Criteria

- [ ] `msg.go` is under 400 lines
- [ ] `msg_phase.go` contains all phase-scoped, nebula, fabric, and bead messages
- [ ] `go build ./...` compiles without errors
- [ ] `go test ./internal/tui/...` passes
- [ ] No functionality changes — types are only relocated
