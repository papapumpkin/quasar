+++
id = "watch-mode"
title = "Implement watch mode for non-blocking observation"
type = "feature"
priority = 3
depends_on = ["gate-mode-implementation", "live-progress-dashboard"]
+++

## Problem

The `review` and `approve` gate modes block execution waiting for human input. Sometimes the human wants to observe what's happening in real time and only intervene if something looks wrong — a "monitor" workflow rather than an "approve" workflow.

## Solution

Implement `watch` mode, which streams checkpoint diffs and dashboard updates in real time without blocking execution. The human watches the output and can intervene using the `PAUSE`/`STOP` files from phase 07 if needed.

### Behavior

When gate mode is `watch`:
1. After each phase completes, render the checkpoint diff to stderr (same as `review` mode)
2. Do NOT call `Gater.Prompt` — continue immediately to the next phase
3. The live dashboard continues updating throughout
4. The human can drop a `PAUSE` or `STOP` file to intervene

### Implementation

This is mostly already handled by the gate implementation in phase 05. The `watch` case in `executePhase`:

```go
case GateModeWatch:
    RenderCheckpoint(os.Stderr, checkpoint)
    // No prompt — continue immediately
```

The main work is ensuring the output is clean:
- Checkpoint diffs should be clearly delimited (start/end markers)
- Dashboard should resume rendering after the checkpoint block
- If multiple phases complete concurrently (parallel workers), their checkpoints should not interleave — use a mutex for output

### Scroll-Back Friendly

In `watch` mode, don't use ANSI cursor-up to overwrite the dashboard. Instead, print each checkpoint as an append-only log block so the human can scroll back through the full history:

```
── Phase complete: test-script-action ─────────────
   Status: done | $0.12 | 2 cycles
   +++ scripts/test.sh (15 lines)
   +++ .github/actions/test/action.yml (22 lines)
   "Clean implementation, follows POSIX conventions"
───────────────────────────────────────────────────

── Phase complete: vet-script-action ──────────────
   ...
```

### Config

No new config needed — `watch` is already a valid `GateMode` from phase 02. This phase implements the runtime behavior.

## Files to Modify

- `internal/nebula/worker.go` — Add `watch` case in gate logic (may already be stubbed)
- `internal/nebula/dashboard.go` — Add append-only mode for watch
- `internal/nebula/checkpoint.go` — Ensure checkpoint rendering works in append-only context

## Acceptance Criteria

- [ ] `watch` mode renders checkpoint diffs without blocking
- [ ] Phases continue executing immediately after checkpoint is printed
- [ ] Output is append-only (no cursor movement) for scroll-back review
- [ ] Concurrent phase completions don't interleave output (mutex-protected)
- [ ] Human can still intervene via PAUSE/STOP files
- [ ] Dashboard updates interleave cleanly with checkpoint blocks
- [ ] `go test ./internal/nebula/...` passes