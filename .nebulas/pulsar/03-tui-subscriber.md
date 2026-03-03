+++
id = "tui-subscriber"
title = "TUI bus subscriber adapter (bus events to tea.Msg)"
type = "task"
priority = 2
depends_on = ["bus-impl"]
scope = ["internal/tui/bus_subscriber.go"]
+++

## Problem

The TUI currently receives events via two mechanisms:

1. **`PhaseUIBridge`** (`internal/tui/bridge.go`): each phase's loop holds a `PhaseUIBridge` that directly calls `tea.Program.Send()` with `MsgPhase*` structs. This is created per-phase inside `tuiLoopAdapter.RunExistingPhase`.
2. **Worker callbacks** on `WorkerGroup`: `OnProgress`, `OnRefactor`, `OnHotAdd`, `OnHail`, `OnScanning` are ad-hoc function fields wired in `runNebulaApply` to call `tea.Program.Send()`.

Both patterns require the producer to hold a reference to `*tea.Program` and to know the exact TUI message type. This creates a direct dependency from producers to `internal/tui`.

With the bus in place, the TUI should become a passive subscriber that reads bus events and translates them to `tea.Msg` structs sent via `tea.Program.Send()`. The TUI's rendering logic (AppModel, NebulaView, LoopView, etc.) stays completely unchanged.

## Solution

Create `internal/tui/bus_subscriber.go` containing a `BusSubscriber` that:
1. Subscribes to the bus.
2. Reads events from the subscription channel in a goroutine.
3. Maps each `bus.Event` to the corresponding `tui.Msg*` struct.
4. Calls `tea.Program.Send()` with the mapped message.

### BusSubscriber struct

```go
// BusSubscriber bridges a bus.Subscription to a BubbleTea program by
// converting bus events into tea.Msg structs. It runs a background
// goroutine that reads events and calls program.Send().
type BusSubscriber struct {
    program *tea.Program
    sub     bus.Subscription
    done    chan struct{}
}

// NewBusSubscriber creates a subscriber that converts bus events to TUI
// messages. Call Start to begin processing. The bufSize parameter controls
// backpressure (0 uses the bus default).
func NewBusSubscriber(program *tea.Program, b bus.Bus, bufSize int) *BusSubscriber {
    return &BusSubscriber{
        program: program,
        sub:     b.Subscribe("tui", bufSize),
        done:    make(chan struct{}),
    }
}
```

### Start/Stop lifecycle

```go
// Start begins the background event processing goroutine.
func (s *BusSubscriber) Start() {
    go s.run()
}

// Stop unsubscribes from the bus and waits for the processing goroutine
// to drain and exit.
func (s *BusSubscriber) Stop() {
    s.sub.Unsubscribe()
    <-s.done
}

func (s *BusSubscriber) run() {
    defer close(s.done)
    for ev := range s.sub.Events() {
        msg := s.mapEvent(ev)
        if msg != nil {
            s.program.Send(msg)
        }
    }
}
```

### Event-to-message mapping

The `mapEvent` method is a switch on `ev.Kind` that reconstructs the exact `tui.Msg*` struct the TUI model expects. This is a direct 1:1 translation — every bus event kind maps to its original TUI message type:

```go
func (s *BusSubscriber) mapEvent(ev bus.Event) tea.Msg {
    switch ev.Kind {

    // Phase lifecycle
    case bus.KindPhaseTaskStarted:
        return MsgPhaseTaskStarted{PhaseID: ev.PhaseID, BeadID: ev.BeadID, Title: ev.Title}
    case bus.KindPhaseTaskComplete:
        return MsgPhaseTaskComplete{PhaseID: ev.PhaseID, BeadID: ev.BeadID, TotalCost: ev.CostUSD}
    case bus.KindPhaseCycleStart:
        return MsgPhaseCycleStart{PhaseID: ev.PhaseID, Cycle: ev.Cycle, MaxCycles: ev.MaxCycles}
    case bus.KindPhaseAgentStart:
        return MsgPhaseAgentStart{PhaseID: ev.PhaseID, Role: ev.Role}
    case bus.KindPhaseAgentDone:
        return MsgPhaseAgentDone{
            PhaseID: ev.PhaseID, Role: ev.Role,
            CostUSD: ev.CostUSD, DurationMs: ev.DurationMs, Tokens: ev.Tokens,
        }
    case bus.KindPhaseAgentOutput:
        return MsgPhaseAgentOutput{PhaseID: ev.PhaseID, Role: ev.Role, Cycle: ev.Cycle, Output: ev.Message}
    case bus.KindPhaseAgentDiff:
        if ev.AgentDiff == nil {
            return nil
        }
        return MsgPhaseAgentDiff{
            PhaseID: ev.PhaseID, Role: ev.Role, Cycle: ev.Cycle,
            Diff: ev.AgentDiff.Diff, BaseRef: ev.AgentDiff.BaseRef,
            HeadRef: ev.AgentDiff.HeadRef, Files: ev.AgentDiff.Files,
            WorkDir: ev.AgentDiff.WorkDir,
        }
    case bus.KindPhaseCycleSummary:
        if ev.CycleSummary == nil {
            return nil
        }
        return MsgPhaseCycleSummary{PhaseID: ev.PhaseID, Data: cycleSummaryFromPayload(ev.CycleSummary)}
    case bus.KindPhaseIssuesFound:
        return MsgPhaseIssuesFound{PhaseID: ev.PhaseID, Count: ev.Count}
    case bus.KindPhaseApproved:
        return MsgPhaseApproved{PhaseID: ev.PhaseID}
    case bus.KindPhaseError:
        return MsgPhaseError{PhaseID: ev.PhaseID, Msg: ev.Message}
    case bus.KindPhaseInfo:
        return MsgPhaseInfo{PhaseID: ev.PhaseID, Msg: ev.Message}
    case bus.KindPhaseBeadUpdate:
        if ev.BeadTree == nil {
            return nil
        }
        root, _ := ev.BeadTree.Root.(*BeadInfo)
        return MsgPhaseBeadUpdate{PhaseID: ev.PhaseID, TaskBeadID: ev.BeadTree.TaskBeadID, Root: root}
    case bus.KindPhaseRefactorPending:
        return MsgPhaseRefactorPending{PhaseID: ev.PhaseID}
    case bus.KindPhaseRefactorApplied:
        return MsgPhaseRefactorApplied{PhaseID: ev.PhaseID}
    case bus.KindPhaseHotAdded:
        if ev.HotAdd == nil {
            return nil
        }
        return MsgPhaseHotAdded{PhaseID: ev.PhaseID, Title: ev.HotAdd.Title, DependsOn: ev.HotAdd.DependsOn}
    case bus.KindPhaseScanning:
        return MsgPhaseScanning{PhaseID: ev.PhaseID}

    // Nebula control
    case bus.KindNebulaProgress:
        if ev.Progress == nil {
            return nil
        }
        return MsgNebulaProgress{
            Completed: ev.Progress.Completed, Total: ev.Progress.Total,
            OpenBeads: ev.Progress.OpenBeads, ClosedBeads: ev.Progress.ClosedBeads,
            TotalCostUSD: ev.Progress.TotalCostUSD,
        }
    case bus.KindNebulaDone:
        if ev.DoneResults == nil {
            return nil
        }
        // Type-assert results back to []nebula.WorkerResult.
        results, _ := ev.DoneResults.Results.([]WorkerResult)
        return MsgNebulaDone{Results: results, Err: ev.DoneResults.Err}
    case bus.KindHealingAttempt:
        if ev.Healing == nil {
            return nil
        }
        return MsgHealingAttempt{
            FailedPhaseID: ev.Healing.FailedPhaseID, FailureKind: ev.Healing.FailureKind,
            RemediationID: ev.Healing.RemediationID, RemediationTitle: ev.Healing.RemediationTitle,
        }

    // Single-task lifecycle (loop mode)
    case bus.KindTaskStarted:
        return MsgTaskStarted{BeadID: ev.BeadID, Title: ev.Title}
    case bus.KindTaskComplete:
        return MsgTaskComplete{BeadID: ev.BeadID, TotalCost: ev.CostUSD}
    case bus.KindCycleStart:
        return MsgCycleStart{Cycle: ev.Cycle, MaxCycles: ev.MaxCycles}
    case bus.KindAgentStart:
        return MsgAgentStart{Role: ev.Role}
    case bus.KindAgentDone:
        return MsgAgentDone{Role: ev.Role, CostUSD: ev.CostUSD, DurationMs: ev.DurationMs, Tokens: ev.Tokens}
    case bus.KindAgentOutput:
        return MsgAgentOutput{Role: ev.Role, Cycle: ev.Cycle, Output: ev.Message}
    case bus.KindIssuesFound:
        return MsgIssuesFound{Count: ev.Count}
    case bus.KindApproved:
        return MsgApproved{}
    case bus.KindMaxCyclesReached:
        return MsgMaxCyclesReached{Max: ev.MaxCycles}
    case bus.KindBudgetExceeded:
        return MsgBudgetExceeded{Spent: ev.Spent, Limit: ev.Limit}
    case bus.KindError:
        return MsgError{Msg: ev.Message}
    case bus.KindInfo:
        return MsgInfo{Msg: ev.Message}

    default:
        return nil
    }
}
```

### Helper for CycleSummary conversion

```go
func cycleSummaryFromPayload(p *bus.CycleSummaryPayload) ui.CycleSummaryData {
    return ui.CycleSummaryData{
        Cycle:             p.Cycle,
        MaxCycles:         p.MaxCycles,
        Phase:             p.Phase,
        CostUSD:           p.CostUSD,
        TotalCostUSD:      p.TotalCostUSD,
        MaxBudgetUSD:      p.MaxBudgetUSD,
        DurationMs:        p.DurationMs,
        Approved:          p.Approved,
        IssueCount:        p.IssueCount,
        FilterFixAttempts: p.FilterFixAttempts,
        FilterFixCostUSD:  p.FilterFixCostUSD,
        FilterFixSuccess:  p.FilterFixSuccess,
    }
}
```

### Important: Gate and Hail events

`MsgGatePrompt` and `MsgHail` carry response channels. The bus subscriber must pass the original channel through so the TUI's `Gater` and hail handler can respond:

```go
case bus.KindGatePrompt:
    if ev.GatePrompt == nil {
        return nil
    }
    return MsgGatePrompt{Checkpoint: ev.GatePrompt.Checkpoint, ResponseCh: ev.GatePrompt.ResponseCh}
case bus.KindPhaseHailReceived:
    if ev.Hail == nil {
        return nil
    }
    return MsgHail{PhaseID: ev.PhaseID, Discovery: ev.Hail.Discovery, ResponseCh: ev.Hail.ResponseCh}
case bus.KindPhaseHailResolved:
    if ev.HailResolved == nil {
        return nil
    }
    return MsgHailResolved{PhaseID: ev.PhaseID, ID: ev.HailResolved.ID, Resolution: ev.HailResolved.Resolution}
```

## Files

- `internal/tui/bus_subscriber.go` — `BusSubscriber` struct, `NewBusSubscriber`, `Start`, `Stop`, `run`, `mapEvent`, `cycleSummaryFromPayload`

## Acceptance Criteria

- [ ] `go build ./internal/tui/...` compiles with the new file
- [ ] `go vet ./internal/tui/...` passes
- [ ] `BusSubscriber` has `Start()` and `Stop()` lifecycle methods
- [ ] Every `bus.Kind*` constant has a corresponding case in `mapEvent`
- [ ] Mapping produces exactly the same `tui.Msg*` struct that the existing bridge/callback code produces
- [ ] Gate prompt response channels are passed through (not swallowed)
- [ ] Hail response channels are passed through
- [ ] `Stop()` unsubscribes and waits for the goroutine to exit (no goroutine leak)
- [ ] No behavioral change in TUI rendering — the AppModel.Update() method receives identical messages
- [ ] Existing TUI tests continue to pass
