+++
id = "loop-resume"
title = "Add resume capability to Loop.RunExistingTask"
type = "feature"
priority = 2
depends_on = ["checkpoint-writer", "checkpoint-loader"]
labels = ["quasar", "checkpoint", "reliability"]
+++

## Problem

The `Loop` struct (in `internal/loop/loop.go`) always starts a fresh coder-reviewer loop via `runLoop`. After a crash, even if a checkpoint exists, the loop starts over from cycle 1 with a blank `CycleState`. We need `Loop` to accept a pre-populated `CycleState` from a checkpoint and resume the loop from the interrupted cycle.

## Solution

### New method on Loop

Add a `RunFromCheckpoint` method to `Loop`:

```go
// RunFromCheckpoint resumes a coder-reviewer loop from a previously saved
// checkpoint. It reconstructs CycleState from the checkpoint, validates
// it, and enters runLoop at the appropriate cycle.
func (l *Loop) RunFromCheckpoint(ctx context.Context, cp *checkpoint.Checkpoint) (*TaskResult, error)
```

This method:

1. Calls `cp.ToCycleState()` to reconstruct the `CycleState`.
2. Validates the checkpoint using `checkpoint.Validate(cp, currentSHA)`.
3. Sets `cs.Cycle` to `cp.Cycle` (resume from the cycle that was in progress or the next one, depending on `cp.Phase`).
4. If `cp.Phase` is `PhaseReviewComplete` or `PhaseApproved`, increments `cs.Cycle` to start the next cycle (the completed cycle's results are already captured).
5. If `cp.Phase` is `PhaseCoding` or `PhaseReviewing`, restarts the current cycle (agent output may be incomplete).
6. Calls the existing `runLoop` method with the reconstructed state.
7. On success, calls `checkpoint.Remove` to clean up.

### Wire CheckpointHook into Loop

Add a `CheckpointDir` field to the `Loop` struct:

```go
type Loop struct {
    // ... existing fields ...
    CheckpointDir string // directory for checkpoint files (empty = no checkpointing)
}
```

When `CheckpointDir` is non-empty, `RunTask` and `RunExistingTask` prepend a `CheckpointHook` to `l.Hooks` before entering the loop. This ensures checkpoints are written automatically without requiring callers to manually configure the hook.

### Emit event on resume

Add a new `EventKind` constant in `internal/loop/hooks.go`:

```go
EventResumed EventKind = iota // after existing constants
```

`RunFromCheckpoint` emits `EventResumed` with `Cycle` set to the resumed cycle number and `Message` set to `"resumed from checkpoint"` before entering `runLoop`. This allows the TUI and other hooks to display a resume indicator.

## Files

- `internal/loop/loop.go` -- add `CheckpointDir` field, `RunFromCheckpoint` method, auto-wire `CheckpointHook` in `RunTask`/`RunExistingTask`
- `internal/loop/hooks.go` -- add `EventResumed` constant
- `internal/loop/loop_test.go` -- test `RunFromCheckpoint` with a mock invoker: verify it resumes at the correct cycle, verify cleanup on success

## Acceptance Criteria

- [ ] `RunFromCheckpoint` reconstructs `CycleState` from checkpoint and enters `runLoop`
- [ ] Resume from `PhaseReviewComplete` starts the next cycle (cycle N+1)
- [ ] Resume from `PhaseCoding` restarts the same cycle (agent output discarded)
- [ ] `EventResumed` is emitted before the loop begins
- [ ] `CheckpointHook` is auto-wired when `CheckpointDir` is set
- [ ] Checkpoint file is removed after successful task completion
- [ ] `RunFromCheckpoint` returns error if `checkpoint.Validate` fails
- [ ] No behavior change when `CheckpointDir` is empty (existing code path unaffected)
