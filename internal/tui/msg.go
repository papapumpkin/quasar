package tui

import (
	"time"

	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/ui"
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
	Tokens     int
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

// MsgAgentDiff carries a git diff for an agent's changes (loop mode).
type MsgAgentDiff struct {
	Role    string
	Cycle   int
	Diff    string
	BaseRef string          // git ref before this cycle (empty when unavailable)
	HeadRef string          // git ref after this cycle (empty when unavailable)
	Files   []FileStatEntry // pre-parsed file stats
	WorkDir string          // working directory for git operations
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

// MsgGitPostCompletion delivers the results of the post-nebula git workflow
// (push branch to origin, checkout main).
type MsgGitPostCompletion struct {
	Result *nebula.PostCompletionResult
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

// MsgSplashDone signals that the splash screen timer has elapsed.
type MsgSplashDone struct{}

// PlanAction represents the user's chosen action from the plan preview.
type PlanAction int

const (
	// PlanActionApply proceeds with nebula execution.
	PlanActionApply PlanAction = iota
	// PlanActionCancel returns to the home page.
	PlanActionCancel
	// PlanActionSave writes the plan to disk as JSON.
	PlanActionSave
)

// MsgPlanReady is sent when the execution plan has been computed for a
// selected nebula, transitioning the home view into the plan preview.
type MsgPlanReady struct {
	Plan      *nebula.ExecutionPlan
	Changes   []nebula.PlanChange // diff vs. previous plan (nil if no prior plan)
	NebulaDir string
}

// MsgPlanAction is sent when the user makes a choice in the plan preview.
type MsgPlanAction struct {
	Action    PlanAction
	Plan      *nebula.ExecutionPlan
	NebulaDir string
}

// MsgPlanError is sent when plan computation fails.
type MsgPlanError struct {
	Err error
}
