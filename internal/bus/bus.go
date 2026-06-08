// Package bus provides a typed publish-subscribe event bus for decoupling
// event producers (workers, loops) from consumers (TUI, telemetry, logging).
// All types are safe for concurrent use from multiple goroutines.
package bus

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors.
var (
	// ErrBusClosed is returned by Publish when the bus has been closed.
	ErrBusClosed = errors.New("bus: closed")
)

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

// Kind is the event type discriminator.
type Kind string

// Phase lifecycle event kinds — correspond to MsgPhase* TUI messages.
const (
	KindPhaseTaskStarted      Kind = "phase.task.started"
	KindPhaseTaskComplete     Kind = "phase.task.complete"
	KindPhaseCycleStart       Kind = "phase.cycle.start"
	KindPhaseAgentStart       Kind = "phase.agent.start"
	KindPhaseAgentDone        Kind = "phase.agent.done"
	KindPhaseAgentOutput      Kind = "phase.agent.output"
	KindPhaseAgentDiff        Kind = "phase.agent.diff"
	KindPhaseCycleSummary     Kind = "phase.cycle.summary"
	KindPhaseIssuesFound      Kind = "phase.issues.found"
	KindPhaseApproved         Kind = "phase.approved"
	KindPhaseError            Kind = "phase.error"
	KindPhaseInfo             Kind = "phase.info"
	KindPhaseBeadUpdate       Kind = "phase.bead.update"
	KindPhaseHotAdded         Kind = "phase.hot.added"
	KindPhaseScanning         Kind = "phase.scanning"
	KindPhaseFindingLifecycle Kind = "phase.finding.lifecycle"
)

// Single-task lifecycle event kinds — correspond to Msg* TUI messages (loop mode).
const (
	KindTaskStarted      Kind = "task.started"
	KindTaskComplete     Kind = "task.complete"
	KindCycleStart       Kind = "cycle.start"
	KindAgentStart       Kind = "agent.start"
	KindAgentDone        Kind = "agent.done"
	KindAgentOutput      Kind = "agent.output"
	KindAgentDiff        Kind = "agent.diff"
	KindCycleSummary     Kind = "cycle.summary"
	KindIssuesFound      Kind = "issues.found"
	KindApproved         Kind = "approved"
	KindMaxCyclesReached Kind = "max.cycles.reached"
	KindBudgetExceeded   Kind = "budget.exceeded"
	KindError            Kind = "error"
	KindInfo             Kind = "info"
	KindBeadUpdate       Kind = "bead.update"
)

// Nebula control event kinds — correspond to nebula lifecycle TUI messages.
const (
	KindNebulaProgress     Kind = "nebula.progress"
	KindNebulaDone         Kind = "nebula.done"
	KindGatePrompt         Kind = "gate.prompt"
	KindGateResolved       Kind = "gate.resolved"
	KindHealingAttempt     Kind = "healing.attempt"
	KindEntanglementUpdate Kind = "entanglement.update"
	KindDiscoveryPosted    Kind = "discovery.posted"
	KindScratchpadEntry    Kind = "scratchpad.entry"
)

// Plan lifecycle event kinds — correspond to MsgPlan* TUI messages.
const (
	KindPlanReady  Kind = "plan.ready"
	KindPlanAction Kind = "plan.action"
	KindPlanError  Kind = "plan.error"
)

// KindStaleWarning represents a stale-state alert from the Tycho scheduler.
// Corresponds to MsgStaleWarning in the TUI.
const KindStaleWarning Kind = "stale.warning"

// Engine lifecycle event kinds — correspond to Engine phase transitions.
const (
	KindEngineLoading    Kind = "engine.loading"
	KindEnginePlanning   Kind = "engine.planning"
	KindEngineExecuting  Kind = "engine.executing"
	KindEngineCompleting Kind = "engine.completing"
	KindEngineDone       Kind = "engine.done"
)

// Event is the canonical bus event. The Kind field determines which
// payload fields are populated.
type Event struct {
	// Kind is the event type discriminator.
	Kind Kind

	// Timestamp is when the event was created.
	Timestamp time.Time

	// PhaseID is the originating phase (populated for KindPhase* events).
	PhaseID string

	// Common identity fields.
	BeadID string
	Title  string
	Role   string

	// Cycle context.
	Cycle     int
	MaxCycles int

	// Message carries human-readable text (errors, info, output).
	Message string

	// Numeric payloads.
	CostUSD    float64
	DurationMs int64
	Count      int
	Spent      float64
	Limit      float64
	Tokens     int

	// Rich payloads — at most one is non-nil per event.
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
	PlanReady    *PlanReadyPayload
	PlanAction   *PlanActionPayload
	PlanError    *PlanErrorPayload
	StaleWarning *StaleWarningPayload

	// Fabric payloads — kept as any to avoid import cycles with the fabric package.
	Entanglements   any // []fabric.Entanglement for KindEntanglementUpdate
	FabricDiscovery any // fabric.Discovery for KindDiscoveryPosted

	// Collisions carries scope-overlap conflicts for KindEntanglementUpdate.
	// It is a concrete []CollisionPayload (not any) because CollisionPayload is
	// defined in this package, so there is no import cycle to avoid.
	Collisions []CollisionPayload
}

// CollisionPayload is a transport-neutral description of a scheduler scope
// collision: two phases whose owned file scopes overlap and therefore cannot
// run concurrently. It mirrors nebula.Collision without importing it, letting
// the TUI surface deferred-phase contention without coupling to the
// orchestrator package.
type CollisionPayload struct {
	// Scope is the overlapping scope pattern(s).
	Scope string
	// PhaseID is the candidate phase being deferred.
	PhaseID string
	// OtherPhaseID is the conflicting phase it would collide with.
	OtherPhaseID string
}

// CycleSummaryPayload carries structured data for a completed coder-reviewer
// cycle. Corresponds to ui.CycleSummaryData.
type CycleSummaryPayload struct {
	Cycle             int
	MaxCycles         int
	Phase             string
	CostUSD           float64
	TotalCostUSD      float64
	MaxBudgetUSD      float64
	DurationMs        int64
	Approved          bool
	IssueCount        int
	FilterFixAttempts int
	FilterFixCostUSD  float64
	FilterFixSuccess  bool
}

// AgentDiffPayload carries a git diff produced by an agent.
type AgentDiffPayload struct {
	Diff    string
	BaseRef string
	HeadRef string
	Files   []string
	WorkDir string
}

// ProgressPayload carries nebula execution progress.
type ProgressPayload struct {
	Completed    int
	Total        int
	OpenBeads    int
	ClosedBeads  int
	TotalCostUSD float64
}

// BeadTreePayload carries a bead hierarchy snapshot.
type BeadTreePayload struct {
	TaskID string
	Root   any // *tui.BeadInfo — kept as any to avoid import cycle
}

// DonePayload carries completion results for a nebula run.
type DonePayload struct {
	Results []any // []nebula.WorkerResult — kept as any to avoid import cycle
	Err     error
}

// GatePromptPayload carries a gate checkpoint requiring a human decision.
type GatePromptPayload struct {
	Checkpoint any        // nebula.Checkpoint — kept as any to avoid import cycle
	ResponseCh chan<- any // nebula.GateAction response channel
}

// GateResolvedPayload carries the result of a gate decision.
type GateResolvedPayload struct {
	Action string
}

// HotAddPayload carries metadata for a dynamically inserted phase.
type HotAddPayload struct {
	Title     string
	DependsOn []string
}

// HealingPayload carries auto-healing context for a failed phase.
type HealingPayload struct {
	FailedPhaseID    string
	FailureKind      string
	RemediationID    string
	RemediationTitle string
}

// FindingPayload carries finding lifecycle statistics.
type FindingPayload struct {
	Fixed        int
	StillPresent int
	Regressed    int
}

// PlanReadyPayload carries a computed execution plan for preview.
type PlanReadyPayload struct {
	Plan      any // *nebula.ExecutionPlan — kept as any to avoid import cycle
	Changes   any // []nebula.PlanChange — kept as any to avoid import cycle
	NebulaDir string
}

// PlanActionPayload carries the user's decision from the plan preview.
type PlanActionPayload struct {
	Action    int // PlanAction enum value from tui package
	Plan      any // *nebula.ExecutionPlan — kept as any to avoid import cycle
	NebulaDir string
}

// PlanErrorPayload carries an error from plan computation.
type PlanErrorPayload struct {
	Err error
}

// StaleWarningPayload carries stale-state items detected by the Tycho scheduler.
type StaleWarningPayload struct {
	Items any // []tycho.StaleItem — kept as any to avoid import cycle
}

// New returns a timestamp-populated Event with the given kind.
func New(kind Kind) Event {
	return Event{Kind: kind, Timestamp: time.Now()}
}

// NewPhase returns a timestamp-populated Event with the given kind and phase ID.
func NewPhase(kind Kind, phaseID string) Event {
	return Event{Kind: kind, PhaseID: phaseID, Timestamp: time.Now()}
}
