+++
id = "split-bridge"
title = "Split internal/tui/bridge.go into bridge.go and bridge_phase.go"
type = "task"
priority = 2
scope = ["internal/tui/bridge.go", "internal/tui/bridge_phase.go"]
+++

## Problem

`internal/tui/bridge.go` is 428 lines, exceeding the 400-line arch test limit. The file contains two independent bridge implementations: `UIBridge` (for the single-task loop) and `PhaseUIBridge` (for nebula phase workers).

## Solution

Split along the natural `UIBridge` / `PhaseUIBridge` boundary.

### `bridge.go` keeps (~170 lines):

- `gitDiffTimeout` constant
- `UIBridge` struct and `NewUIBridge` constructor
- All `(*UIBridge).*` methods: `TaskStarted`, `TaskComplete`, `CycleStart`, `AgentStart`, `AgentDone`, `CycleSummary`, `IssuesFound`, `Approved`, `MaxCyclesReached`, `BudgetExceeded`, `Error`, `Info`, `AgentOutput`, `RefactorApplied`, `HailReceived`, `HailResolved`, `BeadUpdate`
- `buildBeadInfoTree` helper function
- `diffResult` struct
- Git-diff helpers: `captureGitDiff`, `captureGitDiffStat`, `parseNumstat`

### `bridge_phase.go` (new) gets (~260 lines):

- `PhaseUIBridge` struct and `NewPhaseUIBridge` constructor
- All `(*PhaseUIBridge).*` methods: `TaskStarted`, `TaskComplete`, `CycleStart`, `AgentStart`, `AgentDone`, `CycleSummary`, `IssuesFound`, `Approved`, `MaxCyclesReached`, `BudgetExceeded`, `Error`, `Info`, `AgentOutput`, `RefactorApplied`, `BeadUpdate`, `HailReceived`, `HailResolved`, `EntanglementPublished`, `DiscoveryPosted`, `Hail`, `HailAndWait`, `ScratchpadNote`

### Steps

1. Create `internal/tui/bridge_phase.go` with `package tui` and necessary imports
2. Move `PhaseUIBridge` struct, `NewPhaseUIBridge`, and all `(*PhaseUIBridge).*` methods
3. Remove moved code and any now-unused imports from `bridge.go`
4. Verify: `go build ./internal/tui/...`

## Files

- `internal/tui/bridge.go` — remove PhaseUIBridge and its methods
- `internal/tui/bridge_phase.go` — new file with PhaseUIBridge

## Acceptance Criteria

- [ ] `bridge.go` is under 400 lines
- [ ] `bridge_phase.go` contains all PhaseUIBridge code
- [ ] `go build ./...` compiles without errors
- [ ] `go test ./internal/tui/...` passes
- [ ] No functionality changes — code is only relocated
