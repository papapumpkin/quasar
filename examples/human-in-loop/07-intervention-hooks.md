+++
id = "intervention-hooks"
title = "Support PAUSE and STOP intervention files"
type = "feature"
priority = 2
depends_on = ["rename-task-to-phase"]
+++

## Problem

Once a nebula starts, the human has no way to intervene except killing the process. There is no graceful mechanism to pause execution, stop after the current phase, or signal that something needs attention.

## Solution

Extend the existing `Watcher` to detect special intervention files dropped into the nebula directory. This is low-tech but powerful — works over SSH, requires no TUI, and builds on infrastructure that already exists.

### Intervention Files

| File | Behavior |
|------|----------|
| `PAUSE` | Finish the current phase, then wait for the file to be removed before continuing |
| `STOP` | Finish the current phase, then stop the nebula gracefully (save state) |

### Watcher Changes

The `Watcher` already watches the nebula directory for file changes. Extend it to recognize `PAUSE` and `STOP` as special files:

```go
type InterventionKind string

const (
    InterventionPause InterventionKind = "pause"
    InterventionStop  InterventionKind = "stop"
)
```

Add a new channel or field to `Watcher`:
```go
Interventions <-chan InterventionKind
```

When `PAUSE` or `STOP` is created in the nebula directory, emit the corresponding intervention on this channel. When `PAUSE` is deleted, emit a resume signal (or use a separate mechanism).

### Worker Integration

In the `WorkerGroup` main loop, check for interventions between phase dispatches:

- **PAUSE**: After current phase(s) complete, print a message ("Nebula paused. Remove PAUSE file to continue.") and block until the `PAUSE` file is removed. The `Watcher` detects the removal.
- **STOP**: After current phase(s) complete, save state and exit `Run` gracefully. Return a distinguishable error or status so the caller knows it was a manual stop, not a failure.

### UX

When paused, print to stderr:
```
── Nebula paused ──────────────────────────────────
   Remove the PAUSE file to continue:
   rm examples/cicd-nebula/PAUSE
───────────────────────────────────────────────────
```

When stopped:
```
── Nebula stopped by user ─────────────────────────
   State saved. Resume with: quasar nebula apply
───────────────────────────────────────────────────
```

### Cleanup

Intervention files should be cleaned up:
- `STOP` is deleted after the nebula stops
- `PAUSE` is left for the user to remove (that's the resume signal)
- Neither file should be committed to git (add to `.gitignore` pattern in commit logic)

## Files to Modify

- `internal/nebula/watcher.go` — Add intervention detection and channel
- `internal/nebula/watcher_test.go` — Test intervention file detection
- `internal/nebula/worker.go` — Check interventions between phases
- `internal/nebula/git.go` — Exclude PAUSE/STOP from commits

## Acceptance Criteria

- [ ] `Watcher` detects `PAUSE` and `STOP` files in the nebula directory
- [ ] `PAUSE` blocks execution after current phase, resumes on file removal
- [ ] `STOP` halts execution gracefully after current phase, saves state
- [ ] Clear stderr messages tell the user what happened and how to continue
- [ ] `STOP` file is cleaned up after nebula stops
- [ ] `PAUSE`/`STOP` files are excluded from git commits
- [ ] Works in non-interactive environments (SSH, CI)
- [ ] Tests verify intervention detection and worker response
- [ ] `go test ./internal/nebula/...` passes