+++
id = "worker-dispatch"
title = "Integrate speculative dispatch into WorkerGroup.Run dispatch loop"
type = "feature"
priority = 2
depends_on = ["tycho-speculation"]
scope = ["internal/nebula/worker.go", "internal/nebula/worker_exec.go", "internal/nebula/worker_fabric.go"]
+++

## Problem

The `WorkerGroup.Run()` dispatch loop in `internal/nebula/worker.go` currently follows a strict pattern: call `tychoScheduler.Eligible()` to get confirmed-ready phases, scan them through fabric, dispatch them, then wait for any completion before re-evaluating. There is no point in the loop where speculative candidates are considered or dispatched.

Even with the speculative state primitives (phase 1) and Tycho's speculative candidate resolution (phase 2) in place, the actual dispatch loop does not call `SpeculativeEligible()`, does not dispatch speculative workers, and does not handle the lifecycle of speculative phases (confirmation on review pass, discard on review reject).

The dispatch loop also needs to be careful about resource management: speculative phases consume a worker slot from the semaphore, and if the review rejects, that slot was "wasted." The design must ensure speculative dispatch does not starve confirmed work.

## Solution

### 1. Add speculative dispatch after confirmed dispatch

In the main `for ctx.Err() == nil` loop within `WorkerGroup.Run()`, after dispatching all confirmed-eligible phases but before calling `awaitCompletion`, add a speculative dispatch step:

```go
// --- After dispatching confirmed eligible phases ---

// Speculative dispatch: if there is spare worker capacity, speculatively
// schedule phases whose sole remaining dependency is in review.
if wg.Nebula.Manifest.Execution.Speculative {
    wg.dispatchSpeculative(ctx, sem, completionCh, &activeCount, &peakConcurrent, scheduler)
}

// After dispatching, wait for any one goroutine to finish...
wg.awaitCompletion(completionCh, &activeCount)
```

### 2. Implement `dispatchSpeculative` method

```go
// dispatchSpeculative queries Tycho for speculative candidates and dispatches
// them if worker capacity is available. Speculative phases are marked in the
// tracker and given a SpeculativeContext for rollback. Only one speculative
// phase per dependency is dispatched (no cascading speculation).
func (wg *WorkerGroup) dispatchSpeculative(
    ctx context.Context,
    sem chan struct{},
    completionCh chan<- string,
    activeCount *int64,
    peakConcurrent *int64,
    scheduler *Scheduler,
) {
    wg.mu.Lock()
    candidates := wg.tychoScheduler.SpeculativeEligible(ctx)
    inFlight := wg.tracker.InFlight()
    wg.mu.Unlock()

    for _, cand := range candidates {
        if ctx.Err() != nil {
            break
        }

        // Don't speculate if we're at worker capacity — confirmed work
        // takes priority. Use a non-blocking send on the semaphore.
        select {
        case sem <- struct{}{}:
            // Got a slot — proceed with speculative dispatch.
        default:
            // At capacity — skip speculation this cycle.
            return
        }

        wg.mu.Lock()
        // Double-check the candidate is still valid (race with completion).
        if inFlight[cand.PhaseID] || wg.tracker.Done()[cand.PhaseID] {
            wg.mu.Unlock()
            <-sem
            continue
        }

        // Record the speculative context for rollback.
        baseCommit := wg.currentHEAD()
        specCtx := &SpeculativeContext{
            DependsOnPhaseID: cand.SpeculatesOn,
            BaseCommitSHA:    baseCommit,
            StartedAt:        time.Now(),
        }
        wg.tracker.MarkSpeculative(cand.PhaseID, specCtx)
        inFlight[cand.PhaseID] = true
        wg.mu.Unlock()

        atomic.AddInt64(activeCount, 1)
        go func(phaseID string) {
            defer func() {
                <-sem
                completionCh <- phaseID
            }()
            // Track peak concurrency.
            for {
                peak := atomic.LoadInt64(peakConcurrent)
                cur := atomic.LoadInt64(activeCount)
                if cur <= peak || atomic.CompareAndSwapInt64(peakConcurrent, peak, cur) {
                    break
                }
            }
            trackID := scheduler.TrackForTask(phaseID)
            wg.executePhase(ctx, phaseID, trackID)
        }(cand.PhaseID)

        fmt.Fprintf(wg.logger(), "  speculative: dispatched %q (speculates on %q review)\n",
            cand.PhaseID, cand.SpeculatesOn)
    }
}
```

### 3. Handle speculative phase completion in the dispatch loop

After `awaitCompletion` returns a completed phase ID, the loop must check whether any speculative phases depended on the completed phase's review result:

```go
// After awaitCompletion:
wg.resolveSpeculativeOutcomes(ctx, completedPhaseID)
```

Implement the resolution method:

```go
// resolveSpeculativeOutcomes checks all speculative phases to see if their
// dependency has completed. If the dependency passed review (done, not failed),
// the speculative phase is promoted to confirmed. If the dependency failed or
// was rejected, the speculative phase is discarded and its work rolled back.
func (wg *WorkerGroup) resolveSpeculativeOutcomes(ctx context.Context, completedPhaseID string) {
    wg.mu.Lock()
    defer wg.mu.Unlock()

    for phaseID, specCtx := range wg.tracker.specCtx {
        if specCtx.DependsOnPhaseID != completedPhaseID {
            continue
        }

        if wg.tracker.Done()[completedPhaseID] && !wg.tracker.Failed()[completedPhaseID] {
            // Review passed — confirm the speculative phase.
            wg.tracker.ConfirmSpeculative(phaseID)
            if wg.Fabric != nil {
                if err := wg.Fabric.SetPhaseState(ctx, phaseID, fabric.StateRunning); err != nil {
                    fmt.Fprintf(wg.logger(), "warning: failed to confirm speculative state for %q: %v\n", phaseID, err)
                }
            }
            fmt.Fprintf(wg.logger(), "  speculative: confirmed %q (dependency %q passed)\n", phaseID, completedPhaseID)
        } else {
            // Review failed or phase failed — discard speculative work.
            wg.discardSpeculativePhase(ctx, phaseID, specCtx)
        }
    }
}
```

### 4. Modify `executePhase` for speculative awareness

In `internal/nebula/worker_exec.go`, update `executePhase` to set the fabric state to `StateSpeculative` instead of `StateRunning` when the phase is speculative:

```go
func (wg *WorkerGroup) executePhase(ctx context.Context, phaseID string, waveNumber int) {
    // ... existing preamble ...

    wg.mu.Lock()
    if wg.tracker.IsSpeculative(phaseID) {
        wg.State.SetPhaseState(phaseID, ps.BeadID, PhaseStatusSpeculative)
        if wg.Fabric != nil {
            wg.Fabric.SetPhaseState(ctx, phaseID, fabric.StateSpeculative)
        }
    } else {
        wg.State.SetPhaseState(phaseID, ps.BeadID, PhaseStatusInProgress)
    }
    // ... rest of existing logic ...
}
```

### 5. Feed loop phase transitions back to WorkerGroup

The loop adapter (`cmd/nebula_adapters.go` via `tuiLoopAdapter.RunExistingPhase`) creates a `loop.Loop` per phase. Add a callback that calls `wg.RegisterPhaseState(phaseID, phase)` on each `CycleState.Phase` transition. This can be done via the existing `UI` interface — the `PhaseUIBridge` already receives `AgentStart` and `AgentDone` calls which map to `PhaseCoding` and `PhaseReviewing` transitions. Wire the bridge to also call `RegisterPhaseState` on the `WorkerGroup`.

### 6. Add `currentHEAD` helper

```go
// currentHEAD returns the current git HEAD SHA, or empty string on error.
func (wg *WorkerGroup) currentHEAD() string {
    // Use exec.CommandContext to run "git rev-parse HEAD".
    // This is called under the mutex, so it must be fast.
}
```

## Files

- `internal/nebula/worker.go` — Add `dispatchSpeculative`, `resolveSpeculativeOutcomes` methods; integrate speculative dispatch into `Run()` loop; add `currentHEAD` helper
- `internal/nebula/worker_exec.go` — Update `executePhase` to set `PhaseStatusSpeculative`/`StateSpeculative` for speculative phases
- `internal/nebula/worker_fabric.go` — Ensure `workerEligibleResolver` filters speculative phases from confirmed-eligible list
- `cmd/nebula_adapters.go` — Wire `RegisterPhaseState` calls from `PhaseUIBridge` to `WorkerGroup`

## Acceptance Criteria

- [ ] `dispatchSpeculative` only runs when `Execution.Speculative` is true
- [ ] Speculative dispatch uses non-blocking semaphore acquire — never starves confirmed work
- [ ] At most one speculative phase per dependency is dispatched (no cascading speculation)
- [ ] Speculative phases are set to `PhaseStatusSpeculative`/`StateSpeculative` on the fabric
- [ ] `resolveSpeculativeOutcomes` promotes speculative phases when dependency review passes
- [ ] `resolveSpeculativeOutcomes` triggers discard when dependency review fails (delegates to rollback mechanism)
- [ ] Loop phase transitions (`PhaseCoding`, `PhaseReviewing`) are fed back to `WorkerGroup` via `RegisterPhaseState`
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] Existing non-speculative tests remain green
- [ ] New integration test: two-phase chain with speculative enabled, confirm path
- [ ] New integration test: two-phase chain with speculative enabled, discard path
