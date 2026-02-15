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

// WorkerGroup executes phases in dependency order using a pool of workers.
type WorkerGroup struct {
	Runner       PhaseRunner
	Nebula       *Nebula
	State        *State
	MaxWorkers   int
	Watcher      *Watcher      // nil = no in-flight editing
	Committer    GitCommitter   // nil = no phase-boundary commits
	GlobalCycles int
	GlobalBudget float64
	GlobalModel  string
	OnProgress   ProgressFunc // optional progress callback

	mu      sync.Mutex
	results []WorkerResult
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

	// Build and render checkpoint after successful phase completion.
	if err == nil && phaseResult != nil && wg.Committer != nil {
		cp, cpErr := BuildCheckpoint(ctx, wg.Committer, phaseID, *phaseResult, wg.Nebula)
		if cpErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to build checkpoint for %q: %v\n", phaseID, cpErr)
		} else {
			RenderCheckpoint(os.Stderr, cp)
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

// Run dispatches phases respecting dependency order.
// It returns after all eligible phases have been executed or the context is canceled.
// If a STOP file is detected, it returns ErrManualStop after finishing current phases.
func (wg *WorkerGroup) Run(ctx context.Context) ([]WorkerResult, error) {
	if wg.MaxWorkers <= 0 {
		wg.MaxWorkers = 1
	}
	phasesByID, done, failed := wg.initPhaseState()
	graph := NewGraph(wg.Nebula.Phases)
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
	}
	wgSync.Wait()

	wg.mu.Lock()
	results := wg.results
	wg.mu.Unlock()
	return results, nil
}
