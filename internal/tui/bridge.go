package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronsalm/quasar/internal/ui"
)

// UIBridge implements ui.UI by forwarding each call as a typed message
// to a BubbleTea program. tea.Program.Send is goroutine-safe, so multiple
// loop/nebula workers can call the bridge concurrently.
// Used in single-task (loop) mode where there is no phase context.
type UIBridge struct {
	program *tea.Program
}

// Verify UIBridge satisfies ui.UI at compile time.
var _ ui.UI = (*UIBridge)(nil)

// NewUIBridge creates a bridge that sends messages to the given program.
func NewUIBridge(p *tea.Program) *UIBridge {
	return &UIBridge{program: p}
}

// TaskStarted sends MsgTaskStarted.
func (b *UIBridge) TaskStarted(beadID, title string) {
	b.program.Send(MsgTaskStarted{BeadID: beadID, Title: title})
}

// TaskComplete sends MsgTaskComplete.
func (b *UIBridge) TaskComplete(beadID string, totalCost float64) {
	b.program.Send(MsgTaskComplete{BeadID: beadID, TotalCost: totalCost})
}

// CycleStart sends MsgCycleStart.
func (b *UIBridge) CycleStart(cycle, maxCycles int) {
	b.program.Send(MsgCycleStart{Cycle: cycle, MaxCycles: maxCycles})
}

// AgentStart sends MsgAgentStart.
func (b *UIBridge) AgentStart(role string) {
	b.program.Send(MsgAgentStart{Role: role})
}

// AgentDone sends MsgAgentDone.
func (b *UIBridge) AgentDone(role string, costUSD float64, durationMs int64) {
	b.program.Send(MsgAgentDone{Role: role, CostUSD: costUSD, DurationMs: durationMs})
}

// CycleSummary sends MsgCycleSummary.
func (b *UIBridge) CycleSummary(d ui.CycleSummaryData) {
	b.program.Send(MsgCycleSummary{Data: d})
}

// IssuesFound sends MsgIssuesFound.
func (b *UIBridge) IssuesFound(count int) {
	b.program.Send(MsgIssuesFound{Count: count})
}

// Approved sends MsgApproved.
func (b *UIBridge) Approved() {
	b.program.Send(MsgApproved{})
}

// MaxCyclesReached sends MsgMaxCyclesReached.
func (b *UIBridge) MaxCyclesReached(max int) {
	b.program.Send(MsgMaxCyclesReached{Max: max})
}

// BudgetExceeded sends MsgBudgetExceeded.
func (b *UIBridge) BudgetExceeded(spent, limit float64) {
	b.program.Send(MsgBudgetExceeded{Spent: spent, Limit: limit})
}

// Error sends MsgError.
func (b *UIBridge) Error(msg string) {
	b.program.Send(MsgError{Msg: msg})
}

// Info sends MsgInfo.
func (b *UIBridge) Info(msg string) {
	b.program.Send(MsgInfo{Msg: msg})
}

// AgentOutput sends MsgAgentOutput for drill-down display.
func (b *UIBridge) AgentOutput(role string, cycle int, output string) {
	b.program.Send(MsgAgentOutput{Role: role, Cycle: cycle, Output: output})
}

// PhaseUIBridge implements ui.UI by sending phase-contextualized messages.
// Each nebula phase gets its own PhaseUIBridge so messages carry the PhaseID.
type PhaseUIBridge struct {
	program *tea.Program
	phaseID string
}

// Verify PhaseUIBridge satisfies ui.UI at compile time.
var _ ui.UI = (*PhaseUIBridge)(nil)

// NewPhaseUIBridge creates a bridge tagged with a specific phase ID.
func NewPhaseUIBridge(p *tea.Program, phaseID string) *PhaseUIBridge {
	return &PhaseUIBridge{program: p, phaseID: phaseID}
}

// TaskStarted sends MsgPhaseTaskStarted.
func (b *PhaseUIBridge) TaskStarted(beadID, title string) {
	b.program.Send(MsgPhaseTaskStarted{PhaseID: b.phaseID, BeadID: beadID, Title: title})
}

// TaskComplete sends MsgPhaseTaskComplete.
func (b *PhaseUIBridge) TaskComplete(beadID string, totalCost float64) {
	b.program.Send(MsgPhaseTaskComplete{PhaseID: b.phaseID, BeadID: beadID, TotalCost: totalCost})
}

// CycleStart sends MsgPhaseCycleStart.
func (b *PhaseUIBridge) CycleStart(cycle, maxCycles int) {
	b.program.Send(MsgPhaseCycleStart{PhaseID: b.phaseID, Cycle: cycle, MaxCycles: maxCycles})
}

// AgentStart sends MsgPhaseAgentStart.
func (b *PhaseUIBridge) AgentStart(role string) {
	b.program.Send(MsgPhaseAgentStart{PhaseID: b.phaseID, Role: role})
}

// AgentDone sends MsgPhaseAgentDone.
func (b *PhaseUIBridge) AgentDone(role string, costUSD float64, durationMs int64) {
	b.program.Send(MsgPhaseAgentDone{PhaseID: b.phaseID, Role: role, CostUSD: costUSD, DurationMs: durationMs})
}

// CycleSummary sends MsgPhaseCycleSummary.
func (b *PhaseUIBridge) CycleSummary(d ui.CycleSummaryData) {
	b.program.Send(MsgPhaseCycleSummary{PhaseID: b.phaseID, Data: d})
}

// IssuesFound sends MsgPhaseIssuesFound.
func (b *PhaseUIBridge) IssuesFound(count int) {
	b.program.Send(MsgPhaseIssuesFound{PhaseID: b.phaseID, Count: count})
}

// Approved sends MsgPhaseApproved.
func (b *PhaseUIBridge) Approved() {
	b.program.Send(MsgPhaseApproved{PhaseID: b.phaseID})
}

// MaxCyclesReached sends MsgPhaseError (treated as an error for the phase).
func (b *PhaseUIBridge) MaxCyclesReached(max int) {
	b.program.Send(MsgPhaseError{PhaseID: b.phaseID, Msg: fmt.Sprintf("max cycles reached (%d)", max)})
}

// BudgetExceeded sends MsgPhaseError.
func (b *PhaseUIBridge) BudgetExceeded(spent, limit float64) {
	b.program.Send(MsgPhaseError{PhaseID: b.phaseID, Msg: fmt.Sprintf("budget exceeded ($%.2f / $%.2f)", spent, limit)})
}

// Error sends MsgPhaseError.
func (b *PhaseUIBridge) Error(msg string) {
	b.program.Send(MsgPhaseError{PhaseID: b.phaseID, Msg: msg})
}

// Info sends MsgPhaseInfo.
func (b *PhaseUIBridge) Info(msg string) {
	b.program.Send(MsgPhaseInfo{PhaseID: b.phaseID, Msg: msg})
}

// AgentOutput sends MsgPhaseAgentOutput.
func (b *PhaseUIBridge) AgentOutput(role string, cycle int, output string) {
	b.program.Send(MsgPhaseAgentOutput{PhaseID: b.phaseID, Role: role, Cycle: cycle, Output: output})
}
