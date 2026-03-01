+++
id = "tui-indicators"
title = "Surface speculative execution state in cockpit TUI"
type = "feature"
priority = 3
depends_on = ["worker-dispatch"]
scope = ["internal/tui/nebulaview.go", "internal/tui/msg.go", "internal/tui/model.go", "internal/tui/bridge.go", "internal/ui/nebula.go"]
+++

## Problem

When speculative pipelining is active, the operator sees phases begin executing before their dependencies are reviewed. Without visual distinction, this looks like a bug — "Why is Phase N+1 running when Phase N's review hasn't finished?" The TUI needs to clearly communicate which phases are speculative, what they're speculating on, and what happens when speculation succeeds or fails.

The current `PhaseStatus` enum in `internal/tui/nebulaview.go` has `PhaseWaiting`, `PhaseWorking`, `PhaseDone`, `PhaseFailed`, `PhaseGate`, and `PhaseSkipped`. There is no `PhaseSpeculative` status, no visual indicator for speculative work, and no messaging for speculation outcomes (confirmed/discarded).

The stderr printer in `internal/ui/nebula.go` also needs a speculative indicator for the non-TUI path.

## Solution

### 1. Add `PhaseSpeculative` status

In `internal/tui/nebulaview.go`, extend the `PhaseStatus` enum:

```go
const (
    PhaseWaiting     PhaseStatus = iota
    PhaseWorking
    PhaseSpeculative  // new: running ahead of confirmed dependency
    PhaseDone
    PhaseFailed
    PhaseGate
    PhaseSkipped
)
```

### 2. Add speculative visual styling

In the `phaseIconAndStyle` method of `NebulaView`, add the speculative case:

```go
case PhaseSpeculative:
    // Dashed spinner or lightning bolt icon with amber/yellow color.
    // The icon signals "tentative" — not yet confirmed.
    icon = "⚡"  // or use the spinner with a different style
    style = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))  // amber
```

In `renderPhaseRow`, when a phase is speculative, append the dependency info:

```go
if entry.Status == PhaseSpeculative && entry.SpeculatesOn != "" {
    detail += fmt.Sprintf(" (speculates on %s)", entry.SpeculatesOn)
}
```

### 3. Extend `PhaseEntry` with speculative metadata

```go
type PhaseEntry struct {
    ID           string
    Title        string
    Status       PhaseStatus
    Wave         int
    CostUSD      float64
    Cycles       int
    MaxCycles    int
    BlockedBy    []string
    DependsOn    []string
    StartedAt    time.Time
    CompletedAt  time.Time
    PlanBody     string
    Refactored   bool
    SpeculatesOn string  // new: phase ID this phase speculates past (empty = not speculative)
}
```

### 4. Add speculative TUI messages

In `internal/tui/msg.go`, add new message types:

```go
// MsgPhaseSpeculativeStarted is sent when a phase begins speculative execution.
type MsgPhaseSpeculativeStarted struct {
    PhaseID      string
    SpeculatesOn string  // the dependency phase being speculated past
}

// MsgPhaseSpeculativeConfirmed is sent when a speculative phase's dependency
// review passes and the phase is promoted to confirmed.
type MsgPhaseSpeculativeConfirmed struct {
    PhaseID string
}

// MsgPhaseSpeculativeDiscarded is sent when a speculative phase's dependency
// review fails and the speculative work is being rolled back.
type MsgPhaseSpeculativeDiscarded struct {
    PhaseID string
    Reason  string  // human-readable reason (e.g., "dependency review-rejected rejected")
}
```

### 5. Handle speculative messages in `AppModel.Update()`

In `internal/tui/model.go`, add cases for the new messages:

```go
case MsgPhaseSpeculativeStarted:
    m.NebulaView.SetPhaseStatus(msg.PhaseID, PhaseSpeculative)
    m.NebulaView.SetSpeculatesOn(msg.PhaseID, msg.SpeculatesOn)
    // Show a toast notification.
    m.addToast(fmt.Sprintf("Speculative: %s (on %s)", msg.PhaseID, msg.SpeculatesOn), toastInfo)

case MsgPhaseSpeculativeConfirmed:
    m.NebulaView.SetPhaseStatus(msg.PhaseID, PhaseWorking)
    m.NebulaView.SetSpeculatesOn(msg.PhaseID, "")
    m.addToast(fmt.Sprintf("Confirmed: %s", msg.PhaseID), toastSuccess)

case MsgPhaseSpeculativeDiscarded:
    m.NebulaView.SetPhaseStatus(msg.PhaseID, PhaseWaiting)
    m.NebulaView.SetSpeculatesOn(msg.PhaseID, "")
    m.addToast(fmt.Sprintf("Discarded: %s (%s)", msg.PhaseID, msg.Reason), toastWarning)
```

### 6. Wire speculative callbacks from `WorkerGroup`

Add an `OnSpeculative` callback to `WorkerGroup`:

```go
type WorkerGroup struct {
    // ... existing fields ...
    OnSpeculative func(phaseID, speculatesOn string, confirmed bool)
}
```

In `dispatchSpeculative`, call the callback:

```go
if wg.OnSpeculative != nil {
    wg.OnSpeculative(cand.PhaseID, cand.SpeculatesOn, false)  // false = not yet confirmed
}
```

In `resolveSpeculativeOutcomes`, call it again for confirmation/discard:

```go
if wg.OnSpeculative != nil {
    wg.OnSpeculative(phaseID, specCtx.DependsOnPhaseID, confirmed)
}
```

In `cmd/nebula_apply.go`, wire the callback to send TUI messages:

```go
wg.OnSpeculative = func(phaseID, speculatesOn string, confirmed bool) {
    if confirmed {
        program.Send(tui.MsgPhaseSpeculativeConfirmed{PhaseID: phaseID})
    } else if speculatesOn != "" {
        program.Send(tui.MsgPhaseSpeculativeStarted{
            PhaseID:      phaseID,
            SpeculatesOn: speculatesOn,
        })
    }
}
```

### 7. Add speculative indicator to `NebulaView` methods

```go
// SetSpeculatesOn records which dependency a phase is speculating past.
func (nv *NebulaView) SetSpeculatesOn(phaseID, speculatesOn string) {
    for i := range nv.Phases {
        if nv.Phases[i].ID == phaseID {
            nv.Phases[i].SpeculatesOn = speculatesOn
            return
        }
    }
}
```

### 8. Update stderr progress bar

In `internal/ui/nebula.go`, update `NebulaProgressBar` to show speculative count when non-zero:

```go
func NebulaProgressBar(completed, total, speculative int, ...) {
    // Format: "[nebula] 3/7 phases complete (1 speculative) | $2.34 spent"
}
```

### 9. Add speculative count to status bar

In `internal/tui/statusbar.go`, extend the status bar to show speculative phase count:

```go
// When speculative phases are running, show: "3/7 done | 1 speculative"
```

## Files

- `internal/tui/nebulaview.go` — Add `PhaseSpeculative` status, `SpeculatesOn` field to `PhaseEntry`, `SetSpeculatesOn` method, speculative icon/style in `phaseIconAndStyle`, speculative detail in `renderPhaseRow`
- `internal/tui/msg.go` — Add `MsgPhaseSpeculativeStarted`, `MsgPhaseSpeculativeConfirmed`, `MsgPhaseSpeculativeDiscarded` messages
- `internal/tui/model.go` — Handle speculative messages in `Update()`, show toast notifications
- `internal/tui/statusbar.go` — Show speculative count in status bar
- `internal/tui/bridge.go` — Wire speculative state changes to message sends (if needed beyond `OnSpeculative` callback)
- `internal/ui/nebula.go` — Update `NebulaProgressBar` to accept and display speculative count
- `internal/nebula/worker.go` — Add `OnSpeculative` callback to `WorkerGroup`
- `cmd/nebula_apply.go` — Wire `OnSpeculative` callback to TUI program

## Acceptance Criteria

- [ ] `PhaseSpeculative` status added to TUI `PhaseStatus` enum
- [ ] Speculative phases render with amber/yellow icon and color
- [ ] `PhaseEntry.SpeculatesOn` shows which dependency is being speculated past
- [ ] Phase row displays "(speculates on X)" annotation when speculative
- [ ] Toast notifications appear for speculative start, confirm, and discard events
- [ ] `MsgPhaseSpeculativeStarted`, `MsgPhaseSpeculativeConfirmed`, `MsgPhaseSpeculativeDiscarded` messages defined
- [ ] `AppModel.Update()` handles all three speculative message types
- [ ] Status bar shows speculative count when non-zero
- [ ] Stderr progress bar shows speculative count for non-TUI path
- [ ] `OnSpeculative` callback wired from `WorkerGroup` to TUI program
- [ ] Phase transitions from Speculative to Working on confirm, Speculative to Waiting on discard
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
