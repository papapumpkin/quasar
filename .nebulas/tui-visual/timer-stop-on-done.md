+++
id = "timer-stop-on-done"
title = "Stop elapsed timer when nebula completes"
type = "bug"
priority = 1
depends_on = []
+++

## Problem

The elapsed timer in the status bar keeps ticking after a nebula finishes (all phases reach done or failed). The timer should freeze at the final elapsed time so the user can see how long the run took.

## Current State

**Timer mechanism**:
- `model.go` stores `StartTime time.Time` and the status bar renders elapsed via `time.Since(s.StartTime)`
- `tickCmd()` fires `MsgTick` every second, which schedules the next tick unconditionally
- `MsgNebulaDone` sets `m.Done = true` and `m.DoneErr = msg.Err`
- But the tick keeps firing and `time.Since(s.StartTime)` keeps growing

**Status bar** (`statusbar.go`):
- `View()` always computes `elapsed := time.Since(s.StartTime).Truncate(time.Second)` if `StartTime` is non-zero
- There's no concept of a frozen/final elapsed time

## Solution

### 1. Freeze Elapsed Time

Add a `FinalElapsed time.Duration` field to `StatusBar`. When it's non-zero, use it instead of `time.Since(s.StartTime)`:

```go
// In StatusBar.View():
var elapsed time.Duration
if s.FinalElapsed > 0 {
    elapsed = s.FinalElapsed
} else if !s.StartTime.IsZero() {
    elapsed = time.Since(s.StartTime).Truncate(time.Second)
}
```

### 2. Set FinalElapsed on Done

In the `MsgNebulaDone` and `MsgLoopDone` handlers in `model.go`, compute and freeze the elapsed time:

```go
case MsgNebulaDone:
    m.Done = true
    m.DoneErr = msg.Err
    m.StatusBar.FinalElapsed = time.Since(m.StartTime).Truncate(time.Second)
```

### 3. Stop Tick on Done

Optionally, stop scheduling ticks when `m.Done` is true to avoid unnecessary redraws:

```go
case MsgTick:
    if !m.Done {
        cmds = append(cmds, tickCmd())
    }
```

## Files to Modify

- `internal/tui/statusbar.go` — Add `FinalElapsed time.Duration`; use it in `View()` when non-zero
- `internal/tui/model.go` — Set `FinalElapsed` on `MsgNebulaDone`/`MsgLoopDone`; stop ticks when done

## Acceptance Criteria

- [ ] Timer freezes at final value when nebula completes (all phases done/failed)
- [ ] Timer freezes when loop completes
- [ ] Frozen timer value accurately reflects total run duration
- [ ] No more tick scheduling after completion (no unnecessary redraws)
- [ ] `go build` and `go test ./internal/tui/...` pass
