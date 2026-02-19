+++
id = "checkpoint-range-diff"
title = "Use base..final SHA range in BuildCheckpoint for accurate phase diffs"
type = "feature"
priority = 2
depends_on = ["task-result-shas", "range-diff"]
+++

## Problem

`BuildCheckpoint` currently calls `git.DiffLastCommit(ctx)` which does `HEAD~1..HEAD`. For a phase that ran 3 cycles with many commits, this only captures the final commit's changes. The diff shown in the checkpoint (and subsequently in the TUI gate view) is incomplete and misleading — it doesn't reflect the full work done during the phase.

## Solution

1. Update `BuildCheckpoint` signature to accept `baseCommitSHA` and `finalCommitSHA` from the `PhaseRunnerResult`.

2. When both SHAs are available, use `git.DiffRange(ctx, baseCommitSHA, finalCommitSHA)` and `git.DiffStatRange(ctx, baseCommitSHA, finalCommitSHA)` instead of `DiffLastCommit` / `DiffStatLastCommit`.

3. Fall back to `DiffLastCommit` when SHAs are missing (e.g., git disabled, standalone run without commit tracking).

4. Update the caller in `worker_exec.go` (`executePhase`) to pass the SHAs from `phaseResult`.

5. Add `BaseCommitSHA` and `FinalCommitSHA` fields to `Checkpoint` so downstream consumers (TUI, gate) know the exact range.

## Files

- `internal/nebula/checkpoint.go` — Update `BuildCheckpoint` signature and logic; add SHA fields to `Checkpoint`
- `internal/nebula/worker_exec.go` — Pass SHAs from `phaseResult` into `BuildCheckpoint`
- `internal/nebula/checkpoint_test.go` — Test range-based diffing and fallback behavior

## Acceptance Criteria

- [ ] `BuildCheckpoint` uses `DiffRange(base, final)` when both SHAs are provided
- [ ] Falls back to `DiffLastCommit` when SHAs are empty
- [ ] `Checkpoint` struct includes `BaseCommitSHA` and `FinalCommitSHA`
- [ ] `Checkpoint.Diff` reflects the full phase diff (all cycles), not just the last commit
- [ ] `Checkpoint.FilesChanged` reflects the full phase stat
- [ ] Caller in `executePhase` passes the SHAs through correctly
- [ ] Tests cover both the range path and the fallback path