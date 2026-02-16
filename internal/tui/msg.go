package tui

import (
	"time"

	"github.com/aaronsalm/quasar/internal/nebula"
	"github.com/aaronsalm/quasar/internal/ui"
)

// Loop lifecycle messages — sent by UIBridge in response to ui.UI calls.

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

// Nebula messages — sent by progress callbacks and gater.

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
