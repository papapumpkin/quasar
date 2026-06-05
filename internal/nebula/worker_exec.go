package nebula

import (
	"context"
	"fmt"
	"strings"

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

	// When resume is enabled, attempt to load a checkpoint and route through
	// RunFromCheckpoint instead of starting a fresh loop.
	var phaseResult *PhaseRunnerResult
	var err error
	if cp := wg.tryLoadCheckpoint(ctx, phaseID); cp != nil {
		phaseResult, err = wg.Runner.RunFromCheckpoint(ctx, cp, phaseID, ps.BeadID, phase.Title, prompt, exec)
	} else {
		phaseResult, err = wg.Runner.RunExistingPhase(ctx, phaseID, ps.BeadID, phase.Title, prompt, exec)
	}

	if phaseResult != nil {
		wg.progress.RecordPhaseComplete(phaseID, *phaseResult)
	}

	// Handle auto-decomposition when the loop signals a struggle.
	if err == nil && phaseResult != nil && phaseResult.Decompose {
		if wg.shouldDecompose(phase) {
			_, decompErr := wg.decomposePhase(ctx, phaseID, phaseResult)
			if decompErr != nil {
				fmt.Fprintf(wg.logger(), "decomposition failed for %s: %v\n", phaseID, decompErr)
				// Fall through to record the phase as failed.
				wg.recordResult(ctx, phaseID, ps, phaseResult, fmt.Errorf("decomposition failed: %w", decompErr), done, failed, inFlight)
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
			return
		}
		// The loop exited early due to a struggle signal, but decomposition
		// is not enabled for this phase. Mark as failed — the phase did not
		// complete its review cycle.
		wg.recordResult(ctx, phaseID, ps, phaseResult, fmt.Errorf("phase %q exited due to struggle but auto-decomposition is disabled", phaseID), done, failed, inFlight)
		return
	}

	if err == nil && wg.Committer != nil {
		if commitErr := wg.Committer.CommitPhase(ctx, wg.Nebula.Manifest.Nebula.Name, phaseID, phase.Title, phase.Type); commitErr != nil {
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
			wg.recordResult(ctx, phaseID, ps, phaseResult, fmt.Errorf("phase %q rejected at gate", phaseID), done, failed, inFlight)
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
			wg.recordResult(ctx, phaseID, ps, phaseResult, nil, done, failed, inFlight)
			wg.mu.Lock()
			wg.gateSignals = append(wg.gateSignals, gateSignal{phaseID: phaseID, action: GateActionSkip})
			wg.mu.Unlock()
			return
		}
	}

	wg.recordResult(ctx, phaseID, ps, phaseResult, err, done, failed, inFlight)

	// Publish entanglements and update fabric state on successful completion.
	if err == nil {
		wg.fabricPhaseComplete(ctx, phaseID, phaseResult)
	}
}

// recordResult updates state maps and persists state after a phase execution.
// Must NOT be called with wg.mu held.
func (wg *WorkerGroup) recordResult(
	ctx context.Context,
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
	// Populate failure context for healing analysis.
	if err != nil && phaseResult != nil {
		wr.TaskResult = phaseResult
	}
	wg.results = append(wg.results, wr)

	if err != nil {
		failed[phaseID] = true
		done[phaseID] = true
		wg.State.SetPhaseState(phaseID, ps.BeadID, PhaseStatusFailed)

		// Trigger healing asynchronously when policy allows.
		if wg.healingPolicy.Enabled && phaseResult != nil {
			go wg.attemptHealing(ctx, phaseID, err, phaseResult)
		}
	} else {
		done[phaseID] = true
		wg.State.SetPhaseState(phaseID, ps.BeadID, PhaseStatusDone)
	}
	wg.progress.SaveState()
	wg.progress.ReportProgress()
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
			if sig.reason != "" {
				return true, fmt.Errorf("phase %q failed: %s", sig.phaseID, sig.reason)
			}
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

	// Apply decomposition under lock.
	wg.mu.Lock()

	// Build graph from phases.
	g, _ := phasesToDAG(wg.Nebula.Phases)
	liveGraph := g
	livePhasesMap := PhasesByID(wg.Nebula.Phases)

	subIDs, err := ApplyDecompositionToNebula(wg.Nebula, liveGraph, op, livePhasesMap)
	if err != nil {
		wg.mu.Unlock()
		return nil, fmt.Errorf("applying decomposition for %s: %w", phaseID, err)
	}
	wg.mu.Unlock()

	// Set fabric state for the original phase (no lock needed for fabric RPCs).
	if wg.Fabric != nil {
		if stateErr := wg.Fabric.SetPhaseState(ctx, phaseID, fabric.StateDecomposed); stateErr != nil {
			fmt.Fprintf(wg.logger(), "warning: failed to set fabric state for decomposed phase %s: %v\n", phaseID, stateErr)
		}
	}

	// Build tracking entries for sub-phases. Each sub-phase uses its own ID
	// as the tracking key (no external bead system).
	type beadResult struct {
		specID string
		beadID string
		ok     bool
	}
	var beadResults []beadResult
	for _, sp := range op.SubPhases {
		beadResults = append(beadResults, beadResult{
			specID: sp.Spec.ID,
			beadID: sp.Spec.ID,
			ok:     true,
		})
	}

	// Apply tracking results and fabric state under lock.
	wg.mu.Lock()
	for _, br := range beadResults {
		if !br.ok {
			continue
		}
		wg.State.SetPhaseState(br.specID, br.beadID, PhaseStatusPending)

		// Set fabric state for sub-phase.
		if wg.Fabric != nil {
			if stateErr := wg.Fabric.SetPhaseState(ctx, br.specID, fabric.StateQueued); stateErr != nil {
				fmt.Fprintf(wg.logger(), "warning: failed to set fabric state for sub-phase %s: %v\n", br.specID, stateErr)
			}
		}
	}

	wg.progress.SaveState()
	wg.progress.ReportProgress()
	wg.mu.Unlock()

	// Notify TUI of hot-added sub-phases (callbacks must not hold the lock).
	if wg.OnHotAdd != nil {
		for _, sp := range op.SubPhases {
			wg.OnHotAdd(sp.Spec.ID, sp.Spec.Title, sp.Spec.DependsOn)
		}
	}

	fmt.Fprintf(wg.logger(), "phase %q decomposed into %d sub-phases: %s\n", phaseID, len(subIDs), strings.Join(subIDs, ", "))

	return subIDs, nil
}

// tryLoadCheckpoint attempts to load and validate a checkpoint for the given
// phase. Returns the opaque checkpoint data if valid, or nil if no checkpoint
// exists, validation fails, or resume is not enabled.
//
// When the phase is already done or failed (stale checkpoint), the checkpoint
// file is removed and nil is returned.
func (wg *WorkerGroup) tryLoadCheckpoint(ctx context.Context, phaseID string) any {
	if !wg.ResumeEnabled || wg.CheckpointDir == "" || wg.CheckpointLoader == nil {
		return nil
	}

	// Check phase state — only in-progress phases are candidates for resume.
	// Done or failed phases have stale checkpoints that should be cleaned up.
	ps := wg.State.Phases[phaseID]
	if ps != nil && (ps.Status == PhaseStatusDone || ps.Status == PhaseStatusFailed) {
		wg.removeCheckpoint(phaseID)
		return nil
	}

	cp, err := wg.CheckpointLoader(wg.CheckpointDir, phaseID)
	if err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to load checkpoint for %q: %v\n", phaseID, err)
		return nil
	}
	if cp == nil {
		return nil // no checkpoint exists
	}

	// Validate the checkpoint against the current git SHA.
	if wg.CheckpointValidator != nil && wg.GitSHAFunc != nil {
		gitSHA, shaErr := wg.GitSHAFunc(ctx)
		if shaErr != nil {
			fmt.Fprintf(wg.logger(), "warning: failed to get git SHA for checkpoint validation of %q: %v\n", phaseID, shaErr)
			wg.removeCheckpoint(phaseID)
			return nil
		}
		if valErr := wg.CheckpointValidator(cp, gitSHA); valErr != nil {
			fmt.Fprintf(wg.logger(), "warning: checkpoint for %q is invalid, starting fresh: %v\n", phaseID, valErr)
			wg.removeCheckpoint(phaseID)
			return nil
		}
	}

	fmt.Fprintf(wg.logger(), "resuming phase %q from checkpoint\n", phaseID)
	return cp
}

// removeCheckpoint removes a stale checkpoint file, logging any errors.
func (wg *WorkerGroup) removeCheckpoint(phaseID string) {
	if wg.CheckpointRemover == nil || wg.CheckpointDir == "" {
		return
	}
	if err := wg.CheckpointRemover(wg.CheckpointDir, phaseID); err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to remove stale checkpoint for %q: %v\n", phaseID, err)
	}
}
