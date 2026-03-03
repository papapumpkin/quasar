+++
id = "bus-interface"
title = "Define bus interface and canonical event types"
type = "task"
priority = 1
depends_on = []
scope = ["internal/bus/**"]
+++

## Problem

Events currently flow from workers directly to `tea.Program.Send()` via bridge structs (`UIBridge`, `PhaseUIBridge` in `internal/tui/bridge.go`) and to telemetry via scattered `Emitter.Emit()` calls in `internal/nebula/metrics.go`. This tight coupling means:

- Adding a new consumer (logging, metrics aggregation, external webhook) requires modifying producer code.
- The TUI bridge must exist at construction time, even though the TUI may not be running (stderr path).
- Telemetry events and TUI events are separate type hierarchies (`telemetry.Event` vs `tui.Msg*` structs) describing the same state transitions.
- Worker callbacks (`OnProgress`, `OnRefactor`, `OnHotAdd`, `OnHail`, `OnScanning`) are ad-hoc function fields on `WorkerGroup` rather than a unified event stream.

A shared typed event bus decouples producers from consumers and provides a single canonical event vocabulary.

## Solution

Create `internal/bus/bus.go` with three components: the `Bus` interface, the `Subscriber` interface, and canonical event types.

### Bus interface

```go
// Bus is a typed publish-subscribe event bus. Publishers call Publish to
// broadcast events; subscribers receive events on a dedicated channel.
// Implementations must be safe for concurrent use.
type Bus interface {
    // Publish sends an event to all current subscribers. It blocks if any
    // subscriber's buffer is full (backpressure). Returns an error if the
    // bus is closed.
    Publish(ctx context.Context, ev Event) error

    // Subscribe returns a Subscription that receives all events published
    // after the call. The buffer size controls per-subscriber backpressure.
    // A zero bufSize uses a sensible default (64).
    Subscribe(name string, bufSize int) Subscription

    // Close shuts down the bus, closing all subscriber channels. Publish
    // calls after Close return ErrBusClosed.
    Close() error
}

// Subscription represents a single subscriber's event stream.
type Subscription interface {
    // Events returns a receive-only channel of events. The channel is
    // closed when the bus shuts down or Unsubscribe is called.
    Events() <-chan Event

    // Unsubscribe detaches this subscriber from the bus.
    Unsubscribe()
}
```

### Canonical event types

Define a single `Event` struct with a `Kind` discriminator and typed payload fields. Events mirror the existing TUI message structs but are bus-native (no `tea.Msg` dependency).

```go
// Kind is the event type discriminator.
type Kind string

const (
    // Phase lifecycle
    KindPhaseTaskStarted   Kind = "phase.task.started"
    KindPhaseTaskComplete  Kind = "phase.task.complete"
    KindPhaseCycleStart    Kind = "phase.cycle.start"
    KindPhaseAgentStart    Kind = "phase.agent.start"
    KindPhaseAgentDone     Kind = "phase.agent.done"
    KindPhaseAgentOutput   Kind = "phase.agent.output"
    KindPhaseAgentDiff     Kind = "phase.agent.diff"
    KindPhaseCycleSummary  Kind = "phase.cycle.summary"
    KindPhaseIssuesFound   Kind = "phase.issues.found"
    KindPhaseApproved      Kind = "phase.approved"
    KindPhaseError         Kind = "phase.error"
    KindPhaseInfo          Kind = "phase.info"
    KindPhaseBeadUpdate    Kind = "phase.bead.update"
    KindPhaseRefactorPending  Kind = "phase.refactor.pending"
    KindPhaseRefactorApplied  Kind = "phase.refactor.applied"
    KindPhaseHotAdded      Kind = "phase.hot.added"
    KindPhaseScanning      Kind = "phase.scanning"
    KindPhaseFindingLifecycle Kind = "phase.finding.lifecycle"
    KindPhaseHailReceived  Kind = "phase.hail.received"
    KindPhaseHailResolved  Kind = "phase.hail.resolved"

    // Single-task lifecycle (loop mode)
    KindTaskStarted        Kind = "task.started"
    KindTaskComplete       Kind = "task.complete"
    KindCycleStart         Kind = "cycle.start"
    KindAgentStart         Kind = "agent.start"
    KindAgentDone          Kind = "agent.done"
    KindAgentOutput        Kind = "agent.output"
    KindAgentDiff          Kind = "agent.diff"
    KindCycleSummary       Kind = "cycle.summary"
    KindIssuesFound        Kind = "issues.found"
    KindApproved           Kind = "approved"
    KindMaxCyclesReached   Kind = "max.cycles.reached"
    KindBudgetExceeded     Kind = "budget.exceeded"
    KindError              Kind = "error"
    KindInfo               Kind = "info"
    KindBeadUpdate         Kind = "bead.update"

    // Nebula control
    KindNebulaProgress     Kind = "nebula.progress"
    KindNebulaDone         Kind = "nebula.done"
    KindGatePrompt         Kind = "gate.prompt"
    KindGateResolved       Kind = "gate.resolved"
    KindHealingAttempt     Kind = "healing.attempt"
    KindEntanglementUpdate Kind = "entanglement.update"
    KindDiscoveryPosted    Kind = "discovery.posted"
    KindScratchpadEntry    Kind = "scratchpad.entry"
)

// Event is the canonical bus event. The Kind field determines which
// payload fields are populated.
type Event struct {
    Kind      Kind
    Timestamp time.Time

    // Phase context (populated for KindPhase* events)
    PhaseID string

    // Common fields
    BeadID  string
    Title   string
    Role    string
    Cycle   int
    Message string

    // Numeric payloads
    CostUSD    float64
    DurationMs int64
    Count      int
    MaxCycles  int
    Spent      float64
    Limit      float64
    Tokens     int

    // Rich payloads (at most one is non-nil per event)
    CycleSummary *CycleSummaryPayload
    AgentDiff    *AgentDiffPayload
    Progress     *ProgressPayload
    BeadTree     *BeadTreePayload
    DoneResults  *DonePayload
    GatePrompt   *GatePromptPayload
    GateResolved *GateResolvedPayload
    HotAdd       *HotAddPayload
    Healing      *HealingPayload
    Finding      *FindingPayload
    Hail         *HailPayload
    HailResolved *HailResolvedPayload
}

// Sentinel errors
var (
    ErrBusClosed = errors.New("bus: closed")
)
```

Define the payload structs to carry structured data. These correspond 1:1 to the existing TUI message payloads:

```go
type CycleSummaryPayload struct {
    Cycle              int
    MaxCycles          int
    Phase              string
    CostUSD            float64
    TotalCostUSD       float64
    MaxBudgetUSD       float64
    DurationMs         int64
    Approved           bool
    IssueCount         int
    FilterFixAttempts  int
    FilterFixCostUSD   float64
    FilterFixSuccess   bool
}

type AgentDiffPayload struct {
    Diff    string
    BaseRef string
    HeadRef string
    Files   []string
    WorkDir string
}

type ProgressPayload struct {
    Completed    int
    Total        int
    OpenBeads    int
    ClosedBeads  int
    TotalCostUSD float64
}

type BeadTreePayload struct {
    TaskBeadID string
    Root       any // *tui.BeadInfo — kept as any to avoid import cycle
}

type DonePayload struct {
    Results []any // []nebula.WorkerResult — kept as any to avoid import cycle
    Err     error
}

type GatePromptPayload struct {
    Checkpoint any // nebula.GateCheckpoint
    ResponseCh chan<- any
}

type GateResolvedPayload struct {
    Action string
}

type HotAddPayload struct {
    Title     string
    DependsOn []string
}

type HealingPayload struct {
    FailedPhaseID    string
    FailureKind      string
    RemediationID    string
    RemediationTitle string
}

type FindingPayload struct {
    Fixed        int
    StillPresent int
    Regressed    int
}

type HailPayload struct {
    Discovery  any // fabric.Discovery
    ResponseCh chan<- any
}

type HailResolvedPayload struct {
    ID         string
    Resolution string
}
```

### Constructor helper

```go
// New returns a timestamp-populated Event with the given kind.
func New(kind Kind) Event {
    return Event{Kind: kind, Timestamp: time.Now()}
}

// NewPhase returns a timestamp-populated Event with the given kind and phase ID.
func NewPhase(kind Kind, phaseID string) Event {
    return Event{Kind: kind, PhaseID: phaseID, Timestamp: time.Now()}
}
```

## Files

- `internal/bus/bus.go` — Bus and Subscription interfaces, Event struct, Kind constants, payload structs, sentinel errors, constructor helpers

## Acceptance Criteria

- [ ] `internal/bus/bus.go` compiles with `go build ./internal/bus/...`
- [ ] `go vet ./internal/bus/...` passes
- [ ] Bus interface has Publish, Subscribe, Close methods
- [ ] Subscription interface has Events, Unsubscribe methods
- [ ] Every existing `tui.MsgPhase*` message type has a corresponding bus Kind constant
- [ ] Every existing `tui.Msg*` (non-phase) message type has a corresponding bus Kind constant
- [ ] Event struct covers all payload fields needed to reconstruct any TUI message
- [ ] No dependency on `internal/tui` or `charmbracelet/bubbletea` from the bus package
- [ ] No dependency on `internal/nebula` or `internal/fabric` — use `any` for cross-package types
- [ ] `ErrBusClosed` sentinel error is exported
