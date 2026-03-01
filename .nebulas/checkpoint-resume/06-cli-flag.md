+++
id = "cli-flag"
title = "Add --resume flag to `quasar nebula apply`"
type = "feature"
priority = 3
depends_on = ["nebula-resume"]
labels = ["quasar", "checkpoint", "reliability"]
+++

## Problem

The checkpoint infrastructure exists but there is no way for users to trigger resume behavior. The `quasar nebula apply` command (in `cmd/nebula_apply.go`) needs a `--resume` flag that activates checkpoint detection and resume logic.

## Solution

### Flag registration

In `addNebulaApplyFlags` (defined at `cmd/nebula_apply.go` line 27), add:

```go
cmd.Flags().Bool("resume", false, "resume from checkpoints if available (with --auto)")
```

### Flag handling in runNebulaApply

In `runNebulaApply` (at `cmd/nebula_apply.go` line 36), after the `--auto` flag check:

```go
resume, _ := cmd.Flags().GetBool("resume")
```

When `resume` is true and `--auto` is also true:

1. Set `wg.ResumeEnabled = true` and `wg.CheckpointDir = dir` (the nebula directory) on the `WorkerGroup`.
2. Always set `Loop.CheckpointDir = dir` on both `loopAdapter` and `tuiLoopAdapter` so that checkpoint files are written during execution regardless of `--resume` (checkpoints are always written; `--resume` controls whether they are consumed on startup).

When `--resume` is used without `--auto`, print a warning: `"--resume requires --auto; ignoring"`.

### Checkpoint status in UI

Add a brief status line via `ui.Printer` when resume is active:

```go
printer.Info(fmt.Sprintf("resume mode: found %d checkpoint(s)", len(checkpoints)))
```

For the TUI path, emit a similar message through the TUI program so the splash or status bar reflects that phases are being resumed.

### Checkpoint cleanup on completion

After `wg.Run` completes successfully (no error), remove all remaining checkpoint files in the nebula directory. This prevents stale checkpoints from affecting future runs. Use `checkpoint.LoadAll` + `checkpoint.Remove` in a cleanup loop.

### Always-write behavior

Checkpoint files should always be written during `--auto` execution (not just when `--resume` is passed). This ensures that if a run crashes, the next invocation with `--resume` has checkpoints to work with. The `--resume` flag only controls whether existing checkpoints are loaded on startup.

To enable always-write: when `--auto` is true, always set `Loop.CheckpointDir = dir` regardless of `--resume`.

## Files

- `cmd/nebula_apply.go` -- add `--resume` flag in `addNebulaApplyFlags`, handle in `runNebulaApply`, set `ResumeEnabled`/`CheckpointDir` on `WorkerGroup`, set `CheckpointDir` on `Loop`, cleanup on completion
- `internal/ui/printer.go` -- no changes needed (existing `Info` method suffices)

## Acceptance Criteria

- [ ] `--resume` flag is registered on the `nebula apply` command
- [ ] `--resume` with `--auto` sets `ResumeEnabled = true` on `WorkerGroup`
- [ ] `--resume` without `--auto` prints a warning and is ignored
- [ ] Checkpoint files are always written during `--auto` execution (not gated on `--resume`)
- [ ] `Loop.CheckpointDir` is set to the nebula directory for both `loopAdapter` and `tuiLoopAdapter`
- [ ] On successful completion, all checkpoint files in the nebula directory are removed
- [ ] Resume status is displayed in both stderr and TUI output paths
- [ ] Existing `--auto` behavior is unchanged when `--resume` is not passed
