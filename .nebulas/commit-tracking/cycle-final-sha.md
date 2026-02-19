+++
id = "cycle-final-sha"
title = "Track one final SHA per cycle in CycleState"
type = "feature"
priority = 2
depends_on = []
+++

## Problem

`CycleState.CycleCommits` is a flat `[]string` that accumulates every commit SHA made during the loop — coder passes and lint fixes alike. There is no clear "final state of cycle N" marker. When a cycle has a coder commit followed by two lint-fix commits, `CycleCommits` ends up with 3 entries for that single cycle, and nothing tells you which SHA represents the cycle's completed state.

This makes it impossible to diff "cycle 2 end" vs "cycle 1 end" to see what changed over one review iteration.

## Solution

Change the commit tracking in `runCoderPhase` and `runLintFixLoop` so that **only the last SHA of each cycle** is stored in `CycleCommits`. The semantics become: `CycleCommits[i]` is the final commit SHA for cycle `i+1`.

Concretely:

1. In `runCoderPhase`, after the coder's `CommitCycle` call, store the SHA in a new `CycleState` field `lastCycleSHA` (unexported, transient — not persisted). Do NOT append to `CycleCommits` yet.

2. In `runLintFixLoop`, when a lint-fix commit succeeds, **overwrite** `lastCycleSHA` instead of appending to `CycleCommits`.

3. At the end of the cycle (after reviewer phase, before the next iteration of the `for cycle` loop), append `lastCycleSHA` to `CycleCommits`. This is the "seal" for that cycle.

4. In `handleApproval`, also seal the current cycle's SHA before returning.

This guarantees `len(CycleCommits) == number of completed cycles` and each entry is the final state.

## Files

- `internal/loop/state.go` — Add unexported `lastCycleSHA string` field to `CycleState`
- `internal/loop/loop.go` — Refactor `runCoderPhase`, `runLintFixLoop`, `runLoop`, and `handleApproval` to use the new seal pattern
- `internal/loop/loop_test.go` — Add test verifying `CycleCommits` has exactly one entry per cycle

## Acceptance Criteria

- [ ] `CycleCommits` has exactly one entry per completed cycle (not per commit)
- [ ] `CycleCommits[0]` is the final SHA after cycle 1 completes (including any lint fixes)
- [ ] Lint-fix commits overwrite the cycle's SHA rather than appending new entries
- [ ] `handleApproval` seals the final cycle's SHA before returning
- [ ] Existing tests pass; new test confirms one-SHA-per-cycle invariant