+++
id = "nebula-resume"
title = "Add checkpoint-aware resume logic to WorkerGroup"
type = "feature"
priority = 2
depends_on = ["loop-resume"]
labels = ["quasar", "checkpoint", "reliability"]
+++

## Problem

The nebula `WorkerGroup` (in `internal/nebula/worker.go`) dispatches phases to workers but has no awareness of checkpoints. When `--resume` is used, the worker group needs to detect existing checkpoints for in-progress phases and route them through `Loop.RunFromCheckpoint` instead of starting fresh loops.

## Solution

### WorkerGroup changes

Add a `ResumeEnabled` field to `WorkerGroup`:

```go
type WorkerGroup struct {
    // ... existing fields ...
    ResumeEnabled bool   // when true, check for checkpoints before starting phases
    CheckpointDir string // directory containing checkpoint files (typically the nebula dir)
}
```

When `ResumeEnabled` is true, before dispatching a phase to a worker, `WorkerGroup.Run` (or its internal dispatch logic) calls `checkpoint.Load(wg.CheckpointDir, phase.ID)`. If a checkpoint is found:

1. Call `checkpoint.Validate(cp, currentGitSHA)` to verify safety.
2. If valid, the worker's `Runner` receives the checkpoint and calls `Loop.RunFromCheckpoint` instead of `Loop.RunTask`/`Loop.RunExistingTask`.
3. If validation fails (e.g., SHA mismatch), log a warning and fall back to a fresh start. The stale checkpoint is removed.

### Runner interface extension

The `Runner` interface used by `WorkerGroup` (check exact definition in the worker file or the adapters in `cmd/nebula_apply.go`) needs to support resume. Add a method:

```go
type Runner interface {
    // ... existing methods ...
    RunFromCheckpoint(ctx context.Context, cp *checkpoint.Checkpoint, spec PhaseSpec, state *PhaseState) (*WorkerResult, error)
}
```

Implement this in both `loopAdapter` and `tuiLoopAdapter` in `cmd/nebula_apply.go`. Both adapters call `loop.RunFromCheckpoint` internally.

### CheckpointDir propagation

Set `Loop.CheckpointDir` to the nebula directory so that the `CheckpointHook` writes files alongside `nebula.state.toml`. This keeps all state for a nebula run in one directory.

### Phase state integration

When a checkpoint is loaded for a phase whose `PhaseState.Status` is `PhaseStatusInProgress` (from `internal/nebula/types.go`), the phase is a candidate for resume. If status is `PhaseStatusDone` or `PhaseStatusFailed`, the checkpoint is stale and should be removed.

After successful resume completion, update `PhaseState.Status` to `PhaseStatusDone` and call `nebula.SaveState` as normal.

## Files

- `internal/nebula/worker.go` -- add `ResumeEnabled`, `CheckpointDir` fields; checkpoint detection in dispatch logic
- `internal/nebula/runner.go` (or wherever the `Runner` interface is defined) -- add `RunFromCheckpoint` to interface
- `cmd/nebula_apply.go` -- implement `RunFromCheckpoint` on `loopAdapter` and `tuiLoopAdapter`; set `CheckpointDir` on `Loop` instances
- `internal/nebula/worker_test.go` -- test resume path: mock runner receives checkpoint, verify fallback on validation failure

## Acceptance Criteria

- [ ] `WorkerGroup` detects checkpoint files when `ResumeEnabled` is true
- [ ] Valid checkpoints route through `RunFromCheckpoint` on the `Runner`
- [ ] Invalid checkpoints log a warning, get removed, and fall back to fresh start
- [ ] Stale checkpoints (phase already done/failed) are cleaned up
- [ ] `loopAdapter` and `tuiLoopAdapter` implement `RunFromCheckpoint`
- [ ] `Loop.CheckpointDir` is set to the nebula directory
- [ ] Normal (non-resume) execution path is unaffected when `ResumeEnabled` is false
- [ ] Phase state transitions work correctly on resumed phases (status goes to `done` on success)
