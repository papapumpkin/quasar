+++
id = "checkpoint-writer"
title = "Implement atomic checkpoint writer with Hook integration"
type = "feature"
priority = 1
depends_on = ["checkpoint-format"]
labels = ["quasar", "checkpoint", "reliability"]
+++

## Problem

The `Checkpoint` struct from the previous phase exists but nothing writes it. We need a writer that persists checkpoints at significant state transitions and integrates with the existing `loop.Hook` interface (defined in `internal/loop/hooks.go`) so checkpoints are written automatically during the coder-reviewer loop.

## Solution

### Writer

Add `Save` and checkpoint path helpers to `internal/checkpoint/checkpoint.go`:

```go
// Save atomically writes the checkpoint to the given directory.
// The file is named checkpoint.<phaseID>.toml (or checkpoint.toml if phaseID is empty).
func Save(dir string, cp *Checkpoint) error
```

Use the same write-tmp-then-rename pattern from `nebula.SaveState` (`internal/nebula/state.go` lines 60-79): marshal to TOML, write to `path + ".tmp"`, then `os.Rename`. This ensures the checkpoint file is never partially written.

```go
// CheckpointPath returns the path to the checkpoint file for the given phase.
func CheckpointPath(dir, phaseID string) string
```

### Hook

Create `internal/checkpoint/hook.go` implementing the `loop.Hook` interface:

```go
// CheckpointHook writes a checkpoint file on significant loop events.
// It satisfies the loop.Hook interface.
type CheckpointHook struct {
    Dir        string // directory to write checkpoint files
    PhaseID    string // nebula phase ID (may be empty for standalone)
    NebulaName string // nebula name (may be empty for standalone)
    GitSHAFunc func() string // returns current HEAD SHA
}

func (h *CheckpointHook) OnEvent(ctx context.Context, event loop.Event)
```

The hook writes a checkpoint on these `EventKind` values (from `internal/loop/hooks.go`):

- `EventReviewComplete` -- a full cycle has completed, checkpoint captures cycle results
- `EventTaskSuccess` -- task finished successfully, write a final checkpoint
- `EventTaskFailed` -- task failed, write a checkpoint so resume can skip or retry

On `EventCycleStart` and `EventAgentDone`, no checkpoint is written (these are too frequent and the state is incomplete).

The hook calls `FromCycleState` using the `CycleState` accessible from the event context, then `Save`. Errors are logged to stderr but do not halt the loop (checkpoint failure is non-fatal).

### Git SHA helper

Add a small helper in the checkpoint package:

```go
// CurrentGitSHA returns HEAD's SHA in the given working directory.
func CurrentGitSHA(ctx context.Context, workDir string) (string, error)
```

This runs `git rev-parse HEAD` via `exec.CommandContext`.

## Files

- `internal/checkpoint/checkpoint.go` -- add `Save`, `CheckpointPath`, `CurrentGitSHA`
- `internal/checkpoint/hook.go` -- `CheckpointHook` struct implementing `loop.Hook`
- `internal/checkpoint/hook_test.go` -- verify hook writes checkpoint on `EventReviewComplete` and `EventTaskSuccess`, skips on `EventCycleStart`

## Acceptance Criteria

- [ ] `Save` writes a valid TOML file that can be loaded back with `Load` (next phase)
- [ ] Write is atomic: uses tmp file + rename, matching `nebula.SaveState` pattern
- [ ] `CheckpointHook` implements `loop.Hook` interface
- [ ] Hook writes checkpoint on `EventReviewComplete`, `EventTaskSuccess`, `EventTaskFailed`
- [ ] Hook does not write on `EventCycleStart` or `EventAgentDone`
- [ ] Checkpoint write errors are logged to stderr, not returned (non-fatal)
- [ ] `CurrentGitSHA` uses `exec.CommandContext` for cancellation support
- [ ] `CheckpointPath` returns `checkpoint.<phaseID>.toml` when phaseID is non-empty, `checkpoint.toml` otherwise
