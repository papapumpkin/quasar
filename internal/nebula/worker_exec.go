package nebula

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
