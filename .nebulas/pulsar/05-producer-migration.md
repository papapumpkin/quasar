+++
id = "producer-migration"
title = "Migrate event producers to publish to the bus"
type = "task"
priority = 2
depends_on = ["tui-subscriber", "telemetry-subscriber"]
scope = ["internal/tui/bridge.go", "internal/nebula/worker*.go", "cmd/nebula_adapters.go", "cmd/nebula_apply.go"]
allow_scope_overlap = true
+++

## Problem

After phases `tui-subscriber` and `telemetry-subscriber`, the bus has consumers but no producers. Events still flow through the old paths:

1. **`PhaseUIBridge`** (`internal/tui/bridge.go`): each method (e.g. `TaskStarted`, `AgentDone`, `CycleSummary`) calls `tea.Program.Send()` directly with TUI message structs.
2. **Worker callbacks** on `WorkerGroup`: `OnProgress`, `OnRefactor`, `OnHotAdd`, `OnHail`, `OnScanning` are function fields set in `runNebulaApply`, each calling `tea.Program.Send()`.
3. **`tuiLoopAdapter.RunExistingPhase`** (`cmd/nebula_adapters.go`): creates a `PhaseUIBridge` and injects it as the `ui.UI` for the loop.

All three producer paths need to publish to the bus instead of (or in addition to) calling `tea.Program.Send()` directly. The transition must be incremental: the existing bridge code continues to work for the stderr printer path (which does not use the bus), while the TUI path switches to bus-mediated delivery.

## Solution

### Step 1: Create a `BusUIBridge` that implements `ui.UI` and publishes to the bus

Create `internal/tui/bus_bridge.go` with a new `BusUIBridge` struct that satisfies the `ui.UI` interface but publishes `bus.Event` instead of calling `tea.Program.Send()`. This is a drop-in replacement for `PhaseUIBridge` in the TUI path.

```go
// BusUIBridge implements ui.UI by publishing events to a bus.Bus.
// It replaces PhaseUIBridge for the bus-mediated TUI path. Each instance
// is scoped to a single phase (identified by phaseID).
type BusUIBridge struct {
    bus     bus.Bus
    phaseID string
    workDir string
    cycle   int
}

// NewBusUIBridge creates a bridge that publishes phase-scoped events to the bus.
func NewBusUIBridge(b bus.Bus, phaseID, workDir string) *BusUIBridge {
    return &BusUIBridge{bus: b, phaseID: phaseID, workDir: workDir}
}
```

Each method constructs and publishes the appropriate bus event:

```go
func (b *BusUIBridge) TaskStarted(beadID, title string) {
    ev := bus.NewPhase(bus.KindPhaseTaskStarted, b.phaseID)
    ev.BeadID = beadID
    ev.Title = title
    _ = b.bus.Publish(context.Background(), ev)
}

func (b *BusUIBridge) AgentDone(role string, costUSD float64, durationMs int64) {
    ev := bus.NewPhase(bus.KindPhaseAgentDone, b.phaseID)
    ev.Role = role
    ev.CostUSD = costUSD
    ev.DurationMs = durationMs
    _ = b.bus.Publish(context.Background(), ev)
    // Capture git diff after coder, same as PhaseUIBridge.AgentDone.
    if role == "coder" {
        go func() {
            dr := captureGitDiff(b.workDir)
            diffEv := bus.NewPhase(bus.KindPhaseAgentDiff, b.phaseID)
            diffEv.Role = role
            diffEv.Cycle = b.cycle
            diffEv.AgentDiff = &bus.AgentDiffPayload{
                Diff: dr.Diff, BaseRef: dr.BaseRef,
                HeadRef: dr.HeadRef, Files: dr.Files, WorkDir: b.workDir,
            }
            _ = b.bus.Publish(context.Background(), diffEv)
        }()
    }
}

func (b *BusUIBridge) CycleStart(cycle, maxCycles int) {
    b.cycle = cycle
    ev := bus.NewPhase(bus.KindPhaseCycleStart, b.phaseID)
    ev.Cycle = cycle
    ev.MaxCycles = maxCycles
    _ = b.bus.Publish(context.Background(), ev)
}

// ... (all other ui.UI methods follow the same pattern)
```

Implement all 18 methods of `ui.UI`:
- `TaskStarted` -> `KindPhaseTaskStarted`
- `TaskComplete` -> `KindPhaseTaskComplete`
- `CycleStart` -> `KindPhaseCycleStart`
- `AgentStart` -> `KindPhaseAgentStart`
- `AgentDone` -> `KindPhaseAgentDone` (plus async `KindPhaseAgentDiff` for coder)
- `CycleSummary` -> `KindPhaseCycleSummary`
- `IssuesFound` -> `KindPhaseIssuesFound`
- `Approved` -> `KindPhaseApproved`
- `MaxCyclesReached` -> `KindPhaseError` (with "max cycles reached" message)
- `BudgetExceeded` -> `KindPhaseError` (with "budget exceeded" message)
- `Error` -> `KindPhaseError`
- `Info` -> `KindPhaseInfo`
- `AgentOutput` -> `KindPhaseAgentOutput`
- `BeadUpdate` -> `KindPhaseBeadUpdate`
- `RefactorApplied` -> `KindPhaseRefactorApplied`
- `FindingLifecycle` -> `KindPhaseFindingLifecycle`
- `HailReceived` -> `KindPhaseHailReceived`
- `HailResolved` -> `KindPhaseHailResolved`

### Step 2: Migrate WorkerGroup callbacks to bus publishing

Add a `Bus` field to `WorkerGroup` (or pass the bus via an option):

```go
// In internal/nebula/worker_options.go:
func WithBus(b bus.Bus) Option {
    return func(wg *WorkerGroup) {
        wg.Bus = b
    }
}
```

Where the `WorkerGroup.Run` method currently calls `wg.OnProgress(...)`, `wg.OnRefactor(...)`, etc., add parallel bus publishes:

```go
// In the progress callback within WorkerGroup internals:
if wg.Bus != nil {
    ev := bus.New(bus.KindNebulaProgress)
    ev.Progress = &bus.ProgressPayload{
        Completed: completed, Total: total,
        OpenBeads: openBeads, ClosedBeads: closedBeads,
        TotalCostUSD: totalCostUSD,
    }
    _ = wg.Bus.Publish(ctx, ev)
}
```

This is additive — the existing `OnProgress` callback continues to work for the stderr path. When the bus is nil (stderr path), no bus events are published.

### Step 3: Wire `tuiLoopAdapter` to use `BusUIBridge`

In `cmd/nebula_adapters.go`, modify `tuiLoopAdapter` to accept a `bus.Bus` field. In `RunExistingPhase`, create a `BusUIBridge` instead of a `PhaseUIBridge` when the bus is available:

```go
func (a *tuiLoopAdapter) RunExistingPhase(ctx context.Context, phaseID, beadID, phaseTitle, phaseDescription string, exec nebula.ResolvedExecution) (*nebula.PhaseRunnerResult, error) {
    var phaseUI ui.UI
    if a.bus != nil {
        phaseUI = tui.NewBusUIBridge(a.bus, phaseID, a.workDir)
    } else {
        phaseUI = tui.NewPhaseUIBridge(a.program, phaseID, a.workDir)
    }
    l := &loop.Loop{
        UI: phaseUI,
        // ... rest unchanged
    }
    // ...
}
```

### Step 4: Remove direct OnProgress/OnRefactor/OnHotAdd/OnHail/OnScanning wiring in `runNebulaApply` TUI path

In `cmd/nebula_apply.go`, when useTUI is true and the bus is being used, stop setting the `wg.OnProgress`, `wg.OnRefactor`, `wg.OnHotAdd`, `wg.OnHail`, `wg.OnScanning` callback fields to functions that call `tea.Program.Send()`. Instead, these events flow through the bus and the `BusSubscriber` handles them. The stderr path keeps its callbacks unchanged.

## Files

- `internal/tui/bus_bridge.go` — `BusUIBridge` struct implementing `ui.UI`, publishes to `bus.Bus`
- `internal/nebula/worker_options.go` — add `WithBus(bus.Bus) Option` and `Bus` field to `WorkerGroup`
- `internal/nebula/worker.go` — add bus publishes alongside existing callback invocations in Run (for progress, refactor, hot-add, hail, scanning)
- `cmd/nebula_adapters.go` — add `bus` field to `tuiLoopAdapter`, use `BusUIBridge` when bus is available
- `cmd/nebula_apply.go` — in TUI path, pass bus to `tuiLoopAdapter` and `WorkerGroup`, remove direct `tea.Program.Send()` callback wiring

## Acceptance Criteria

- [ ] `go build ./...` compiles
- [ ] `go vet ./...` passes
- [ ] `go test ./...` passes (all existing tests)
- [ ] `BusUIBridge` implements `ui.UI` (compile-time check: `var _ ui.UI = (*BusUIBridge)(nil)`)
- [ ] Every `ui.UI` method in `BusUIBridge` publishes the correct bus event kind
- [ ] `AgentDone` for "coder" role captures git diff asynchronously (same as `PhaseUIBridge`)
- [ ] `WorkerGroup` publishes `KindNebulaProgress` when bus is non-nil
- [ ] `WorkerGroup` publishes `KindPhaseRefactorPending` when bus is non-nil and refactor detected
- [ ] `WorkerGroup` publishes `KindPhaseHotAdded` when bus is non-nil and hot-add occurs
- [ ] `WorkerGroup` publishes `KindPhaseScanning` when bus is non-nil and scanning gate entered
- [ ] Existing `OnProgress`/`OnRefactor`/`OnHotAdd`/`OnHail`/`OnScanning` callbacks still work for stderr path
- [ ] `tuiLoopAdapter` uses `BusUIBridge` when bus field is set
- [ ] TUI path in `runNebulaApply` wires bus instead of direct `tea.Program.Send()` callbacks
- [ ] Stderr path in `runNebulaApply` is completely unchanged
- [ ] No double-delivery of events in either TUI or stderr path
