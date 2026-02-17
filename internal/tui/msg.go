package tui

import (
	"time"

	"github.com/aaronsalm/quasar/internal/nebula"
	"github.com/aaronsalm/quasar/internal/ui"
)

// Loop lifecycle messages — sent by UIBridge in response to ui.UI calls.
// Used in single-task (loop) mode where there is no phase context.

// MsgTaskStarted is sent when a task begins.
type MsgTaskStarted struct {
	BeadID string
	Title  string
}

// MsgTaskComplete is sent when a task finishes successfully.
type MsgTaskComplete struct {
	BeadID    string
	TotalCost float64
}

// MsgCycleStart is sent at the beginning of each coder-reviewer cycle.
type MsgCycleStart struct {
	Cycle     int
	MaxCycles int
}

// MsgAgentStart is sent when an agent (coder/reviewer) begins work.
type MsgAgentStart struct {
	Role string
}

// MsgAgentDone is sent when an agent finishes.
type MsgAgentDone struct {
	Role       string
	CostUSD    float64
	DurationMs int64
}

// MsgCycleSummary is sent after each phase with structured summary data.
type MsgCycleSummary struct {
	Data ui.CycleSummaryData
}

// MsgIssuesFound is sent when the reviewer finds issues.
type MsgIssuesFound struct {
	Count int
}

// MsgApproved is sent when the reviewer approves the code.
type MsgApproved struct{}

// MsgMaxCyclesReached is sent when the cycle limit is hit.
type MsgMaxCyclesReached struct {
	Max int
}

// MsgBudgetExceeded is sent when the cost budget is exceeded.
type MsgBudgetExceeded struct {
	Spent float64
	Limit float64
}

// MsgError is sent for error messages.
type MsgError struct {
	Msg string
}

// MsgInfo is sent for informational messages.
type MsgInfo struct {
	Msg string
}

// MsgAgentOutput carries agent output for drill-down display.
type MsgAgentOutput struct {
	Role   string
	Cycle  int
	Output string
}

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
}

// MsgPhaseAgentOutput carries agent output for a specific phase.
type MsgPhaseAgentOutput struct {
	PhaseID string
	Role    string
	Cycle   int
	Output  string
}

// MsgAgentDiff carries a git diff for an agent's changes (loop mode).
type MsgAgentDiff struct {
	Role    string
	Cycle   int
	Diff    string
	BaseRef string          // git ref before this cycle (empty when unavailable)
	HeadRef string          // git ref after this cycle (empty when unavailable)
	Files   []FileStatEntry // pre-parsed file stats
	WorkDir string          // working directory for running git difftool
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
	WorkDir string          // working directory for running git difftool
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
	PlanBody  string // markdown content from the phase file
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

// Internal TUI messages.

// MsgTick drives the elapsed-time timer.
type MsgTick struct {
	Time time.Time
}

// MsgLoopDone signals the loop goroutine has finished.
type MsgLoopDone struct {
	Err error
}

// MsgNebulaDone signals the nebula goroutine has finished.
type MsgNebulaDone struct {
	Results []nebula.WorkerResult
	Err     error
}

// MsgNebulaChoicesLoaded delivers available nebulae after discovery completes.
type MsgNebulaChoicesLoaded struct {
	Choices []NebulaChoice
}

// MsgToastExpired signals that a toast notification should be dismissed.
type MsgToastExpired struct {
	ID int
}

// MsgResourceUpdate carries a periodic resource usage snapshot.
type MsgResourceUpdate struct {
	Snapshot ResourceSnapshot
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

// Architect overlay messages — drive the interactive phase creation/refactor flow.

// MsgArchitectStart triggers the architect agent to generate or refactor a phase.
type MsgArchitectStart struct {
	Mode    string // "create" or "refactor"
	PhaseID string // for refactor: which phase to modify
	Prompt  string // user's description of what they want
}

// MsgArchitectResult carries the architect agent's output back to the TUI.
type MsgArchitectResult struct {
	Result *nebula.ArchitectResult
	Err    error
}

// MsgArchitectConfirm signals the user confirmed the generated phase.
type MsgArchitectConfirm struct {
	Result    *nebula.ArchitectResult
	DependsOn []string // user-modified dependency list
}
