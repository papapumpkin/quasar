package nebula

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// PhaseRunnerResult holds the outcome of a single phase execution.
type PhaseRunnerResult struct {
	TotalCostUSD float64
	CyclesUsed   int
	Report       *ReviewReport
}

// PhaseRunner is the interface for executing a phase (satisfied by loop.Loop).
type PhaseRunner interface {
	RunExistingPhase(ctx context.Context, beadID, phaseDescription string, exec ResolvedExecution) (*PhaseRunnerResult, error)
	GenerateCheckpoint(ctx context.Context, beadID, phaseDescription string) (string, error)
}

// ProgressFunc is called after each phase status change to report progress.
// Parameters: completed, total, openBeads, closedBeads, totalCostUSD.
type ProgressFunc func(completed, total, openBeads, closedBeads int, totalCostUSD float64)

// gateSignal communicates a gate decision from a worker goroutine back to the dispatch loop.
type gateSignal struct {
	phaseID string
	action  GateAction
}

// WorkerGroup executes phases in dependency order using a pool of workers.
type WorkerGroup struct {
	Runner       PhaseRunner
	Nebula       *Nebula
	State        *State
	MaxWorkers   int
	Watcher      *Watcher      // nil = no in-flight editing
	Committer    GitCommitter   // nil = no phase-boundary commits
	Gater        Gater          // nil = trust mode (no prompts)
	GlobalCycles int
	GlobalBudget float64
	GlobalModel  string
	OnProgress   ProgressFunc // optional progress callback

	mu          sync.Mutex
	results     []WorkerResult
	gateSignals []gateSignal // collected after each batch
}

// buildPhasePrompt prepends nebula context (goals, constraints) to the phase body.
func buildPhasePrompt(phase *PhaseSpec, ctx *Context) string {
	if ctx == nil || (len(ctx.Goals) == 0 && len(ctx.Constraints) == 0) {
		return phase.Body
	}

	var sb strings.Builder
	sb.WriteString("PROJECT CONTEXT:\n")
	if len(ctx.Goals) > 0 {
		sb.WriteString("Goals:\n")
		for _, g := range ctx.Goals {
			sb.WriteString("- ")
			sb.WriteString(g)
			sb.WriteString("\n")
		}
	}
	if len(ctx.Constraints) > 0 {
		sb.WriteString("Constraints:\n")
		for _, c := range ctx.Constraints {
			sb.WriteString("- ")
			sb.WriteString(c)
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\nPHASE:\n")
	sb.WriteString(phase.Body)
	return sb.String()
}

// reportProgress calls the OnProgress callback (if set) with current counts.
// Must be called with wg.mu held.
func (wg *WorkerGroup) reportProgress() {
	if wg.OnProgress == nil {
		return
	}
	total := len(wg.Nebula.Phases)
	var completed, open, closed int
	for _, ps := range wg.State.Phases {
		switch ps.Status {
		case PhaseStatusDone:
			closed++
			completed++
		case PhaseStatusFailed:
			closed++
			completed++
		case PhaseStatusSkipped:
			closed++
			completed++
		case PhaseStatusInProgress, PhaseStatusCreated:
			open++
		case PhaseStatusPending:
			// Pending phases have no bead yet — not counted in open or closed.
			// They still contribute to total (via len(wg.Nebula.Phases)).
		}
	}
	wg.OnProgress(completed, total, open, closed, wg.State.TotalCostUSD)
}

// initPhaseState builds lookup maps from the current nebula and state.
// It returns a phase-spec index, and sets of already-done and already-failed phase IDs.
// Failed phases are also marked done so that graph.Ready() can unblock dependents.
func (wg *WorkerGroup) initPhaseState() (phasesByID map[string]*PhaseSpec, done, failed map[string]bool) {
	phasesByID = make(map[string]*PhaseSpec)
	for i := range wg.Nebula.Phases {
		phasesByID[wg.Nebula.Phases[i].ID] = &wg.Nebula.Phases[i]
	}

	done = make(map[string]bool)
	failed = make(map[string]bool)
	for id, ps := range wg.State.Phases {
		if ps.Status == PhaseStatusDone {
			done[id] = true
		}
		if ps.Status == PhaseStatusFailed {
			failed[id] = true
			done[id] = true
		}
	}
	return phasesByID, done, failed
}

// filterEligible returns phase IDs from ready that are not in-flight, not failed,
// and not blocked by a failed dependency.
// Must be called with wg.mu held.
func filterEligible(ready []string, inFlight, failed map[string]bool, graph *Graph) []string {
	var eligible []string
	for _, id := range ready {
		if inFlight[id] || failed[id] {
			continue
		}
		if hasFailedDep(id, failed, graph) {
			continue
		}
		eligible = append(eligible, id)
	}
	return eligible
}

// hasFailedDep reports whether any direct dependency of phaseID has failed.
func hasFailedDep(phaseID string, failed map[string]bool, graph *Graph) bool {
	deps, ok := graph.adjacency[phaseID]
	if !ok {
		return false
	}
	for dep := range deps {
		if failed[dep] {
			return true
		}
	}
	return false
}

// resolveGateMode determines the effective gate mode for a phase.
// Returns GateModeTrust if no Gater is configured (nil-safe).
func (wg *WorkerGroup) resolveGateMode(phase *PhaseSpec) GateMode {
	if wg.Gater == nil {
		return GateModeTrust
	}
	return ResolveGate(wg.Nebula.Manifest.Execution, *phase)
}

// applyGate handles the gate check after a phase completes successfully.
// It resolves the gate mode, optionally renders the checkpoint, and prompts the
// human if required. Returns the GateAction taken.
func (wg *WorkerGroup) applyGate(ctx context.Context, phase *PhaseSpec, cp *Checkpoint) GateAction {
	mode := wg.resolveGateMode(phase)

	switch mode {
	case GateModeTrust:
		return GateActionAccept

	case GateModeWatch:
		// Render checkpoint but don't block.
		if cp != nil {
			RenderCheckpoint(os.Stderr, cp)
		}
		return GateActionAccept

	case GateModeReview, GateModeApprove:
		// Render checkpoint and prompt for decision.
		if cp != nil {
			RenderCheckpoint(os.Stderr, cp)
		}
		action, err := wg.Gater.Prompt(ctx, cp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: gate prompt failed: %v (defaulting to accept)\n", err)
			return GateActionAccept
		}
		return action

	default:
		// Unknown gate mode — treat as trust.
		return GateActionAccept
	}
}

// executePhase runs a single phase and records the result.
// It is intended to be called as a goroutine from the dispatch loop.
func (wg *WorkerGroup) executePhase(
	ctx context.Context,
	phaseID string,
	phasesByID map[string]*PhaseSpec,
	done, failed, inFlight map[string]bool,
) {
	phase := phasesByID[phaseID]
	ps := wg.State.Phases[phaseID]
	if phase == nil || ps == nil || ps.BeadID == "" {
		wg.recordFailure(phaseID, done, failed, inFlight)
		return
	}

	wg.mu.Lock()
	wg.State.SetPhaseState(phaseID, ps.BeadID, PhaseStatusInProgress)
	if err := SaveState(wg.Nebula.Dir, wg.State); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save state: %v\n", err)
	}
	wg.reportProgress()
	wg.mu.Unlock()

	exec := ResolveExecution(wg.GlobalCycles, wg.GlobalBudget, wg.GlobalModel, &wg.Nebula.Manifest.Execution, phase)
	prompt := buildPhasePrompt(phase, &wg.Nebula.Manifest.Context)
	phaseResult, err := wg.Runner.RunExistingPhase(ctx, ps.BeadID, prompt, exec)

	// Commit phase changes on success so reviewers see clean diffs.
	if err == nil && wg.Committer != nil {
		if commitErr := wg.Committer.CommitPhase(ctx, wg.Nebula.Manifest.Nebula.Name, phaseID); commitErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to commit phase %q: %v\n", phaseID, commitErr)
		}
	}

	// Build checkpoint after successful phase completion.
	var cp *Checkpoint
	if err == nil && phaseResult != nil && wg.Committer != nil {
		var cpErr error
		cp, cpErr = BuildCheckpoint(ctx, wg.Committer, phaseID, *phaseResult, wg.Nebula)
		if cpErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to build checkpoint for %q: %v\n", phaseID, cpErr)
		}
	}

	// Apply gate logic after successful phase completion.
	if err == nil {
		action := wg.applyGate(ctx, phase, cp)
		switch action {
		case GateActionAccept:
			// Continue normally — fall through to recordResult.
		case GateActionReject:
			// Mark as failed and signal stop.
			wg.recordResult(phaseID, ps, phaseResult, fmt.Errorf("phase %q rejected at gate", phaseID), done, failed, inFlight)
			wg.mu.Lock()
			wg.gateSignals = append(wg.gateSignals, gateSignal{phaseID: phaseID, action: GateActionReject})
			wg.mu.Unlock()
			return
		case GateActionRetry:
			// Undo the in-flight/done marking so the phase can be re-queued.
			wg.mu.Lock()
			delete(inFlight, phaseID)
			wg.State.SetPhaseState(phaseID, ps.BeadID, PhaseStatusInProgress)
			if saveErr := SaveState(wg.Nebula.Dir, wg.State); saveErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to save state: %v\n", saveErr)
			}
			wg.gateSignals = append(wg.gateSignals, gateSignal{phaseID: phaseID, action: GateActionRetry})
			wg.mu.Unlock()
			return
		case GateActionSkip:
			// Record success for this phase but signal stop.
			wg.recordResult(phaseID, ps, phaseResult, nil, done, failed, inFlight)
			wg.mu.Lock()
			wg.gateSignals = append(wg.gateSignals, gateSignal{phaseID: phaseID, action: GateActionSkip})
			wg.mu.Unlock()
			return
		}
	}

	wg.recordResult(phaseID, ps, phaseResult, err, done, failed, inFlight)
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
		done[phaseID] = true // unblock dependents (blocked-by-failure filter skips them)
		wg.State.SetPhaseState(phaseID, ps.BeadID, PhaseStatusFailed)
	} else {
		done[phaseID] = true
		wg.State.SetPhaseState(phaseID, ps.BeadID, PhaseStatusDone)
	}
	if err := SaveState(wg.Nebula.Dir, wg.State); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save state: %v\n", err)
	}
	wg.reportProgress()
}

// recordFailure marks a phase as failed when it has no valid bead ID.
// Must NOT be called with wg.mu held.
func (wg *WorkerGroup) recordFailure(phaseID string, done, failed, inFlight map[string]bool) {
	wg.mu.Lock()
	failed[phaseID] = true
	done[phaseID] = true
	delete(inFlight, phaseID)
	wg.results = append(wg.results, WorkerResult{
		PhaseID: phaseID,
		Err:     fmt.Errorf("no bead ID for phase %q", phaseID),
	})
	wg.mu.Unlock()
}

// markRemainingSkipped sets all pending/created phases to skipped status.
// Must be called with wg.mu held.
func (wg *WorkerGroup) markRemainingSkipped(done map[string]bool) {
	for _, phase := range wg.Nebula.Phases {
		if done[phase.ID] {
			continue
		}
		ps := wg.State.Phases[phase.ID]
		if ps == nil {
			continue
		}
		if ps.Status == PhaseStatusPending || ps.Status == PhaseStatusCreated {
			wg.State.SetPhaseState(phase.ID, ps.BeadID, PhaseStatusSkipped)
		}
	}
}

// drainGateSignals returns and clears any pending gate signals.
// Must be called with wg.mu held.
func (wg *WorkerGroup) drainGateSignals() []gateSignal {
	signals := wg.gateSignals
	wg.gateSignals = nil
	return signals
}

// checkInterventions drains the intervention channel and returns the most
// significant pending intervention (stop > pause > none).
func (wg *WorkerGroup) checkInterventions() InterventionKind {
	if wg.Watcher == nil {
		return ""
	}
	var latest InterventionKind
	for {
		select {
		case kind := <-wg.Watcher.Interventions:
			// Stop takes priority over pause.
			if kind == InterventionStop {
				return InterventionStop
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
// It watches the Interventions channel for a resume signal.
func (wg *WorkerGroup) handlePause() {
	pausePath := filepath.Join(wg.Nebula.Dir, "PAUSE")
	fmt.Fprintf(os.Stderr, "\n── Nebula paused ──────────────────────────────────\n")
	fmt.Fprintf(os.Stderr, "   Remove the PAUSE file to continue:\n")
	fmt.Fprintf(os.Stderr, "   rm %s\n", pausePath)
	fmt.Fprintf(os.Stderr, "───────────────────────────────────────────────────\n\n")

	// Check if PAUSE was already removed before we started waiting.
	if _, err := os.Stat(pausePath); os.IsNotExist(err) {
		return
	}

	// Block until resume signal or stop signal.
	for kind := range wg.Watcher.Interventions {
		if kind == InterventionResume {
			return
		}
		if kind == InterventionStop {
			// Stop overrides pause; re-send so the main loop picks it up.
			wg.Watcher.SendIntervention(InterventionStop)
			return
		}
	}
}

// handleStop saves state, cleans up the STOP file, and prints a message.
func (wg *WorkerGroup) handleStop() {
	wg.mu.Lock()
	if err := SaveState(wg.Nebula.Dir, wg.State); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save state: %v\n", err)
	}
	wg.mu.Unlock()

	// Clean up the STOP file.
	stopPath := filepath.Join(wg.Nebula.Dir, "STOP")
	if err := os.Remove(stopPath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to remove STOP file: %v\n", err)
	}

	fmt.Fprintf(os.Stderr, "\n── Nebula stopped by user ─────────────────────────\n")
	fmt.Fprintf(os.Stderr, "   State saved. Resume with: quasar nebula apply\n")
	fmt.Fprintf(os.Stderr, "───────────────────────────────────────────────────\n\n")
}

// globalGateMode returns the effective gate mode from the manifest execution config.
// If no Gater is configured, returns GateModeTrust.
func (wg *WorkerGroup) globalGateMode() GateMode {
	if wg.Gater == nil {
		return GateModeTrust
	}
	if wg.Nebula.Manifest.Execution.Gate != "" {
		return wg.Nebula.Manifest.Execution.Gate
	}
	return GateModeTrust
}

// gatePlan displays the execution plan and prompts for approval when in approve mode.
// Returns nil if the plan is approved or the mode doesn't require plan gating.
// Returns ErrPlanRejected if the user rejects the plan.
func (wg *WorkerGroup) gatePlan(ctx context.Context, graph *Graph) error {
	mode := wg.globalGateMode()
	if mode != GateModeApprove {
		return nil
	}

	waves, err := graph.ComputeWaves()
	if err != nil {
		return fmt.Errorf("failed to compute execution waves: %w", err)
	}

	RenderPlan(os.Stderr, wg.Nebula.Manifest.Nebula.Name, waves, len(wg.Nebula.Phases), wg.GlobalBudget, mode)

	// Build a plan-level checkpoint (no diff, just plan metadata).
	cp := &Checkpoint{
		PhaseID:    "_plan",
		PhaseTitle: "Execution Plan",
		NebulaName: wg.Nebula.Manifest.Nebula.Name,
	}

	action, err := wg.Gater.Prompt(ctx, cp)
	if err != nil {
		return fmt.Errorf("plan gate prompt failed: %w", err)
	}

	switch action {
	case GateActionAccept:
		return nil
	default:
		return ErrPlanRejected
	}
}

// Run dispatches phases respecting dependency order.
// It returns after all eligible phases have been executed or the context is canceled.
// If a STOP file is detected, it returns ErrManualStop after finishing current phases.
// Gate signals (reject, skip) from phase boundaries also cause graceful termination.
// In approve mode, the execution plan is displayed and requires human approval before
// any phases begin executing.
func (wg *WorkerGroup) Run(ctx context.Context) ([]WorkerResult, error) {
	if wg.MaxWorkers <= 0 {
		wg.MaxWorkers = 1
	}
	phasesByID, done, failed := wg.initPhaseState()
	graph := NewGraph(wg.Nebula.Phases)

	// Gate the execution plan before dispatching any phases.
	if err := wg.gatePlan(ctx, graph); err != nil {
		return nil, err
	}

	inFlight := make(map[string]bool)
	sem := make(chan struct{}, wg.MaxWorkers)
	var wgSync sync.WaitGroup

	for ctx.Err() == nil {
		// Check for interventions between batches.
		switch wg.checkInterventions() {
		case InterventionStop:
			wg.handleStop()
			wg.mu.Lock()
			results := wg.results
			wg.mu.Unlock()
			return results, ErrManualStop
		case InterventionPause:
			wg.handlePause()
			// After resume, re-check for stop.
			if wg.checkInterventions() == InterventionStop {
				wg.handleStop()
				wg.mu.Lock()
				results := wg.results
				wg.mu.Unlock()
				return results, ErrManualStop
			}
		}

		wg.mu.Lock()
		eligible := filterEligible(graph.Ready(done), inFlight, failed, graph)
		anyInFlight := len(inFlight) > 0
		wg.mu.Unlock()

		if len(eligible) == 0 {
			if !anyInFlight {
				break
			}
			wgSync.Wait()

			// Process gate signals after batch completes.
			stop, retErr := wg.processGateSignals(done)
			if stop {
				return wg.collectResults(), retErr
			}
			continue
		}

		for _, id := range eligible {
			if ctx.Err() != nil {
				break
			}
			wg.mu.Lock()
			inFlight[id] = true
			wg.mu.Unlock()

			sem <- struct{}{}
			wgSync.Add(1)
			go func(phaseID string) {
				defer func() { <-sem; wgSync.Done() }()
				wg.executePhase(ctx, phaseID, phasesByID, done, failed, inFlight)
			}(id)
		}
		wgSync.Wait() // wait for batch before looking for more ready phases

		// Process gate signals after batch completes.
		stop, retErr := wg.processGateSignals(done)
		if stop {
			return wg.collectResults(), retErr
		}
	}
	wgSync.Wait()

	wg.mu.Lock()
	results := wg.results
	wg.mu.Unlock()
	return results, nil
}

// processGateSignals handles pending gate signals after a batch completes.
// Returns true if the dispatch loop should stop, along with any error.
// Must NOT be called with wg.mu held.
func (wg *WorkerGroup) processGateSignals(done map[string]bool) (stop bool, err error) {
	wg.mu.Lock()
	signals := wg.drainGateSignals()
	wg.mu.Unlock()

	for _, sig := range signals {
		switch sig.action {
		case GateActionReject:
			wg.mu.Lock()
			wg.markRemainingSkipped(done)
			if saveErr := SaveState(wg.Nebula.Dir, wg.State); saveErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to save state: %v\n", saveErr)
			}
			wg.mu.Unlock()
			return true, fmt.Errorf("phase %q rejected at gate", sig.phaseID)

		case GateActionSkip:
			wg.mu.Lock()
			wg.markRemainingSkipped(done)
			if saveErr := SaveState(wg.Nebula.Dir, wg.State); saveErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to save state: %v\n", saveErr)
			}
			wg.mu.Unlock()
			return true, nil

		case GateActionRetry:
			// Phase was already removed from inFlight in executePhase.
			// It will be re-eligible in the next iteration.
		}
	}
	return false, nil
}

// collectResults returns a snapshot of the current results.
func (wg *WorkerGroup) collectResults() []WorkerResult {
	wg.mu.Lock()
	defer wg.mu.Unlock()
	return wg.results
}
