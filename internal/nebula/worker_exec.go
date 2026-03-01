package nebula

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/dag"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// executePhase runs a single phase and records the result.
// It is intended to be called as a goroutine from the dispatch loop.
func (wg *WorkerGroup) executePhase(ctx context.Context, phaseID string, waveNumber int) {
	tracker := wg.tracker
	phasesByID := tracker.PhasesByIDMap()
	done := tracker.Done()
	failed := tracker.Failed()
	inFlight := tracker.InFlight()

	phase := phasesByID[phaseID]
	ps := wg.State.Phases[phaseID]
	if phase == nil || ps == nil || ps.BeadID == "" {
		wg.recordFailure(phaseID)
		return
	}

	wg.progress.RecordPhaseStart(phaseID, waveNumber)

	wg.mu.Lock()
	wg.State.SetPhaseState(phaseID, ps.BeadID, PhaseStatusInProgress)
	wg.progress.SaveState()
	wg.progress.ReportProgress()
	wg.mu.Unlock()

	exec := ResolveExecution(wg.GlobalCycles, wg.GlobalBudget, wg.GlobalModel, &wg.Nebula.Manifest.Execution, phase, wg.routingCtx)
	prompt := buildPhasePrompt(phase, &wg.Nebula.Manifest.Context)
	phaseResult, err := wg.Runner.RunExistingPhase(ctx, phaseID, ps.BeadID, phase.Title, prompt, exec)

	if phaseResult != nil {
		wg.progress.RecordPhaseComplete(phaseID, *phaseResult)
	}

	// Handle auto-decomposition when the loop signals a struggle.
	if err == nil && phaseResult != nil && phaseResult.Decompose && wg.shouldDecompose(phase) {
		_, decompErr := wg.decomposePhase(ctx, phaseID, phaseResult)
		if decompErr != nil {
			fmt.Fprintf(wg.logger(), "decomposition failed for %s: %v\n", phaseID, decompErr)
			// Fall through to record the phase as failed.
			wg.recordResult(phaseID, ps, phaseResult, fmt.Errorf("decomposition failed: %w", decompErr), done, failed, inFlight)
			return
		}
		// Mark original phase as decomposed and enqueue sub-phases.
		wg.mu.Lock()
		wg.State.SetPhaseState(phaseID, ps.BeadID, PhaseStatusDecomposed)
		done[phaseID] = true
		delete(inFlight, phaseID)
		wg.results = append(wg.results, WorkerResult{PhaseID: phaseID, BeadID: ps.BeadID})
		wg.progress.SaveState()
		wg.progress.ReportProgress()
		wg.mu.Unlock()
		// Signal hot-added phases to the scheduler.
		if wg.hotReload != nil {
			wg.mu.Lock()
			wg.hotReload.CheckHotAddedReady()
			wg.mu.Unlock()
		}
		return
	}

	if err == nil && wg.Committer != nil {
		if commitErr := wg.Committer.CommitPhase(ctx, wg.Nebula.Manifest.Nebula.Name, phaseID, phase.Title); commitErr != nil {
			fmt.Fprintf(wg.logger(), "warning: failed to commit phase %q: %v\n", phaseID, commitErr)
		}
	}

	var cp *Checkpoint
	if err == nil && phaseResult != nil && wg.Committer != nil {
		var cpErr error
		cp, cpErr = BuildCheckpoint(ctx, wg.Committer, phaseID, *phaseResult, wg.Nebula)
		if cpErr != nil {
			fmt.Fprintf(wg.logger(), "warning: failed to build checkpoint for %q: %v\n", phaseID, cpErr)
		}
	}

	if err == nil {
		action, gateErr := wg.Gater.PhaseGate(ctx, phase, cp)
		if gateErr != nil {
			fmt.Fprintf(wg.logger(), "warning: gate failed for phase %q: %v\n", phaseID, gateErr)
		}
		switch action {
		case GateActionAccept:
			// Fall through to recordResult.
		case GateActionReject:
			wg.recordResult(phaseID, ps, phaseResult, fmt.Errorf("phase %q rejected at gate", phaseID), done, failed, inFlight)
			wg.mu.Lock()
			wg.gateSignals = append(wg.gateSignals, gateSignal{phaseID: phaseID, action: GateActionReject})
			wg.mu.Unlock()
			return
		case GateActionRetry:
			wg.mu.Lock()
			delete(inFlight, phaseID)
			wg.State.SetPhaseState(phaseID, ps.BeadID, PhaseStatusInProgress)
			wg.progress.SaveState()
			wg.gateSignals = append(wg.gateSignals, gateSignal{phaseID: phaseID, action: GateActionRetry})
			wg.mu.Unlock()
			return
		case GateActionSkip:
			wg.recordResult(phaseID, ps, phaseResult, nil, done, failed, inFlight)
			wg.mu.Lock()
			wg.gateSignals = append(wg.gateSignals, gateSignal{phaseID: phaseID, action: GateActionSkip})
			wg.mu.Unlock()
			return
		}
	}

	wg.recordResult(phaseID, ps, phaseResult, err, done, failed, inFlight)

	// Publish entanglements and update fabric state on successful completion.
	if err == nil {
		wg.fabricPhaseComplete(ctx, phaseID, phaseResult)
	}
}

// recordResult updates state maps and persists state after a phase execution.
// Must NOT be called with wg.mu held.
func (wg *WorkerGroup) recordResult(
	phaseID string,
	ps *PhaseState,
	phaseResult *PhaseRunnerResult,
	err error,
	done, failed, inFlight map[string]bool,
) {
	wg.mu.Lock()
	defer wg.mu.Unlock()

	delete(inFlight, phaseID)
	wr := WorkerResult{PhaseID: phaseID, BeadID: ps.BeadID, Err: err}
	if phaseResult != nil {
		wg.State.TotalCostUSD += phaseResult.TotalCostUSD
	}
	if err == nil && phaseResult != nil && phaseResult.Report != nil {
		wr.Report = phaseResult.Report
		ps.Report = phaseResult.Report
	}
	wg.results = append(wg.results, wr)

	if err != nil {
		failed[phaseID] = true
		done[phaseID] = true
		wg.State.SetPhaseState(phaseID, ps.BeadID, PhaseStatusFailed)
	} else {
		done[phaseID] = true
		wg.State.SetPhaseState(phaseID, ps.BeadID, PhaseStatusDone)
	}
	wg.progress.SaveState()
	wg.progress.ReportProgress()

	if wg.hotReload != nil {
		wg.hotReload.CheckHotAddedReady()
	}
}

// recordFailure marks a phase as failed when it has no valid bead ID.
// Must NOT be called with wg.mu held.
func (wg *WorkerGroup) recordFailure(phaseID string) {
	wg.mu.Lock()
	wg.tracker.Failed()[phaseID] = true
	wg.tracker.Done()[phaseID] = true
	delete(wg.tracker.InFlight(), phaseID)
	wg.results = append(wg.results, WorkerResult{
		PhaseID: phaseID,
		Err:     fmt.Errorf("no bead ID for phase %q", phaseID),
	})
	wg.mu.Unlock()
}

// checkInterventions drains the intervention channel and returns the most
// significant pending intervention (stop > retry > pause > none).
func (wg *WorkerGroup) checkInterventions() InterventionKind {
	if wg.Watcher == nil {
		return ""
	}
	var latest InterventionKind
	for {
		select {
		case kind := <-wg.Watcher.Interventions:
			if kind == InterventionStop {
				return InterventionStop
			}
			if kind == InterventionRetry {
				wg.handleRetry()
				continue
			}
			if kind == InterventionPause {
				latest = InterventionPause
			}
		default:
			return latest
		}
	}
}

// handlePause blocks until the PAUSE file is removed from the nebula directory.
func (wg *WorkerGroup) handlePause() {
	pausePath := filepath.Join(wg.Nebula.Dir, "PAUSE")
	fmt.Fprintf(wg.logger(), "\n── Nebula paused ──────────────────────────────────\n")
	fmt.Fprintf(wg.logger(), "   Remove the PAUSE file to continue:\n")
	fmt.Fprintf(wg.logger(), "   rm %s\n", pausePath)
	fmt.Fprintf(wg.logger(), "───────────────────────────────────────────────────\n\n")

	if _, err := os.Stat(pausePath); os.IsNotExist(err) {
		return
	}

	for kind := range wg.Watcher.Interventions {
		if kind == InterventionResume {
			return
		}
		if kind == InterventionStop {
			wg.Watcher.SendIntervention(InterventionStop)
			return
		}
	}
}

// handleStop saves state, cleans up the STOP file, and prints a message.
func (wg *WorkerGroup) handleStop() {
	wg.mu.Lock()
	wg.progress.SaveState()
	wg.mu.Unlock()

	stopPath := filepath.Join(wg.Nebula.Dir, "STOP")
	if err := os.Remove(stopPath); err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to remove STOP file: %v\n", err)
	}

	fmt.Fprintf(wg.logger(), "\n── Nebula stopped by user ─────────────────────────\n")
	fmt.Fprintf(wg.logger(), "   State saved. Resume with: quasar nebula apply\n")
	fmt.Fprintf(wg.logger(), "───────────────────────────────────────────────────\n\n")
}

// handleRetry reads the RETRY file, resets the phase, and removes the file.
func (wg *WorkerGroup) handleRetry() {
	retryPath := filepath.Join(wg.Nebula.Dir, "RETRY")
	content, err := os.ReadFile(retryPath)
	if err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to read RETRY file: %v\n", err)
		return
	}

	phaseID := strings.TrimSpace(string(content))
	if phaseID == "" {
		fmt.Fprintf(wg.logger(), "warning: RETRY file is empty\n")
		_ = os.Remove(retryPath)
		return
	}

	if err := os.Remove(retryPath); err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to remove RETRY file: %v\n", err)
	}

	done := wg.tracker.Done()
	failed := wg.tracker.Failed()
	inFlight := wg.tracker.InFlight()

	wg.mu.Lock()
	defer wg.mu.Unlock()

	if !failed[phaseID] {
		fmt.Fprintf(wg.logger(), "warning: phase %q is not failed, ignoring retry\n", phaseID)
		return
	}

	delete(failed, phaseID)
	delete(done, phaseID)
	delete(inFlight, phaseID)

	ps := wg.State.Phases[phaseID]
	if ps != nil {
		wg.State.SetPhaseState(phaseID, ps.BeadID, PhaseStatusInProgress)
		wg.progress.SaveState()
	}

	fmt.Fprintf(wg.logger(), "\n── Retrying phase %q ──────────────────────────────\n\n", phaseID)
}

// processGateSignals handles pending gate signals after a batch completes.
// Returns true if the dispatch loop should stop, along with any error.
// Must NOT be called with wg.mu held.
func (wg *WorkerGroup) processGateSignals() (stop bool, err error) {
	wg.mu.Lock()
	signals := wg.drainGateSignals()
	wg.mu.Unlock()

	for _, sig := range signals {
		switch sig.action {
		case GateActionReject:
			wg.mu.Lock()
			wg.tracker.MarkRemainingSkipped(wg.Nebula.Phases, wg.State)
			wg.progress.SaveState()
			wg.mu.Unlock()
			return true, fmt.Errorf("phase %q rejected at gate", sig.phaseID)

		case GateActionSkip:
			wg.mu.Lock()
			wg.tracker.MarkRemainingSkipped(wg.Nebula.Phases, wg.State)
			wg.progress.SaveState()
			wg.mu.Unlock()
			return true, nil

		case GateActionRetry:
			// Phase already removed from inFlight; re-eligible next iteration.
		}
	}
	return false, nil
}

// shouldDecompose checks whether a phase is eligible for auto-decomposition.
// Decomposition is disabled for phases that were themselves produced by
// decomposition (to prevent infinite recursion), and when the manifest or
// per-phase override disables auto_decompose.
func (wg *WorkerGroup) shouldDecompose(phase *PhaseSpec) bool {
	if phase.Decomposed {
		return false
	}
	if wg.Invoker == nil {
		return false
	}
	// Per-phase override takes precedence over the manifest default.
	if phase.AutoDecompose != nil {
		return *phase.AutoDecompose
	}
	return wg.Nebula.Manifest.Execution.AutoDecompose
}

// decomposePhase invokes the architect to decompose a struggling phase and
// applies the resulting sub-phases to the DAG. It returns the IDs of the
// newly created sub-phases. Must NOT be called with wg.mu held.
func (wg *WorkerGroup) decomposePhase(ctx context.Context, phaseID string, result *PhaseRunnerResult) ([]string, error) {
	wg.mu.Lock()
	phasesByID := wg.tracker.PhasesByIDMap()
	phase := phasesByID[phaseID]
	nebSnap := wg.Nebula.Snapshot()
	wg.mu.Unlock()

	if phase == nil {
		return nil, fmt.Errorf("phase %q not found in tracker", phaseID)
	}

	req := ArchitectRequest{
		Mode:           ArchitectModeDecompose,
		UserPrompt:     phase.Body,
		Nebula:         nebSnap,
		PhaseID:        phaseID,
		StruggleReason: result.StruggleReason,
		CyclesUsed:     result.CyclesUsed,
		AllFindings:    result.AllFindings,
		CostSoFar:      result.TotalCostUSD,
	}

	decomp, err := RunDecompose(ctx, wg.Invoker, req)
	if err != nil {
		return nil, fmt.Errorf("running decompose for %s: %w", phaseID, err)
	}

	// Build the DecomposeOp from the architect result.
	op := DecomposeOp{
		OriginalPhaseID: phaseID,
		SubPhases:       make([]SubPhaseEntry, len(decomp.SubPhases)),
	}
	for i, sp := range decomp.SubPhases {
		sp.PhaseSpec.Decomposed = true
		op.SubPhases[i] = SubPhaseEntry{
			Spec:     sp.PhaseSpec,
			Body:     sp.Body,
			Filename: sp.Filename,
		}
	}

	wg.mu.Lock()
	defer wg.mu.Unlock()

	// Build live graph if hot-reload state is available, otherwise build from phases.
	var liveGraph *dag.DAG
	var livePhasesMap map[string]*PhaseSpec
	if wg.hotReload != nil && wg.hotReload.liveGraph != nil {
		liveGraph = wg.hotReload.liveGraph
		livePhasesMap = wg.hotReload.livePhasesByID
	}
	if liveGraph == nil {
		// Fallback: build from phases.
		g, _ := phasesToDAG(wg.Nebula.Phases)
		liveGraph = g
		livePhasesMap = PhasesByID(wg.Nebula.Phases)
	}

	subIDs, err := ApplyDecompositionToNebula(wg.Nebula, liveGraph, op, livePhasesMap)
	if err != nil {
		return nil, fmt.Errorf("applying decomposition for %s: %w", phaseID, err)
	}

	// Set fabric state for the original phase.
	if wg.Fabric != nil {
		if stateErr := wg.Fabric.SetPhaseState(ctx, phaseID, fabric.StateDecomposed); stateErr != nil {
			fmt.Fprintf(wg.logger(), "warning: failed to set fabric state for decomposed phase %s: %v\n", phaseID, stateErr)
		}
	}

	// Create beads and state entries for sub-phases.
	for _, sp := range op.SubPhases {
		beadID := ""
		if wg.BeadsClient != nil {
			wg.mu.Unlock()
			var createErr error
			beadID, createErr = wg.BeadsClient.Create(ctx, sp.Spec.Title, beads.CreateOpts{
				Description: sp.Body,
				Type:        sp.Spec.Type,
				Labels:      sp.Spec.Labels,
				Assignee:    sp.Spec.Assignee,
				Priority:    priorityStr(sp.Spec.Priority),
			})
			wg.mu.Lock()
			if createErr != nil {
				fmt.Fprintf(wg.logger(), "warning: failed to create bead for sub-phase %q: %v\n", sp.Spec.ID, createErr)
				continue
			}
		}
		wg.State.SetPhaseState(sp.Spec.ID, beadID, PhaseStatusPending)

		// Set fabric state for sub-phase.
		if wg.Fabric != nil {
			if stateErr := wg.Fabric.SetPhaseState(ctx, sp.Spec.ID, fabric.StateQueued); stateErr != nil {
				fmt.Fprintf(wg.logger(), "warning: failed to set fabric state for sub-phase %s: %v\n", sp.Spec.ID, stateErr)
			}
		}
	}

	wg.progress.SaveState()
	wg.progress.ReportProgress()

	// Notify TUI of hot-added sub-phases.
	if wg.OnHotAdd != nil {
		for _, sp := range op.SubPhases {
			wg.OnHotAdd(sp.Spec.ID, sp.Spec.Title, sp.Spec.DependsOn)
		}
	}

	// Post a hail if configured.
	if wg.OnHail != nil {
		wg.OnHail(phaseID, fabric.Discovery{
			Kind:   "decomposition",
			Detail: fmt.Sprintf("Phase %q decomposed into %d sub-phases: %s (reason: %s)", phaseID, len(subIDs), strings.Join(subIDs, ", "), result.StruggleReason),
		})
	}

	fmt.Fprintf(wg.logger(), "phase %q decomposed into %d sub-phases: %s\n", phaseID, len(subIDs), strings.Join(subIDs, ", "))

	return subIDs, nil
}

