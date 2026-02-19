+++
id = "task-result-shas"
title = "Expose BaseCommitSHA and FinalCommitSHA in TaskResult"
type = "feature"
priority = 2
depends_on = ["cycle-final-sha"]
+++

## Problem

`loop.TaskResult` only carries `TotalCostUSD`, `CyclesUsed`, and `Report`. It has no git information. The nebula layer (`PhaseRunnerResult`) inherits this gap — when `BuildCheckpoint` needs to compute a phase-level diff, it has no idea where the phase started or ended in the commit history.

Currently `BuildCheckpoint` calls `DiffLastCommit()` (`git diff HEAD~1..HEAD`), which only shows the last single commit. If a phase ran 3 cycles with multiple commits each, this misses everything except the final commit.

## Solution

1. Add `BaseCommitSHA` and `FinalCommitSHA` fields to `loop.TaskResult`.

2. In `handleApproval` and the max-cycles exit path in `runLoop`, populate these from `state.BaseCommitSHA` and the last entry in `state.CycleCommits` (or a fresh `HeadSHA()` call if `CycleCommits` is empty).

3. Add matching fields to `nebula.PhaseRunnerResult`.

4. Update `toPhaseRunnerResult` in `cmd/nebula_adapters.go` to copy the SHAs through.

## Files

- `internal/loop/loop.go` — Add fields to `TaskResult`, populate in `handleApproval` and max-cycles path
- `internal/nebula/worker_options.go` — Add `BaseCommitSHA` and `FinalCommitSHA` to `PhaseRunnerResult`
- `cmd/nebula_adapters.go` — Update `toPhaseRunnerResult` to forward the new fields

## Acceptance Criteria

- [ ] `TaskResult.BaseCommitSHA` equals the HEAD captured at task start
- [ ] `TaskResult.FinalCommitSHA` equals the last cycle's sealed SHA (or current HEAD as fallback)
- [ ] `PhaseRunnerResult` carries the same two fields
- [ ] `toPhaseRunnerResult` copies both fields through
- [ ] Both success and failure (max-cycles) paths populate the SHAs