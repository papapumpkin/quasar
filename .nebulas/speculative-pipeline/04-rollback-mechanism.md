+++
id = "rollback-mechanism"
title = "Implement git-based rollback for discarded speculative work"
type = "feature"
priority = 1
depends_on = ["worker-dispatch"]
scope = ["internal/nebula/speculative_rollback.go", "internal/nebula/worker_exec.go", "internal/nebula/tracker.go"]
+++

## Problem

When a speculative phase's dependency fails review, the speculative work must be completely discarded. This is the "pipeline flush" — analogous to a CPU discarding instructions from a mispredicted branch. The speculative phase may have generated code changes, created child beads, posted fabric entanglements, and claimed files. All of these side effects must be unwound.

The current codebase has no rollback capability. `executePhase` in `internal/nebula/worker_exec.go` always calls `recordResult` which marks phases as done/failed, and `fabricPhaseComplete` which publishes entanglements and marks the fabric state as `StateDone`. There is no mechanism to undo these operations, and there is no git rollback to discard code changes.

The rollback must handle several scenarios:
1. The speculative phase is still running when the dependency review rejects — the phase's context must be cancelled.
2. The speculative phase has already completed when the dependency rejects — its committed changes must be reverted.
3. The speculative phase's own review is in progress when the dependency rejects — both coder and reviewer work are discarded.

## Solution

### 1. Create `internal/nebula/speculative_rollback.go`

This file contains all rollback logic, keeping it isolated from the main execution path:

```go
package nebula

import (
    "context"
    "fmt"
    "os/exec"

    "github.com/papapumpkin/quasar/internal/fabric"
)

// discardSpeculativePhase rolls back all side effects of a speculative phase
// whose dependency failed review. It cancels any in-flight work, reverts git
// changes, releases fabric state, and returns the phase to eligible status.
//
// Must be called with wg.mu held.
func (wg *WorkerGroup) discardSpeculativePhase(ctx context.Context, phaseID string, specCtx *SpeculativeContext) {
    fmt.Fprintf(wg.logger(), "  speculative: discarding %q (dependency %q failed)\n",
        phaseID, specCtx.DependsOnPhaseID)

    // 1. Cancel the speculative phase's context if still running.
    wg.cancelSpeculativePhase(phaseID)

    // 2. Revert git changes back to the base commit.
    if specCtx.BaseCommitSHA != "" {
        wg.mu.Unlock()
        if err := wg.revertSpeculativeChanges(ctx, phaseID, specCtx.BaseCommitSHA); err != nil {
            fmt.Fprintf(wg.logger(), "warning: failed to revert speculative changes for %q: %v\n", phaseID, err)
        }
        wg.mu.Lock()
    }

    // 3. Clean up fabric state.
    wg.cleanupSpeculativeFabric(ctx, phaseID)

    // 4. Return the phase to eligible status.
    wg.tracker.DiscardSpeculative(phaseID)
    wg.State.SetPhaseState(phaseID, wg.State.Phases[phaseID].BeadID, PhaseStatusPending)
    wg.progress.SaveState()
    wg.progress.ReportProgress()
}
```

### 2. Implement per-phase cancellation

Each speculative goroutine needs its own cancellable context so that rollback can stop it mid-execution:

```go
// speculativeCancels maps phase IDs to their cancel functions.
// Added to WorkerGroup struct.
type WorkerGroup struct {
    // ... existing fields ...
    speculativeCancels map[string]context.CancelFunc
}
```

In `dispatchSpeculative`, create a derived context:

```go
specCtx, cancel := context.WithCancel(ctx)
wg.speculativeCancels[cand.PhaseID] = cancel

go func(phaseID string) {
    defer func() {
        <-sem
        completionCh <- phaseID
    }()
    wg.executePhase(specCtx, phaseID, trackID)  // uses cancellable context
}(cand.PhaseID)
```

The cancel method:

```go
// cancelSpeculativePhase cancels the context of a running speculative phase.
// Must be called with wg.mu held.
func (wg *WorkerGroup) cancelSpeculativePhase(phaseID string) {
    if cancel, ok := wg.speculativeCancels[phaseID]; ok {
        cancel()
        delete(wg.speculativeCancels, phaseID)
    }
}
```

### 3. Implement git rollback

```go
// revertSpeculativeChanges resets the git working tree for a speculative phase
// back to the base commit SHA. This uses `git checkout <sha> -- .` followed by
// a commit that records the revert, preserving history.
//
// Must NOT be called with wg.mu held (performs I/O).
func (wg *WorkerGroup) revertSpeculativeChanges(ctx context.Context, phaseID, baseCommitSHA string) error {
    // First, check if there are any changes to revert by comparing HEAD to base.
    diffCmd := exec.CommandContext(ctx, "git", "diff", "--quiet", baseCommitSHA, "HEAD")
    diffCmd.Dir = wg.Nebula.Manifest.Context.WorkingDir
    if diffCmd.Run() == nil {
        // No diff — nothing to revert.
        return nil
    }

    // Restore working tree to base state.
    restoreCmd := exec.CommandContext(ctx, "git", "checkout", baseCommitSHA, "--", ".")
    restoreCmd.Dir = wg.Nebula.Manifest.Context.WorkingDir
    if out, err := restoreCmd.CombinedOutput(); err != nil {
        return fmt.Errorf("git checkout failed: %s: %w", string(out), err)
    }

    // Stage and commit the revert.
    addCmd := exec.CommandContext(ctx, "git", "add", "-A")
    addCmd.Dir = wg.Nebula.Manifest.Context.WorkingDir
    if out, err := addCmd.CombinedOutput(); err != nil {
        return fmt.Errorf("git add failed: %s: %w", string(out), err)
    }

    commitMsg := fmt.Sprintf("revert speculative work for phase %q (dependency review failed)", phaseID)
    commitCmd := exec.CommandContext(ctx, "git", "commit", "--allow-empty", "-m", commitMsg)
    commitCmd.Dir = wg.Nebula.Manifest.Context.WorkingDir
    if out, err := commitCmd.CombinedOutput(); err != nil {
        return fmt.Errorf("git commit failed: %s: %w", string(out), err)
    }

    return nil
}
```

### 4. Clean up fabric state

```go
// cleanupSpeculativeFabric removes all fabric artifacts from a discarded
// speculative phase: entanglements, file claims, discoveries, and phase state.
//
// Must be called with wg.mu held (releases temporarily for I/O).
func (wg *WorkerGroup) cleanupSpeculativeFabric(ctx context.Context, phaseID string) {
    if wg.Fabric == nil {
        return
    }

    wg.mu.Unlock()
    defer wg.mu.Lock()

    // Release file claims.
    if err := wg.Fabric.ReleaseClaims(ctx, phaseID); err != nil {
        fmt.Fprintf(wg.logger(), "warning: failed to release claims for speculative phase %q: %v\n", phaseID, err)
    }

    // Reset phase state to QUEUED.
    if err := wg.Fabric.SetPhaseState(ctx, phaseID, fabric.StateQueued); err != nil {
        fmt.Fprintf(wg.logger(), "warning: failed to reset fabric state for speculative phase %q: %v\n", phaseID, err)
    }
}
```

### 5. Handle the race between completion and discard

A speculative phase might complete (successfully or with failure) before `resolveSpeculativeOutcomes` runs. In `recordResult`, check if the phase is speculative and if so, defer the done/failed marking — store the result in a "pending speculative results" map instead of immediately marking done:

```go
// In recordResult, before marking done:
if wg.tracker.IsSpeculative(phaseID) {
    // Store result but don't mark done yet — wait for dependency resolution.
    wg.pendingSpecResults[phaseID] = &pendingSpecResult{
        result:      wr,
        phaseResult: phaseResult,
        err:         err,
    }
    delete(inFlight, phaseID)
    return
}
```

Then in `resolveSpeculativeOutcomes`, when confirming a speculative phase that has a pending result, apply the stored result:

```go
if pending, ok := wg.pendingSpecResults[phaseID]; ok {
    // Apply the stored result as if the phase completed normally.
    wg.applyPendingResult(phaseID, pending)
    delete(wg.pendingSpecResults, phaseID)
}
```

### 6. Metrics tracking

Record speculative execution statistics in `Metrics`:

```go
// Add to Metrics struct:
type Metrics struct {
    // ... existing fields ...
    SpeculativeDispatched int           // phases dispatched speculatively
    SpeculativeConfirmed  int           // phases whose speculation was correct
    SpeculativeDiscarded  int           // phases whose work was rolled back
    SpeculativeSavedTime  time.Duration // estimated time saved by correct speculation
}
```

## Files

- `internal/nebula/speculative_rollback.go` — New file: `discardSpeculativePhase`, `revertSpeculativeChanges`, `cleanupSpeculativeFabric`, `cancelSpeculativePhase`
- `internal/nebula/worker.go` — Add `speculativeCancels` map and `pendingSpecResults` map to `WorkerGroup`; initialize in `Run()`
- `internal/nebula/worker_exec.go` — Update `recordResult` to defer results for speculative phases; update `executePhase` to use cancellable context
- `internal/nebula/tracker.go` — Ensure `DiscardSpeculative` correctly clears all speculative tracking state
- `internal/nebula/types.go` — Add speculative fields to `Metrics` struct
- `internal/nebula/speculative_rollback_test.go` — Tests for rollback logic

## Acceptance Criteria

- [ ] `discardSpeculativePhase` cancels in-flight context, reverts git, cleans fabric, returns phase to pending
- [ ] `revertSpeculativeChanges` restores working tree to `BaseCommitSHA` with a revert commit
- [ ] `cleanupSpeculativeFabric` releases claims and resets phase state to `StateQueued`
- [ ] `cancelSpeculativePhase` calls the cancel function and removes it from the map
- [ ] Speculative results are deferred in `recordResult` until dependency resolution
- [ ] Confirmed speculative phases have their pending results applied normally
- [ ] Discarded speculative phases return to `PhaseStatusPending` and are re-eligible
- [ ] Race condition handled: speculative phase completes before/after dependency review
- [ ] `Metrics` tracks `SpeculativeDispatched`, `SpeculativeConfirmed`, `SpeculativeDiscarded`
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] `go test ./internal/nebula/...` passes with rollback tests
- [ ] Git rollback produces a clean revert commit with descriptive message
- [ ] No speculative artifacts (entanglements, claims) survive a discard
