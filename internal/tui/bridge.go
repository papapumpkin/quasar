package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronsalm/quasar/internal/ui"
)

// UIBridge implements ui.UI by forwarding each call as a typed message
// to a BubbleTea program. tea.Program.Send is goroutine-safe, so multiple
// loop/nebula workers can call the bridge concurrently.
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
