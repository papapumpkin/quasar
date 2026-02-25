package tui

import (
	"time"

	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/tycho"
	"github.com/papapumpkin/quasar/internal/ui"
)

// Phase-contextualized messages — sent by PhaseUIBridge in nebula mode.
// Each message carries a PhaseID so the TUI can route it to the correct
// per-phase LoopView.

// MsgPhaseTaskStarted is sent when a phase's loop begins.
type MsgPhaseTaskStarted struct {
	PhaseID string
	BeadID  string
	Title   string
}

// MsgPhaseTaskComplete is sent when a phase's loop finishes.
type MsgPhaseTaskComplete struct {
	PhaseID   string
	BeadID    string
	TotalCost float64
}

// MsgPhaseCycleStart is sent at the beginning of a cycle within a phase.
type MsgPhaseCycleStart struct {
	PhaseID   string
	Cycle     int
	MaxCycles int
}

// MsgPhaseAgentStart is sent when an agent begins within a phase.
type MsgPhaseAgentStart struct {
	PhaseID string
	Role    string
}

// MsgPhaseAgentDone is sent when an agent finishes within a phase.
type MsgPhaseAgentDone struct {
	PhaseID    string
	Role       string
	CostUSD    float64
	DurationMs int64
	Tokens     int
}

// MsgPhaseAgentOutput carries agent output for a specific phase.
type MsgPhaseAgentOutput struct {
	PhaseID string
	Role    string
	Cycle   int
	Output  string
}

// MsgPhaseAgentDiff carries a git diff for an agent's changes (nebula mode).
type MsgPhaseAgentDiff struct {
	PhaseID string
	Role    string
	Cycle   int
	Diff    string
	BaseRef string          // git ref before this cycle (empty when unavailable)
	HeadRef string          // git ref after this cycle (empty when unavailable)
	Files   []FileStatEntry // pre-parsed file stats
	WorkDir string          // working directory for git operations
}

// MsgPhaseCycleSummary is sent after each coder/reviewer step within a phase.
type MsgPhaseCycleSummary struct {
	PhaseID string
	Data    ui.CycleSummaryData
}

// MsgPhaseIssuesFound is sent when issues are found within a phase.
type MsgPhaseIssuesFound struct {
	PhaseID string
	Count   int
}

// MsgPhaseApproved is sent when the reviewer approves within a phase.
type MsgPhaseApproved struct {
	PhaseID string
}

// MsgPhaseError is sent for an error within a phase.
type MsgPhaseError struct {
	PhaseID string
	Msg     string
}

// MsgPhaseInfo is sent for informational messages within a phase.
type MsgPhaseInfo struct {
	PhaseID string
	Msg     string
}

// Nebula initialization and lifecycle messages.

// PhaseInfo carries phase metadata for populating the NebulaView at startup.
type PhaseInfo struct {
	ID        string
	Title     string
	DependsOn []string
	PlanBody  string      // markdown content from the phase file
	Status    PhaseStatus // initial status from saved state (default PhaseWaiting)
}

// MsgNebulaInit is sent at TUI startup to populate the phase table.
type MsgNebulaInit struct {
	Name   string
	Phases []PhaseInfo
}

// MsgNebulaProgress is sent when nebula execution progress changes.
type MsgNebulaProgress struct {
	Completed    int
	Total        int
	OpenBeads    int
	ClosedBeads  int
	TotalCostUSD float64
}

// MsgGatePrompt is sent when a gate decision is needed from the user.
type MsgGatePrompt struct {
	Checkpoint *nebula.Checkpoint
	ResponseCh chan<- nebula.GateAction
}

// MsgGateResolved is sent after the user makes a gate decision.
type MsgGateResolved struct {
	PhaseID string
	Action  nebula.GateAction
}

// Phase refactor messages — sent when a phase file is edited during execution.

// MsgPhaseRefactorPending signals that a running phase's file was modified
// and the updated description is waiting to be applied after the current cycle.
type MsgPhaseRefactorPending struct {
	PhaseID string
}

// MsgPhaseRefactorApplied signals that the pending refactor was picked up by
// the loop and the new description is now in use.
type MsgPhaseRefactorApplied struct {
	PhaseID string
}

// MsgPhaseHotAdded signals that a new phase was dynamically inserted into
// the running nebula DAG.
type MsgPhaseHotAdded struct {
	PhaseID   string
	Title     string
	DependsOn []string
}

// MsgPhaseScanning is sent when a phase enters the fabric scanning gate,
// allowing the TUI to surface a brief toast before the phase starts running.
type MsgPhaseScanning struct {
	PhaseID string
}

// Bead hierarchy messages — carry bead state snapshots for the bead tracker.

// BeadInfo represents a bead's display state in the hierarchy.
type BeadInfo struct {
	ID       string
	Title    string
	Status   string     // "open", "in_progress", "closed"
	Type     string     // "epic", "task", "bug", "feature"
	Severity string     // "critical", "major", "minor" (empty for root)
	Cycle    int        // cycle in which this child was created (0 for root)
	Children []BeadInfo // nested child issues
}

// MsgBeadUpdate carries the current bead hierarchy for a task (loop mode).
type MsgBeadUpdate struct {
	TaskBeadID string
	Root       BeadInfo
}

// MsgPhaseBeadUpdate carries bead state for a specific phase (nebula mode).
type MsgPhaseBeadUpdate struct {
	PhaseID    string
	TaskBeadID string
	Root       BeadInfo
}

// Fabric bridge messages — carry fabric event data for the cockpit TUI.

// MsgEntanglementUpdate carries entanglement data for the cockpit viewer.
type MsgEntanglementUpdate struct {
	Entanglements []fabric.Entanglement
}

// MsgDiscoveryPosted surfaces a new discovery in the cockpit.
type MsgDiscoveryPosted struct {
	Discovery fabric.Discovery
}

// MsgHail surfaces a human-attention-required interrupt from a blocked phase.
// ResponseCh, when non-nil, carries the user's response back to the worker
// awaiting a human decision. A nil channel means fire-and-forget (the overlay
// renders but the response is silently dropped).
type MsgHail struct {
	PhaseID    string
	Discovery  fabric.Discovery
	ResponseCh chan<- string
}

// MsgHailReceived notifies the TUI that an agent has posted a new hail
// requiring human attention. Sent by UIBridge and PhaseUIBridge in response
// to the ui.UI.HailReceived call.
type MsgHailReceived struct {
	PhaseID string // Empty in single-task (loop) mode.
	Hail    ui.HailInfo
}

// MsgHailResolved notifies the TUI that a previously posted hail has been
// resolved by the human. Sent by UIBridge and PhaseUIBridge in response
// to the ui.UI.HailResolved call.
type MsgHailResolved struct {
	PhaseID    string // Empty in single-task (loop) mode.
	ID         string // Hail identifier that was resolved.
	Resolution string // The human's response text.
}

// MsgScratchpadEntry adds a timestamped note to the scratchpad view.
type MsgScratchpadEntry struct {
	Timestamp time.Time
	PhaseID   string
	Text      string
}

// MsgStaleWarning alerts the operator to stale state detected by the Tycho scheduler.
type MsgStaleWarning struct {
	Items []tycho.StaleItem
}
