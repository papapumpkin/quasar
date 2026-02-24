+++
id = "phase-timer-cumulative"
title = "Fix phase elapsed timers showing cumulative time instead of actual phase duration"
type = "bug"
priority = 1
depends_on = []
labels = ["quasar", "tui"]
scope = ["internal/tui/nebulaview.go", "internal/tui/loopview.go"]
allow_scope_overlap = true
+++

## Problem

Phase elapsed timers keep counting after a phase completes. A phase that took 2 minutes to finish will display "5m32s" when viewed 3 minutes later, because the timer shows `time.Since(StartedAt)` — the time from when the phase started until **now**, not until when it finished.

### Root Cause

`PhaseEntry` in `nebulaview.go:42` has a `StartedAt time.Time` field but no corresponding `CompletedAt` or `FinalDuration` field. When a phase finishes (transitions to `PhaseDone`, `PhaseFailed`, or `PhaseSkipped`), the start time is preserved but no completion timestamp is recorded.

`phaseDetail()` at `nebulaview.go:318` calls `formatElapsed(p.StartedAt)` for both `PhaseDone` and `PhaseWorking`:

```go
case PhaseDone:
    elapsed := formatElapsed(p.StartedAt)  // ← keeps growing forever
```

And `formatElapsed()` at `loopview.go:351` computes:

```go
func formatElapsed(start time.Time) string {
    // ...
    d := time.Since(start).Truncate(time.Second)  // ← wall-clock now, not completion time
```

So for completed phases, the displayed time is `now - startedAt` instead of `completedAt - startedAt`.

## Solution

Add a `CompletedAt time.Time` field to `PhaseEntry`, set it on terminal transitions, and use it in `phaseDetail()`.

### Changes

1. **`internal/tui/nebulaview.go`** — Add `CompletedAt` field and set it on completion:
   ```go
   type PhaseEntry struct {
       // ... existing fields ...
       StartedAt   time.Time
       CompletedAt time.Time // set when phase reaches a terminal state
   }
   ```

   In `SetPhaseStatus()`, record the completion timestamp:
   ```go
   if status == PhaseWorking && nv.Phases[i].StartedAt.IsZero() {
       nv.Phases[i].StartedAt = time.Now()
   }
   if status == PhaseDone || status == PhaseFailed || status == PhaseSkipped {
       if nv.Phases[i].CompletedAt.IsZero() {
           nv.Phases[i].CompletedAt = time.Now()
       }
   }
   ```

2. **`internal/tui/nebulaview.go`** — Update `phaseDetail()` to use `CompletedAt` for done phases:
   ```go
   case PhaseDone:
       elapsed := formatDuration(p.StartedAt, p.CompletedAt)
   ```

3. **`internal/tui/loopview.go`** — Add a `formatDuration(start, end time.Time)` helper alongside the existing `formatElapsed`:
   ```go
   // formatDuration returns a human-readable duration between start and end.
   // If end is zero, falls back to time.Since(start).
   func formatDuration(start, end time.Time) string {
       if start.IsZero() {
           return ""
       }
       var d time.Duration
       if end.IsZero() {
           d = time.Since(start)
       } else {
           d = end.Sub(start)
       }
       d = d.Truncate(time.Second)
       if d < time.Minute {
           return fmt.Sprintf("%ds", int(d.Seconds()))
       }
       m := int(d.Minutes())
       s := int(d.Seconds()) % 60
       return fmt.Sprintf("%dm%02ds", m, s)
   }
   ```

## Files

- `internal/tui/nebulaview.go` — Add `CompletedAt` field to `PhaseEntry`, set it in `SetPhaseStatus()`, use it in `phaseDetail()`
- `internal/tui/loopview.go` — Add `formatDuration(start, end)` helper

## Acceptance Criteria

- [ ] Completed phases display the time they took to run, not time since they started
- [ ] In-progress phases still show a live-updating elapsed timer
- [ ] Failed and skipped phases also freeze their timer at completion
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] All existing tests pass (`go test ./internal/tui/...`)
