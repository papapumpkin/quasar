+++
id = "fabric-auto-inject"
title = "Auto-inject fabric state into coder prompts"
type = "feature"
priority = 1
depends_on = ["scanner-injection"]
scope = ["internal/loop/prompts.go", "internal/loop/loop.go", "internal/nebula/worker_exec.go"]
+++

## Problem

`PrependFabricContext` exists in `internal/loop/prompts.go` (line 118) and is fully tested in `prompt_fabric_test.go`, but it is **never called** from the actual execution path. Agents running in a nebula with `FabricEnabled=true` have the fabric protocol instructions (telling them to run CLI commands to query fabric) but never receive the actual fabric state automatically.

This means every coder agent's first action in a multi-phase nebula is to manually run `quasar fabric entanglements` and `quasar fabric read` — wasting an agent turn and tokens on information we already have. The fabric snapshot is trivially available from the SQLite store; we just need to inject it.

## Solution

### 1. Add Fabric accessor to Loop

The loop needs access to the Fabric store to build snapshots. Add an optional field:

```go
type Loop struct {
    // ... existing fields
    Fabric fabric.Fabric // Optional; when set + FabricEnabled, auto-inject state
}
```

### 2. Inject in buildCoderPrompt

Before the task description, prepend the current fabric state on cycle 1 (or when fabric state has changed since last injection). Use the existing `PrependFabricContext` function:

```go
func (l *Loop) buildCoderPrompt(state *CycleState) string {
    desc := state.TaskTitle

    // Auto-inject fabric state when available.
    if l.FabricEnabled && l.Fabric != nil {
        snap := l.buildFabricSnapshot()
        desc = PrependFabricContext(desc, snap)
    }

    // ... rest of existing prompt logic (cycle 1 vs N, refactor, etc.)
}
```

### 3. Build snapshot helper

```go
func (l *Loop) buildFabricSnapshot() fabric.Snapshot {
    // Query fabric for current state.
    // All these methods already exist on the Fabric interface.
    entanglements, _ := l.Fabric.AllEntanglements()
    claims, _ := l.Fabric.AllClaims()
    states, _ := l.Fabric.AllPhaseStates()
    discoveries, _ := l.Fabric.UnresolvedDiscoveries()
    pulses, _ := l.Fabric.AllPulses()

    // Partition phases into completed/in-progress.
    var completed, inProgress []string
    for id, ps := range states {
        switch ps.Status {
        case nebula.PhaseStatusDone:
            completed = append(completed, id)
        case nebula.PhaseStatusInProgress:
            inProgress = append(inProgress, id)
        }
    }
    sort.Strings(completed)
    sort.Strings(inProgress)

    // Build claim map.
    claimMap := make(map[string]string, len(claims))
    for _, c := range claims {
        claimMap[c.Filepath] = c.OwnerTask
    }

    return fabric.Snapshot{
        Entanglements:         entanglements,
        FileClaims:            claimMap,
        Completed:             completed,
        InProgress:            inProgress,
        UnresolvedDiscoveries: discoveries,
        Pulses:                pulses,
    }
}
```

### 4. Wire Fabric through nebula adapters

In `cmd/nebula_adapters.go`, the `tuiLoopAdapter.RunExistingPhase` and `loopAdapter` already create `loop.Loop` per phase. Pass the fabric store through:

```go
l := &loop.Loop{
    // ... existing fields
    Fabric: a.fabric,  // Add this
}
```

### 5. Reviewer also gets fabric context

The reviewer should also see fabric state (especially discoveries and disputed entanglements) to assess whether the coder respected coordination constraints. Add fabric injection to `buildReviewerPrompt` as well.

### 6. Refresh strategy

Fabric state is injected once per agent invocation (at prompt build time). Since each cycle creates a new prompt, the agent always sees the latest state. No stale-cache concern.

## Files

- `internal/loop/loop.go` — Add `Fabric fabric.Fabric` field
- `internal/loop/prompts.go` — Call `PrependFabricContext` in `buildCoderPrompt` and `buildReviewerPrompt`
- `internal/loop/loop_test.go` — Update tests with mock fabric
- `cmd/nebula_adapters.go` — Pass fabric store through to loop construction
- `cmd/run.go` — Wire fabric for single-task mode (when `--fabric` flag is set)

## Acceptance Criteria

- [ ] When `FabricEnabled=true` and `Fabric` is non-nil, coder prompts include the fabric snapshot
- [ ] Reviewer prompts also include fabric state when enabled
- [ ] Fabric state refreshes each cycle (agents see latest entanglements/claims/discoveries)
- [ ] When `FabricEnabled=false` or `Fabric` is nil, behavior is identical to current (backward compatible)
- [ ] Agents no longer need to manually run `quasar fabric read` as their first action
- [ ] `go test ./internal/loop/...` passes
- [ ] `go vet ./...` clean
