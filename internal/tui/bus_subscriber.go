// Package tui provides a BubbleTea-based terminal user interface for quasar.

package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/papapumpkin/quasar/internal/bus"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/tycho"
	"github.com/papapumpkin/quasar/internal/ui"
)

// BusSubscriber bridges a bus.Subscription to a BubbleTea program by
// converting bus events into tea.Msg structs. It runs a background
// goroutine that reads events and calls program.Send().
type BusSubscriber struct {
	program *tea.Program
	sub     bus.Subscription
	done    chan struct{}
}

// NewBusSubscriber creates a subscriber that converts bus events to TUI
// messages. Call Start to begin processing. The bufSize parameter controls
// backpressure (0 uses the bus default).
func NewBusSubscriber(program *tea.Program, b bus.Bus, bufSize int) *BusSubscriber {
	return &BusSubscriber{
		program: program,
		sub:     b.Subscribe("tui", bufSize),
		done:    make(chan struct{}),
	}
}

// Start begins the background event processing goroutine.
func (s *BusSubscriber) Start() {
	go s.run()
}

// Stop unsubscribes from the bus and waits for the processing goroutine
// to drain and exit.
func (s *BusSubscriber) Stop() {
	s.sub.Unsubscribe()
	<-s.done
}

func (s *BusSubscriber) run() {
	defer close(s.done)
	for ev := range s.sub.Events() {
		msg := s.mapEvent(ev)
		if msg != nil {
			s.program.Send(msg)
		}
	}
}

// mapEvent translates a bus.Event into the corresponding tea.Msg that the
// AppModel.Update() method expects. Returns nil for unrecognized event kinds
// or events with missing payloads.
func (s *BusSubscriber) mapEvent(ev bus.Event) tea.Msg {
	switch ev.Kind {

	// ── Phase lifecycle ───────────────────────────────────────────────
	case bus.KindPhaseTaskStarted:
		return MsgPhaseTaskStarted{PhaseID: ev.PhaseID, BeadID: ev.BeadID, Title: ev.Title}
	case bus.KindPhaseTaskComplete:
		return MsgPhaseTaskComplete{PhaseID: ev.PhaseID, BeadID: ev.BeadID, TotalCost: ev.CostUSD}
	case bus.KindPhaseCycleStart:
		return MsgPhaseCycleStart{PhaseID: ev.PhaseID, Cycle: ev.Cycle, MaxCycles: ev.MaxCycles}
	case bus.KindPhaseAgentStart:
		return MsgPhaseAgentStart{PhaseID: ev.PhaseID, Role: ev.Role}
	case bus.KindPhaseAgentDone:
		return MsgPhaseAgentDone{
			PhaseID: ev.PhaseID, Role: ev.Role,
			CostUSD: ev.CostUSD, DurationMs: ev.DurationMs, Tokens: ev.Tokens,
		}
	case bus.KindPhaseAgentOutput:
		return MsgPhaseAgentOutput{PhaseID: ev.PhaseID, Role: ev.Role, Cycle: ev.Cycle, Output: ev.Message}
	case bus.KindPhaseAgentDiff:
		return mapPhaseAgentDiff(ev)
	case bus.KindPhaseCycleSummary:
		if ev.CycleSummary == nil {
			return nil
		}
		return MsgPhaseCycleSummary{PhaseID: ev.PhaseID, Data: cycleSummaryFromPayload(ev.CycleSummary)}
	case bus.KindPhaseIssuesFound:
		return MsgPhaseIssuesFound{PhaseID: ev.PhaseID, Count: ev.Count}
	case bus.KindPhaseApproved:
		return MsgPhaseApproved{PhaseID: ev.PhaseID}
	case bus.KindPhaseError:
		return MsgPhaseError{PhaseID: ev.PhaseID, Msg: ev.Message}
	case bus.KindPhaseInfo:
		return MsgPhaseInfo{PhaseID: ev.PhaseID, Msg: ev.Message}
	case bus.KindPhaseBeadUpdate:
		return mapPhaseBeadUpdate(ev)
	case bus.KindPhaseRefactorPending:
		return MsgPhaseRefactorPending{PhaseID: ev.PhaseID}
	case bus.KindPhaseRefactorApplied:
		return MsgPhaseRefactorApplied{PhaseID: ev.PhaseID}
	case bus.KindPhaseHotAdded:
		if ev.HotAdd == nil {
			return nil
		}
		return MsgPhaseHotAdded{PhaseID: ev.PhaseID, Title: ev.HotAdd.Title, DependsOn: ev.HotAdd.DependsOn}
	case bus.KindPhaseScanning:
		return MsgPhaseScanning{PhaseID: ev.PhaseID}
	case bus.KindPhaseFindingLifecycle:
		// Finding lifecycle is currently a no-op in the bridge; drop silently.
		return nil

	// ── Single-task lifecycle (loop mode) ─────────────────────────────
	case bus.KindTaskStarted:
		return MsgTaskStarted{BeadID: ev.BeadID, Title: ev.Title}
	case bus.KindTaskComplete:
		return MsgTaskComplete{BeadID: ev.BeadID, TotalCost: ev.CostUSD}
	case bus.KindCycleStart:
		return MsgCycleStart{Cycle: ev.Cycle, MaxCycles: ev.MaxCycles}
	case bus.KindAgentStart:
		return MsgAgentStart{Role: ev.Role}
	case bus.KindAgentDone:
		return MsgAgentDone{Role: ev.Role, CostUSD: ev.CostUSD, DurationMs: ev.DurationMs, Tokens: ev.Tokens}
	case bus.KindAgentOutput:
		return MsgAgentOutput{Role: ev.Role, Cycle: ev.Cycle, Output: ev.Message}
	case bus.KindAgentDiff:
		return mapAgentDiff(ev)
	case bus.KindCycleSummary:
		if ev.CycleSummary == nil {
			return nil
		}
		return MsgCycleSummary{Data: cycleSummaryFromPayload(ev.CycleSummary)}
	case bus.KindIssuesFound:
		return MsgIssuesFound{Count: ev.Count}
	case bus.KindApproved:
		return MsgApproved{}
	case bus.KindMaxCyclesReached:
		return MsgMaxCyclesReached{Max: ev.MaxCycles}
	case bus.KindBudgetExceeded:
		return MsgBudgetExceeded{Spent: ev.Spent, Limit: ev.Limit}
	case bus.KindError:
		return MsgError{Msg: ev.Message}
	case bus.KindInfo:
		return MsgInfo{Msg: ev.Message}
	case bus.KindBeadUpdate:
		return mapBeadUpdate(ev)

	// ── Nebula control ────────────────────────────────────────────────
	case bus.KindNebulaProgress:
		if ev.Progress == nil {
			return nil
		}
		return MsgNebulaProgress{
			Completed:    ev.Progress.Completed,
			Total:        ev.Progress.Total,
			OpenBeads:    ev.Progress.OpenBeads,
			ClosedBeads:  ev.Progress.ClosedBeads,
			TotalCostUSD: ev.Progress.TotalCostUSD,
		}
	case bus.KindNebulaDone:
		return mapNebulaDone(ev)
	case bus.KindGatePrompt:
		return s.mapGatePrompt(ev)
	case bus.KindGateResolved:
		return mapGateResolved(ev)
	case bus.KindHealingAttempt:
		if ev.Healing == nil {
			return nil
		}
		return MsgHealingAttempt{
			FailedPhaseID:    ev.Healing.FailedPhaseID,
			FailureKind:      ev.Healing.FailureKind,
			RemediationID:    ev.Healing.RemediationID,
			RemediationTitle: ev.Healing.RemediationTitle,
		}
	case bus.KindEntanglementUpdate:
		ents, ok := ev.Entanglements.([]fabric.Entanglement)
		if !ok || len(ents) == 0 {
			return nil
		}
		return MsgEntanglementUpdate{Entanglements: ents}
	case bus.KindDiscoveryPosted:
		disc, ok := ev.FabricDiscovery.(fabric.Discovery)
		if !ok {
			return nil
		}
		return MsgDiscoveryPosted{Discovery: disc}
	case bus.KindScratchpadEntry:
		return MsgScratchpadEntry{
			Timestamp: ev.Timestamp,
			PhaseID:   ev.PhaseID,
			Text:      ev.Message,
		}

	// ── Stale warning ─────────────────────────────────────────────────
	case bus.KindStaleWarning:
		return mapStaleWarning(ev)

	// ── Plan lifecycle ────────────────────────────────────────────────
	case bus.KindPlanReady:
		return mapPlanReady(ev)
	case bus.KindPlanAction:
		return mapPlanAction(ev)
	case bus.KindPlanError:
		if ev.PlanError == nil {
			return nil
		}
		return MsgPlanError{Err: ev.PlanError.Err}

	default:
		return nil
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────

// cycleSummaryFromPayload converts a bus CycleSummaryPayload to the ui
// CycleSummaryData struct expected by the TUI's LoopView.
func cycleSummaryFromPayload(p *bus.CycleSummaryPayload) ui.CycleSummaryData {
	return ui.CycleSummaryData{
		Cycle:             p.Cycle,
		MaxCycles:         p.MaxCycles,
		Phase:             p.Phase,
		CostUSD:           p.CostUSD,
		TotalCostUSD:      p.TotalCostUSD,
		MaxBudgetUSD:      p.MaxBudgetUSD,
		DurationMs:        p.DurationMs,
		Approved:          p.Approved,
		IssueCount:        p.IssueCount,
		FilterFixAttempts: p.FilterFixAttempts,
		FilterFixCostUSD:  p.FilterFixCostUSD,
		FilterFixSuccess:  p.FilterFixSuccess,
	}
}

// fileStatEntriesFromStrings converts a []string of file paths into
// []FileStatEntry with zero additions/deletions. The bus payload carries
// only paths; detailed stats are computed by the bridge at capture time.
func fileStatEntriesFromStrings(paths []string) []FileStatEntry {
	if len(paths) == 0 {
		return nil
	}
	entries := make([]FileStatEntry, len(paths))
	for i, p := range paths {
		entries[i] = FileStatEntry{Path: p}
	}
	return entries
}

// mapPhaseAgentDiff converts a KindPhaseAgentDiff event to MsgPhaseAgentDiff.
func mapPhaseAgentDiff(ev bus.Event) tea.Msg {
	if ev.AgentDiff == nil {
		return nil
	}
	return MsgPhaseAgentDiff{
		PhaseID: ev.PhaseID,
		Role:    ev.Role,
		Cycle:   ev.Cycle,
		Diff:    ev.AgentDiff.Diff,
		BaseRef: ev.AgentDiff.BaseRef,
		HeadRef: ev.AgentDiff.HeadRef,
		Files:   fileStatEntriesFromStrings(ev.AgentDiff.Files),
		WorkDir: ev.AgentDiff.WorkDir,
	}
}

// mapAgentDiff converts a KindAgentDiff event to MsgAgentDiff (loop mode).
func mapAgentDiff(ev bus.Event) tea.Msg {
	if ev.AgentDiff == nil {
		return nil
	}
	return MsgAgentDiff{
		Role:    ev.Role,
		Cycle:   ev.Cycle,
		Diff:    ev.AgentDiff.Diff,
		BaseRef: ev.AgentDiff.BaseRef,
		HeadRef: ev.AgentDiff.HeadRef,
		Files:   fileStatEntriesFromStrings(ev.AgentDiff.Files),
		WorkDir: ev.AgentDiff.WorkDir,
	}
}

// mapPhaseBeadUpdate converts a KindPhaseBeadUpdate event to MsgPhaseBeadUpdate.
// BeadTreePayload.Root is stored as any to avoid import cycles; we assert it
// back to *BeadInfo here.
func mapPhaseBeadUpdate(ev bus.Event) tea.Msg {
	if ev.BeadTree == nil {
		return nil
	}
	root, ok := ev.BeadTree.Root.(*BeadInfo)
	if !ok || root == nil {
		return nil
	}
	return MsgPhaseBeadUpdate{
		PhaseID: ev.PhaseID,
		TaskID:  ev.BeadTree.TaskID,
		Root:    *root,
	}
}

// mapBeadUpdate converts a KindBeadUpdate event to MsgBeadUpdate (loop mode).
func mapBeadUpdate(ev bus.Event) tea.Msg {
	if ev.BeadTree == nil {
		return nil
	}
	root, ok := ev.BeadTree.Root.(*BeadInfo)
	if !ok || root == nil {
		return nil
	}
	return MsgBeadUpdate{
		TaskID: ev.BeadTree.TaskID,
		Root:   *root,
	}
}

// mapNebulaDone converts a KindNebulaDone event to MsgNebulaDone.
// DonePayload.Results is []any to avoid import cycles; each element is
// asserted back to nebula.WorkerResult.
func mapNebulaDone(ev bus.Event) tea.Msg {
	if ev.DoneResults == nil {
		return nil
	}
	results := make([]nebula.WorkerResult, 0, len(ev.DoneResults.Results))
	for _, r := range ev.DoneResults.Results {
		if wr, ok := r.(nebula.WorkerResult); ok {
			results = append(results, wr)
		}
	}
	return MsgNebulaDone{Results: results, Err: ev.DoneResults.Err}
}

// mapGatePrompt converts a KindGatePrompt event to MsgGatePrompt.
// The bus stores Checkpoint and ResponseCh as any to avoid import cycles.
// For the response channel, we create a typed adapter that forwards the
// nebula.GateAction to the underlying chan<- any.
func (s *BusSubscriber) mapGatePrompt(ev bus.Event) tea.Msg {
	if ev.GatePrompt == nil {
		return nil
	}
	checkpoint, ok := ev.GatePrompt.Checkpoint.(*nebula.Checkpoint)
	if !ok || checkpoint == nil {
		return nil
	}

	// Create a typed channel adapter: the TUI sends nebula.GateAction,
	// and we forward it to the bus's chan<- any channel.
	typedCh := make(chan nebula.GateAction, 1)
	go forwardGateAction(typedCh, ev.GatePrompt.ResponseCh, s.done)

	return MsgGatePrompt{
		Checkpoint: checkpoint,
		ResponseCh: typedCh,
	}
}

// forwardGateAction reads a single nebula.GateAction from the typed channel
// and forwards it to the bus's untyped response channel. This bridges the
// type gap between the bus (any) and the TUI (nebula.GateAction).
// The done channel allows the goroutine to exit if the subscriber stops
// before a response is received, preventing goroutine leaks.
func forwardGateAction(from <-chan nebula.GateAction, to chan<- any, done <-chan struct{}) {
	select {
	case action, ok := <-from:
		if ok && to != nil {
			to <- action
		}
	case <-done:
	}
}

// mapGateResolved converts a KindGateResolved event to MsgGateResolved.
func mapGateResolved(ev bus.Event) tea.Msg {
	if ev.GateResolved == nil {
		return nil
	}
	return MsgGateResolved{
		PhaseID: ev.PhaseID,
		Action:  nebula.GateAction(ev.GateResolved.Action),
	}
}

// mapStaleWarning converts a KindStaleWarning event to MsgStaleWarning.
// StaleWarningPayload.Items is stored as any to avoid import cycles.
func mapStaleWarning(ev bus.Event) tea.Msg {
	if ev.StaleWarning == nil {
		return nil
	}
	items, _ := ev.StaleWarning.Items.([]tycho.StaleItem)
	return MsgStaleWarning{Items: items}
}

// mapPlanReady converts a KindPlanReady event to MsgPlanReady.
// PlanReadyPayload stores Plan and Changes as any to avoid import cycles.
func mapPlanReady(ev bus.Event) tea.Msg {
	if ev.PlanReady == nil {
		return nil
	}
	plan, _ := ev.PlanReady.Plan.(*nebula.ExecutionPlan)
	changes, _ := ev.PlanReady.Changes.([]nebula.PlanChange)
	return MsgPlanReady{
		Plan:      plan,
		Changes:   changes,
		NebulaDir: ev.PlanReady.NebulaDir,
	}
}

// mapPlanAction converts a KindPlanAction event to MsgPlanAction.
// PlanActionPayload stores Action as int and Plan as any.
func mapPlanAction(ev bus.Event) tea.Msg {
	if ev.PlanAction == nil {
		return nil
	}
	plan, _ := ev.PlanAction.Plan.(*nebula.ExecutionPlan)
	return MsgPlanAction{
		Action:    PlanAction(ev.PlanAction.Action),
		Plan:      plan,
		NebulaDir: ev.PlanAction.NebulaDir,
	}
}
