package tui

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaronsalm/quasar/internal/ui"
)

// gitDiffTimeout is the maximum time allowed for a git diff subprocess.
const gitDiffTimeout = 10 * time.Second

// UIBridge implements ui.UI by forwarding each call as a typed message
// to a BubbleTea program. tea.Program.Send is goroutine-safe, so multiple
// loop/nebula workers can call the bridge concurrently.
// Used in single-task (loop) mode where there is no phase context.
type UIBridge struct {
	program *tea.Program
	workDir string // working directory for git diff capture
	cycle   int    // current cycle number, set by CycleStart
}

// Verify UIBridge satisfies ui.UI at compile time.
var _ ui.UI = (*UIBridge)(nil)

// NewUIBridge creates a bridge that sends messages to the given program.
// The workDir is used to run git diff after coder agents complete.
func NewUIBridge(p *tea.Program, workDir string) *UIBridge {
	return &UIBridge{program: p, workDir: workDir}
}

// TaskStarted sends MsgTaskStarted.
func (b *UIBridge) TaskStarted(beadID, title string) {
	b.program.Send(MsgTaskStarted{BeadID: beadID, Title: title})
}

// TaskComplete sends MsgTaskComplete.
func (b *UIBridge) TaskComplete(beadID string, totalCost float64) {
	b.program.Send(MsgTaskComplete{BeadID: beadID, TotalCost: totalCost})
}

// CycleStart sends MsgCycleStart and records the current cycle number.
func (b *UIBridge) CycleStart(cycle, maxCycles int) {
	b.cycle = cycle
	b.program.Send(MsgCycleStart{Cycle: cycle, MaxCycles: maxCycles})
}

// AgentStart sends MsgAgentStart.
func (b *UIBridge) AgentStart(role string) {
	b.program.Send(MsgAgentStart{Role: role})
}

// AgentDone sends MsgAgentDone. For coder agents, it also captures the git
// diff of the most recent commit and sends MsgAgentDiff.
func (b *UIBridge) AgentDone(role string, costUSD float64, durationMs int64) {
	b.program.Send(MsgAgentDone{Role: role, CostUSD: costUSD, DurationMs: durationMs})
	if role == "coder" {
		if diff := captureGitDiff(b.workDir); diff != "" {
			b.program.Send(MsgAgentDiff{Role: role, Cycle: b.cycle, Diff: diff})
		}
	}
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

// BeadUpdate sends MsgBeadUpdate with the bead hierarchy.
func (b *UIBridge) BeadUpdate(taskBeadID, title, status string, children []ui.BeadChild) {
	root := buildBeadInfoTree(taskBeadID, title, status, children)
	b.program.Send(MsgBeadUpdate{TaskBeadID: taskBeadID, Root: root})
}

// buildBeadInfoTree converts a task bead and its children into a BeadInfo tree.
// Used by both UIBridge and PhaseUIBridge to avoid duplicated conversion logic.
func buildBeadInfoTree(taskBeadID, title, status string, children []ui.BeadChild) BeadInfo {
	root := BeadInfo{
		ID:     taskBeadID,
		Title:  title,
		Status: status,
		Type:   "task",
	}
	for _, c := range children {
		root.Children = append(root.Children, BeadInfo{
			ID:       c.ID,
			Title:    c.Title,
			Status:   c.Status,
			Type:     "bug",
			Severity: c.Severity,
			Cycle:    c.Cycle,
		})
	}
	return root
}

// captureGitDiff runs "git diff HEAD~1..HEAD" in the given directory and
// returns the unified diff output. Returns empty string on any error (no git
// repo, no commits, command failure) — diff capture is best-effort.
func captureGitDiff(workDir string) string {
	if workDir == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitDiffTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "diff", "HEAD~1..HEAD")
	cmd.Dir = workDir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil // discard stderr
	if err := cmd.Run(); err != nil {
		return ""
	}
	return out.String()
}

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
}

// TaskComplete sends MsgPhaseTaskComplete.
func (b *PhaseUIBridge) TaskComplete(beadID string, totalCost float64) {
	b.program.Send(MsgPhaseTaskComplete{PhaseID: b.phaseID, BeadID: beadID, TotalCost: totalCost})
}

// CycleStart sends MsgPhaseCycleStart and records the current cycle number.
func (b *PhaseUIBridge) CycleStart(cycle, maxCycles int) {
	b.cycle = cycle
	b.program.Send(MsgPhaseCycleStart{PhaseID: b.phaseID, Cycle: cycle, MaxCycles: maxCycles})
}

// AgentStart sends MsgPhaseAgentStart.
func (b *PhaseUIBridge) AgentStart(role string) {
	b.program.Send(MsgPhaseAgentStart{PhaseID: b.phaseID, Role: role})
}

// AgentDone sends MsgPhaseAgentDone. For coder agents, it also captures the
// git diff of the most recent commit and sends MsgPhaseAgentDiff.
func (b *PhaseUIBridge) AgentDone(role string, costUSD float64, durationMs int64) {
	b.program.Send(MsgPhaseAgentDone{PhaseID: b.phaseID, Role: role, CostUSD: costUSD, DurationMs: durationMs})
	if role == "coder" {
		if diff := captureGitDiff(b.workDir); diff != "" {
			b.program.Send(MsgPhaseAgentDiff{PhaseID: b.phaseID, Role: role, Cycle: b.cycle, Diff: diff})
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

// BeadUpdate sends MsgPhaseBeadUpdate with the bead hierarchy for this phase.
func (b *PhaseUIBridge) BeadUpdate(taskBeadID, title, status string, children []ui.BeadChild) {
	root := buildBeadInfoTree(taskBeadID, title, status, children)
	b.program.Send(MsgPhaseBeadUpdate{PhaseID: b.phaseID, TaskBeadID: taskBeadID, Root: root})
}
