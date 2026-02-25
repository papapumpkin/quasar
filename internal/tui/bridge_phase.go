package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/ui"
)

// PhaseUIBridge implements ui.UI by sending phase-contextualized messages.
// Each nebula phase gets its own PhaseUIBridge so messages carry the PhaseID.
type PhaseUIBridge struct {
	program *tea.Program
	phaseID string
	workDir string // working directory for git diff capture
	cycle   int    // current cycle number, set by CycleStart
}

// Verify PhaseUIBridge satisfies ui.UI at compile time.
var _ ui.UI = (*PhaseUIBridge)(nil)

// NewPhaseUIBridge creates a bridge tagged with a specific phase ID.
// The workDir is used to run git diff after coder agents complete.
func NewPhaseUIBridge(p *tea.Program, phaseID, workDir string) *PhaseUIBridge {
	return &PhaseUIBridge{program: p, phaseID: phaseID, workDir: workDir}
}

// TaskStarted sends MsgPhaseTaskStarted.
func (b *PhaseUIBridge) TaskStarted(beadID, title string) {
	b.program.Send(MsgPhaseTaskStarted{PhaseID: b.phaseID, BeadID: beadID, Title: title})
	b.ScratchpadNote(b.phaseID, "started")
}

// TaskComplete sends MsgPhaseTaskComplete.
func (b *PhaseUIBridge) TaskComplete(beadID string, totalCost float64) {
	b.program.Send(MsgPhaseTaskComplete{PhaseID: b.phaseID, BeadID: beadID, TotalCost: totalCost})
	b.ScratchpadNote(b.phaseID, fmt.Sprintf("complete ($%.2f)", totalCost))
}

// CycleStart sends MsgPhaseCycleStart and records the current cycle number.
func (b *PhaseUIBridge) CycleStart(cycle, maxCycles int) {
	b.cycle = cycle
	b.program.Send(MsgPhaseCycleStart{PhaseID: b.phaseID, Cycle: cycle, MaxCycles: maxCycles})
	b.ScratchpadNote(b.phaseID, fmt.Sprintf("cycle %d", cycle))
}

// AgentStart sends MsgPhaseAgentStart.
func (b *PhaseUIBridge) AgentStart(role string) {
	b.program.Send(MsgPhaseAgentStart{PhaseID: b.phaseID, Role: role})
	b.ScratchpadNote(b.phaseID, fmt.Sprintf("%s running", role))
}

// AgentDone sends MsgPhaseAgentDone. For coder agents, it also captures the
// git diff of the most recent commit and sends MsgPhaseAgentDiff.
func (b *PhaseUIBridge) AgentDone(role string, costUSD float64, durationMs int64) {
	b.program.Send(MsgPhaseAgentDone{PhaseID: b.phaseID, Role: role, CostUSD: costUSD, DurationMs: durationMs})
	b.ScratchpadNote(b.phaseID, fmt.Sprintf("%s done ($%.2f)", role, costUSD))
	if role == "coder" {
		if dr := captureGitDiff(b.workDir, "", ""); dr.Diff != "" {
			b.program.Send(MsgPhaseAgentDiff{
				PhaseID: b.phaseID,
				Role:    role,
				Cycle:   b.cycle,
				Diff:    dr.Diff,
				BaseRef: dr.BaseRef,
				HeadRef: dr.HeadRef,
				Files:   dr.Files,
				WorkDir: b.workDir,
			})
		}
	}
}

// CycleSummary sends MsgPhaseCycleSummary.
func (b *PhaseUIBridge) CycleSummary(d ui.CycleSummaryData) {
	b.program.Send(MsgPhaseCycleSummary{PhaseID: b.phaseID, Data: d})
}

// IssuesFound sends MsgPhaseIssuesFound.
func (b *PhaseUIBridge) IssuesFound(count int) {
	b.program.Send(MsgPhaseIssuesFound{PhaseID: b.phaseID, Count: count})
	b.ScratchpadNote(b.phaseID, fmt.Sprintf("%d issues found", count))
}

// Approved sends MsgPhaseApproved.
func (b *PhaseUIBridge) Approved() {
	b.program.Send(MsgPhaseApproved{PhaseID: b.phaseID})
	b.ScratchpadNote(b.phaseID, "approved")
}

// MaxCyclesReached sends MsgPhaseError (treated as an error for the phase).
func (b *PhaseUIBridge) MaxCyclesReached(max int) {
	b.program.Send(MsgPhaseError{PhaseID: b.phaseID, Msg: fmt.Sprintf("max cycles reached (%d)", max)})
	b.ScratchpadNote(b.phaseID, fmt.Sprintf("max cycles reached (%d)", max))
}

// BudgetExceeded sends MsgPhaseError.
func (b *PhaseUIBridge) BudgetExceeded(spent, limit float64) {
	b.program.Send(MsgPhaseError{PhaseID: b.phaseID, Msg: fmt.Sprintf("budget exceeded ($%.2f / $%.2f)", spent, limit)})
	b.ScratchpadNote(b.phaseID, fmt.Sprintf("budget exceeded ($%.2f / $%.2f)", spent, limit))
}

// Error sends MsgPhaseError.
func (b *PhaseUIBridge) Error(msg string) {
	b.program.Send(MsgPhaseError{PhaseID: b.phaseID, Msg: msg})
	b.ScratchpadNote(b.phaseID, fmt.Sprintf("error: %s", msg))
}

// Info sends MsgPhaseInfo.
func (b *PhaseUIBridge) Info(msg string) {
	b.program.Send(MsgPhaseInfo{PhaseID: b.phaseID, Msg: msg})
}

// AgentOutput sends MsgPhaseAgentOutput.
func (b *PhaseUIBridge) AgentOutput(role string, cycle int, output string) {
	b.program.Send(MsgPhaseAgentOutput{PhaseID: b.phaseID, Role: role, Cycle: cycle, Output: output})
}

// RefactorApplied sends MsgPhaseRefactorApplied to notify the TUI that the
// loop consumed the pending refactor for this phase.
func (b *PhaseUIBridge) RefactorApplied(phaseID string) {
	b.program.Send(MsgPhaseRefactorApplied{PhaseID: b.phaseID})
}

// BeadUpdate sends MsgPhaseBeadUpdate with the bead hierarchy for this phase.
func (b *PhaseUIBridge) BeadUpdate(taskBeadID, title, status string, children []ui.BeadChild) {
	root := buildBeadInfoTree(taskBeadID, title, status, children)
	b.program.Send(MsgPhaseBeadUpdate{PhaseID: b.phaseID, TaskBeadID: taskBeadID, Root: root})
}

// HailReceived sends MsgHailReceived tagged with this phase's ID.
func (b *PhaseUIBridge) HailReceived(h ui.HailInfo) {
	b.program.Send(MsgHailReceived{PhaseID: b.phaseID, Hail: h})
}

// HailResolved sends MsgHailResolved tagged with this phase's ID.
func (b *PhaseUIBridge) HailResolved(id, resolution string) {
	b.program.Send(MsgHailResolved{PhaseID: b.phaseID, ID: id, Resolution: resolution})
}

// EntanglementPublished sends MsgEntanglementUpdate with the full entanglement list.
func (b *PhaseUIBridge) EntanglementPublished(entanglements []fabric.Entanglement) {
	b.program.Send(MsgEntanglementUpdate{Entanglements: entanglements})
}

// DiscoveryPosted sends MsgDiscoveryPosted when a new discovery is recorded.
func (b *PhaseUIBridge) DiscoveryPosted(d fabric.Discovery) {
	b.program.Send(MsgDiscoveryPosted{Discovery: d})
}

// Hail sends MsgHail when a phase requires human attention.
// The ResponseCh is nil here (fire-and-forget). Callers that need to block on
// the user's response should use HailAndWait instead, which creates a channel
// and blocks until the overlay resolves or the context is canceled.
func (b *PhaseUIBridge) Hail(phaseID string, d fabric.Discovery) {
	b.program.Send(MsgHail{PhaseID: phaseID, Discovery: d})
}

// HailAndWait sends MsgHail and blocks until the user responds or the context
// is canceled. Returns the user's free-text response.
func (b *PhaseUIBridge) HailAndWait(ctx context.Context, phaseID string, d fabric.Discovery) (string, error) {
	responseCh := make(chan string, 1)
	b.program.Send(MsgHail{PhaseID: phaseID, Discovery: d, ResponseCh: responseCh})

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case resp := <-responseCh:
		return resp, nil
	}
}

// ScratchpadNote sends MsgScratchpadEntry with a timestamped note.
func (b *PhaseUIBridge) ScratchpadNote(phaseID, text string) {
	b.program.Send(MsgScratchpadEntry{
		Timestamp: time.Now(),
		PhaseID:   phaseID,
		Text:      text,
	})
}
