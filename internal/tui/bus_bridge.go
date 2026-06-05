package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/papapumpkin/quasar/internal/bus"
	"github.com/papapumpkin/quasar/internal/ui"
)

// Verify BusUIBridge satisfies ui.UI at compile time.
var _ ui.UI = (*BusUIBridge)(nil)

// BusUIBridge implements ui.UI by publishing events to a bus.Bus.
// It replaces PhaseUIBridge for the bus-mediated TUI path. Each instance
// is scoped to a single phase (identified by phaseID).
type BusUIBridge struct {
	bus     bus.Bus
	phaseID string
	workDir string
	cycle   int
}

// NewBusUIBridge creates a bridge that publishes phase-scoped events to the bus.
func NewBusUIBridge(b bus.Bus, phaseID, workDir string) *BusUIBridge {
	return &BusUIBridge{bus: b, phaseID: phaseID, workDir: workDir}
}

// publish is a helper that publishes an event to the bus, logging errors
// to the scratchpad rather than propagating them (fire-and-forget).
func (b *BusUIBridge) publish(ev bus.Event) {
	_ = b.bus.Publish(context.Background(), ev)
}

// scratchpad publishes a KindScratchpadEntry event for the current phase.
func (b *BusUIBridge) scratchpad(text string) {
	ev := bus.NewPhase(bus.KindScratchpadEntry, b.phaseID)
	ev.Message = text
	ev.Timestamp = time.Now()
	b.publish(ev)
}

// TaskStarted publishes KindPhaseTaskStarted.
func (b *BusUIBridge) TaskStarted(beadID, title string) {
	ev := bus.NewPhase(bus.KindPhaseTaskStarted, b.phaseID)
	ev.BeadID = beadID
	ev.Title = title
	b.publish(ev)
	b.scratchpad("started")
}

// TaskComplete publishes KindPhaseTaskComplete.
func (b *BusUIBridge) TaskComplete(beadID string, totalCost float64) {
	ev := bus.NewPhase(bus.KindPhaseTaskComplete, b.phaseID)
	ev.BeadID = beadID
	ev.CostUSD = totalCost
	b.publish(ev)
	b.scratchpad(fmt.Sprintf("complete ($%.2f)", totalCost))
}

// CycleStart publishes KindPhaseCycleStart and records the current cycle number.
func (b *BusUIBridge) CycleStart(cycle, maxCycles int) {
	b.cycle = cycle
	ev := bus.NewPhase(bus.KindPhaseCycleStart, b.phaseID)
	ev.Cycle = cycle
	ev.MaxCycles = maxCycles
	b.publish(ev)
	b.scratchpad(fmt.Sprintf("cycle %d", cycle))
}

// AgentStart publishes KindPhaseAgentStart.
func (b *BusUIBridge) AgentStart(role string) {
	ev := bus.NewPhase(bus.KindPhaseAgentStart, b.phaseID)
	ev.Role = role
	b.publish(ev)
	b.scratchpad(fmt.Sprintf("%s running", role))
}

// AgentDone publishes KindPhaseAgentDone. For coder agents, it also captures
// the git diff and publishes KindPhaseAgentDiff.
func (b *BusUIBridge) AgentDone(role string, costUSD float64, durationMs int64) {
	ev := bus.NewPhase(bus.KindPhaseAgentDone, b.phaseID)
	ev.Role = role
	ev.CostUSD = costUSD
	ev.DurationMs = durationMs
	b.publish(ev)
	b.scratchpad(fmt.Sprintf("%s done ($%.2f)", role, costUSD))

	if role == "coder" {
		// Capture git diff in the same goroutine (matches PhaseUIBridge behavior).
		if dr := captureGitDiff(b.workDir, "", ""); dr.Diff != "" {
			diffEv := bus.NewPhase(bus.KindPhaseAgentDiff, b.phaseID)
			diffEv.Role = role
			diffEv.Cycle = b.cycle
			diffEv.AgentDiff = &bus.AgentDiffPayload{
				Diff:    dr.Diff,
				BaseRef: dr.BaseRef,
				HeadRef: dr.HeadRef,
				Files:   fileStatPaths(dr.Files),
				WorkDir: b.workDir,
			}
			b.publish(diffEv)
		}
	}
}

// CycleSummary publishes KindPhaseCycleSummary.
func (b *BusUIBridge) CycleSummary(d ui.CycleSummaryData) {
	ev := bus.NewPhase(bus.KindPhaseCycleSummary, b.phaseID)
	ev.CycleSummary = &bus.CycleSummaryPayload{
		Cycle:             d.Cycle,
		MaxCycles:         d.MaxCycles,
		Phase:             d.Phase,
		CostUSD:           d.CostUSD,
		TotalCostUSD:      d.TotalCostUSD,
		MaxBudgetUSD:      d.MaxBudgetUSD,
		DurationMs:        d.DurationMs,
		Approved:          d.Approved,
		IssueCount:        d.IssueCount,
		FilterFixAttempts: d.FilterFixAttempts,
		FilterFixCostUSD:  d.FilterFixCostUSD,
		FilterFixSuccess:  d.FilterFixSuccess,
	}
	b.publish(ev)
}

// IssuesFound publishes KindPhaseIssuesFound.
func (b *BusUIBridge) IssuesFound(count int) {
	ev := bus.NewPhase(bus.KindPhaseIssuesFound, b.phaseID)
	ev.Count = count
	b.publish(ev)
	b.scratchpad(fmt.Sprintf("%d issues found", count))
}

// Approved publishes KindPhaseApproved.
func (b *BusUIBridge) Approved() {
	ev := bus.NewPhase(bus.KindPhaseApproved, b.phaseID)
	b.publish(ev)
	b.scratchpad("approved")
}

// MaxCyclesReached publishes KindPhaseError with a descriptive message.
func (b *BusUIBridge) MaxCyclesReached(max int) {
	ev := bus.NewPhase(bus.KindPhaseError, b.phaseID)
	ev.Message = fmt.Sprintf("max cycles reached (%d)", max)
	b.publish(ev)
	b.scratchpad(ev.Message)
}

// BudgetExceeded publishes KindPhaseError with a descriptive message.
func (b *BusUIBridge) BudgetExceeded(spent, limit float64) {
	ev := bus.NewPhase(bus.KindPhaseError, b.phaseID)
	ev.Message = fmt.Sprintf("budget exceeded ($%.2f / $%.2f)", spent, limit)
	b.publish(ev)
	b.scratchpad(ev.Message)
}

// Error publishes KindPhaseError.
func (b *BusUIBridge) Error(msg string) {
	ev := bus.NewPhase(bus.KindPhaseError, b.phaseID)
	ev.Message = msg
	b.publish(ev)
	b.scratchpad(fmt.Sprintf("error: %s", msg))
}

// Info publishes KindPhaseInfo.
func (b *BusUIBridge) Info(msg string) {
	ev := bus.NewPhase(bus.KindPhaseInfo, b.phaseID)
	ev.Message = msg
	b.publish(ev)
}

// AgentOutput publishes KindPhaseAgentOutput.
func (b *BusUIBridge) AgentOutput(role string, cycle int, output string) {
	ev := bus.NewPhase(bus.KindPhaseAgentOutput, b.phaseID)
	ev.Role = role
	ev.Cycle = cycle
	ev.Message = output
	b.publish(ev)
}

// BeadUpdate publishes KindPhaseBeadUpdate with the bead hierarchy.
func (b *BusUIBridge) BeadUpdate(taskBeadID, title, status string, children []ui.BeadChild) {
	root := buildBeadInfoTree(taskBeadID, title, status, children)
	ev := bus.NewPhase(bus.KindPhaseBeadUpdate, b.phaseID)
	ev.BeadTree = &bus.BeadTreePayload{
		TaskID: taskBeadID,
		Root:   &root,
	}
	b.publish(ev)
}

// FindingLifecycle is a no-op for BusUIBridge; finding lifecycle data is
// surfaced through the phase detail view rather than as a standalone message.
func (b *BusUIBridge) FindingLifecycle(cycle int, summary ui.FindingLifecycleData) {}

// fileStatPaths extracts file paths from a slice of FileStatEntry.
// The bus AgentDiffPayload carries paths as []string; this converts from the
// richer diffResult.Files representation.
func fileStatPaths(entries []FileStatEntry) []string {
	if len(entries) == 0 {
		return nil
	}
	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.Path
	}
	return paths
}
