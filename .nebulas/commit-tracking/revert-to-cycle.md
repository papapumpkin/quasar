+++
id = "revert-to-cycle"
title = "Add ResetTo method on CycleCommitter for reverting to a cycle's state"
type = "feature"
priority = 3
depends_on = ["cycle-final-sha"]
+++

## Problem

When a cycle produces bad results or the reviewer rejects changes, there's no programmatic way to revert the working tree to a previous cycle's known-good state. The SHAs are tracked but nothing uses them for restoration. Users and the TUI gate system could benefit from a "revert to cycle N" capability.

## Solution

1. Add `ResetTo(ctx context.Context, sha string) error` to `loop.CycleCommitter`. This performs `git reset --hard <sha>` to restore the working tree to the given commit.

2. Implement in `gitCycleCommitter` with branch verification (must be on the expected branch before resetting).

3. Add a matching `ResetTo` on `nebula.GitCommitter` for phase-level resets.

4. Both implementations must verify the target SHA exists and is an ancestor of the current branch to prevent jumping to unrelated commits.

## Files

- `internal/loop/git.go` — Add `ResetTo` to `CycleCommitter` interface and `gitCycleCommitter`
- `internal/nebula/git.go` — Add `ResetTo` to `GitCommitter` interface and `gitCommitter`
- `internal/loop/git_test.go` — Test reset behavior, invalid SHA, ancestor check
- `internal/nebula/git_test.go` — Test reset behavior

## Acceptance Criteria

- [ ] `CycleCommitter.ResetTo(ctx, sha)` performs `git reset --hard <sha>`
- [ ] `GitCommitter.ResetTo(ctx, sha)` performs `git reset --hard <sha>`
- [ ] Both verify the SHA is a valid, reachable commit before resetting
- [ ] Both verify branch (if branch enforcement is active) before resetting
- [ ] Nil receivers are no-ops (nil-safety preserved)
- [ ] Tests confirm working tree matches the target SHA after reset
- [ ] Tests confirm error on invalid SHA