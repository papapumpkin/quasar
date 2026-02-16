+++
id = "phase-change-pipeline"
title = "Wire watcher change events to worker for in-flight phase modifications"
type = "feature"
priority = 1
depends_on = ["detail-scroll"]
+++

## Problem

The `Watcher` already detects `ChangeModified` and `ChangeAdded` events for `.md` phase files and emits them on `Watcher.Changes`, but `WorkerGroup` never reads from this channel. The change events are silently dropped. We need to wire them into the worker execution loop so the system can react to phase file edits and additions during a live run.

## Current State

**Watcher** (`internal/nebula/watcher.go`):
- `Watcher.Changes <-chan Change` — emits `Change{Kind, PhaseID, File}`
- `ChangeKind`: `ChangeModified`, `ChangeRemoved`, `ChangeAdded`
- `emitChange()` parses the modified `.md` file to extract `PhaseID`
- Debounces at 100ms to avoid duplicate events

**WorkerGroup** (`internal/nebula/worker.go`):
- `Watcher *Watcher` field — already wired but only `Watcher.Interventions` is consumed
- `Run()` method dispatches phases to workers based on the resolved DAG
- `executePhase()` runs a single phase through `PhaseRunner.RunExistingPhase()`
- No mechanism to pause a running phase's loop or inject context mid-execution

**Loop** (`internal/loop/loop.go`):
- Each cycle: `runCoderPhase()` → `runReviewerPhase()` → check approval → next cycle
- No concept of external context injection between cycles

## Solution

### 1. Consume Watcher.Changes in WorkerGroup

Add a change consumer goroutine in `WorkerGroup.Run()` that reads from `Watcher.Changes` and dispatches to the appropriate handler:

```go
go func() {
    for change := range wg.Watcher.Changes {
        switch change.Kind {
        case ChangeModified:
            wg.handlePhaseModified(change)
        case ChangeAdded:
            wg.handlePhaseAdded(change)
        case ChangeRemoved:
            // Log warning, don't remove running phases
        }
    }
}()
```

### 2. Phase Modified → Graceful Refactor Signal

When an in-progress phase's `.md` file is modified:
1. Re-parse the phase file to get the updated body/description
2. Store the updated content in a `pendingRefactors map[string]string` (phaseID → new body)
3. Signal the phase's loop to pick up the new content after the current cycle completes

This requires a signaling mechanism from the worker to the loop. Options:
- **Channel-based**: Add a `RefactorCh <-chan string` field to the loop that carries new context
- **Context value**: Inject a refactor signal into the phase's context
- **Callback**: Add a `OnCycleComplete func() *RefactorContext` callback to Loop

The channel approach is cleanest — the loop checks after each reviewer cycle:
```go
select {
case newBody := <-l.RefactorCh:
    // Update the task description, mark as refactored
    state.TaskDescription = newBody
    state.Refactored = true
    // Continue to next coder cycle with updated context
default:
    // No refactor pending, continue normally
}
```

### 3. Send Refactor Signal to Running Phase

`WorkerGroup.handlePhaseModified()` looks up the phase's active loop (needs a registry of running phase loops) and sends the new body on its refactor channel.

Add a `phaseLoops map[string]*phaseLoopHandle` to track running phases:
```go
type phaseLoopHandle struct {
    RefactorCh chan<- string
    Cancel     context.CancelFunc
}
```

### 4. TUI Notification

Send a message to the TUI when a phase refactor is detected:
```go
type MsgPhaseRefactorPending struct {
    PhaseID string
}
type MsgPhaseRefactorApplied struct {
    PhaseID string
}
```

The TUI can show a visual indicator (e.g., a ⟳ icon next to the phase) that a refactor is pending, then clear it when applied.

## Files to Modify

- `internal/nebula/worker.go` — Consume `Watcher.Changes`; add `phaseLoops` registry; `handlePhaseModified()`, `handlePhaseAdded()` stubs
- `internal/loop/loop.go` — Add `RefactorCh <-chan string` field; check after each cycle
- `internal/loop/state.go` — Add `Refactored bool` and `OriginalDescription string` to `CycleState`
- `internal/tui/msg.go` — Add `MsgPhaseRefactorPending`, `MsgPhaseRefactorApplied`

## Acceptance Criteria

- [ ] `WorkerGroup` consumes `Watcher.Changes` when watcher is non-nil
- [ ] Modified in-progress phase stores updated body in pending map
- [ ] Refactor signal channel is wired between worker and loop
- [ ] Loop checks refactor channel after each cycle completes
- [ ] TUI receives notification messages for pending/applied refactors
- [ ] No running agent is interrupted — current cycle always completes
- [ ] `go build` and `go test ./...` pass
