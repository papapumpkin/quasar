+++
id = "wire-committer"
title = "Wire CycleCommitter into Loop, state, and cmd layer"
type = "feature"
priority = 1
depends_on = ["cycle-committer"]
scope = ["internal/loop/loop.go", "internal/loop/state.go", "cmd/run.go", "cmd/nebula.go"]
+++

## Problem

The `CycleCommitter` exists but isn't integrated into the loop or state tracking. We need the loop to commit after each coder cycle and track the resulting SHAs.

## Solution

### Add `Git` field to `Loop` struct

**File: `internal/loop/loop.go`** (around line 13-25)

- Add `Git CycleCommitter` field to the `Loop` struct
- After the coder completes (after `l.UI.AgentDone("coder", ...)`), call `l.Git.CommitCycle(ctx, <task-label>, cycle)` if `l.Git != nil`
- Capture the returned SHA and store it in `CycleState.CycleCommits`

### Track SHAs in state

**File: `internal/loop/state.go`** (around line 51-67, `CycleState` struct)

Add fields to `CycleState`:
```go
BaseCommitSHA string   // HEAD before first cycle (captured at task start)
CycleCommits  []string // commit SHA per cycle (index = cycle-1)
```

At task start (before the first cycle), capture `BaseCommitSHA` via `l.Git.HeadSHA(ctx)`.
After each coder cycle commit, append the SHA to `CycleCommits`.

### Wire in cmd layer

**File: `cmd/run.go`** — create `CycleCommitter` via `loop.NewCycleCommitter(".")`, set `loop.Git`

**File: `cmd/nebula.go`** — pass committer to per-phase loop instances. Nebula already commits at phase boundaries; per-cycle commits add finer granularity within phases. Create the committer once and pass it to each phase's loop.

### Existing tests

Update or add tests in `internal/loop/loop_test.go` if the `Loop` struct construction changes (e.g., ensure nil `Git` doesn't panic in the loop).

## Files to Modify

- `internal/loop/loop.go` — Add `Git` field, commit after coder phase
- `internal/loop/state.go` — Add `BaseCommitSHA`, `CycleCommits` to `CycleState`
- `cmd/run.go` — Create and wire `CycleCommitter`
- `cmd/nebula.go` — Pass `CycleCommitter` to phase loops

## Acceptance Criteria

- [ ] `Loop.Git` field exists and is used after coder completes
- [ ] `CycleState` tracks `BaseCommitSHA` and `CycleCommits`
- [ ] `cmd/run.go` creates and passes `CycleCommitter`
- [ ] `cmd/nebula.go` passes `CycleCommitter` to phase loops
- [ ] Nil `Git` field doesn't panic (no-op path)
- [ ] `go build` passes
- [ ] `go test ./internal/loop/...` passes
