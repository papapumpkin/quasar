+++
id = "line-count-bug"
title = "Fix +0 -0 line count in phase summaries"
type = "bug"
priority = 1
depends_on = []
+++

## Problem

Phase summaries always show `+0 -0` for line counts. The `captureGitDiff()` function in `bridge.go` falls back to `HEAD~1..HEAD` when baseRef and headRef are empty. In nebula mode with worktrees, the agent may not create traditional commits — or the diff range doesn't match what the agent actually changed. The `captureGitDiffStat()` call returns empty results, so `FileStatEntry.Additions` and `.Deletions` are both 0.

## Solution

Fix the git diff capture to reliably produce line counts in all execution modes.

### Approach

1. **Diagnose the root cause**: The `captureGitDiff` is called from `UIBridge.AgentDone()` and `PhaseUIBridge.AgentDone()` with empty baseRef/headRef, triggering the `HEAD~1..HEAD` fallback. Check whether:
   - The agent's changes are uncommitted (so `HEAD~1..HEAD` shows nothing)
   - The agent works in a worktree where HEAD hasn't moved
   - The refs should be coming from somewhere but aren't being passed

2. **Fix for uncommitted changes**: If the agent's changes are staged but not committed, use `git diff --cached` instead of `git diff HEAD~1..HEAD`. If they're unstaged, use `git diff` (no ref range). The bridge should try multiple strategies:
   ```
   1. If baseRef/headRef provided → git diff baseRef..headRef
   2. Else try git diff --cached (staged changes)
   3. Else try git diff (unstaged changes)
   4. Else try git diff HEAD~1..HEAD (last commit)
   ```

3. **Pass refs from the loop**: The loop tracks `BaseCommitSHA` and `FinalCommitSHA` in its result. Ensure these are propagated through the UI callback chain so `captureGitDiff` gets real refs instead of empty strings. Check `worker_exec.go` → `loop.RunExistingPhase()` → result → bridge.

4. **Fix `bus_bridge.go` too**: The bus subscriber path (`mapPhaseAgentDiff`) should also handle this correctly. Check if the bus event carries the right diff data or if it has the same empty-ref problem.

## Files

- `internal/tui/bridge.go` — fix `captureGitDiff()` fallback strategy, ensure refs are passed from loop results
- `internal/tui/bus_bridge.go` — verify bus path produces correct line counts
- `internal/tui/bridge_test.go` — add test for uncommitted/staged change detection
- `internal/loop/loop.go` — verify BaseCommitSHA/FinalCommitSHA are set correctly in results

## Acceptance Criteria

- [ ] Phase summaries show correct `+N -M` line counts after agent runs
- [ ] Works for both committed and uncommitted agent changes
- [ ] Works in both worktree and non-worktree execution modes
- [ ] `captureGitDiff` tries multiple strategies before returning empty
- [ ] `go test ./internal/tui/...` passes
- [ ] `go build ./...` and `go vet ./...` pass
