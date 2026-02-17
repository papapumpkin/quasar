+++
id = "loop-core"
title = "Test core loop state machine methods"
type = "task"
priority = 1
+++

## Goal

Increase `internal/loop` coverage from 47.2% to ~80% by testing the core loop orchestration methods that are currently at 0%.

## Current State

The following methods in `internal/loop/loop.go` have 0% coverage and are critical for refactor safety:

- `runLoop` — the main loop orchestrator
- `runCoderPhase` — coder agent invocation and result handling
- `runReviewerPhase` — reviewer agent invocation and parsing
- `checkBudget` — budget enforcement logic
- `handleApproval` — approval/rejection flow
- `perAgentBudget` — budget splitting
- `initCycleState` (50%) — cycle state initialization
- `emitCycleSummary` — UI emission for cycle results
- `emitBeadUpdate` — bead status updates
- `createFindingBeads` — bead creation for review findings
- `buildReviewerPrompt` — reviewer prompt construction
- `coderAgent` / `reviewerAgent` — agent configuration builders

Already well-tested (keep as-is):
- `buildCoderPrompt` (100%), `buildRefactorPrompt` (100%)
- `ParseReviewFindings` (100%), `isApproved` (100%), `collectContinuationLines` (100%)
- `drainRefactor` (100%)

## Approach

1. Use the existing mock patterns — `Loop` takes `agent.Invoker`, `beads.Client`, `ui.UI`, and a Git interface. Create mock implementations of each.
2. Test `runLoop` end-to-end with a mock invoker that returns controlled responses (approved on first cycle, rejected then approved, max cycles reached, budget exceeded).
3. Test `runCoderPhase` and `runReviewerPhase` individually with mock invokers.
4. Test `checkBudget` with various cost accumulations.
5. Test `handleApproval` with approved and rejected review outputs.
6. Test `emitCycleSummary`, `emitBeadUpdate`, `createFindingBeads` to verify they call the right UI/beads methods.

## Files

- `internal/loop/loop_test.go` — add tests (file exists, extend it)
- `internal/loop/loop.go` — read for understanding, do not modify unless necessary for testability

## Scope

- `internal/loop/` only
