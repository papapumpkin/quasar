package nebula

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/papapumpkin/quasar/internal/beads"
)

// PhaseRunnerResult holds the outcome of a single phase execution.
type PhaseRunnerResult struct {
	TotalCostUSD float64
	CyclesUsed   int
	Report       *ReviewReport
}

// PhaseRunner is the interface for executing a phase (satisfied by loop.Loop).
type PhaseRunner interface {
	RunExistingPhase(ctx context.Context, phaseID, beadID, phaseDescription string, exec ResolvedExecution) (*PhaseRunnerResult, error)
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

// phaseLoopHandle tracks a running phase's refactor channel so that mid-run
// edits can be signaled to the loop without interrupting the current cycle.
type phaseLoopHandle struct {
	RefactorCh chan<- string
}

// HotAddFunc is called after a new phase is dynamically inserted into the DAG.
// Parameters: phaseID, title, dependsOn.
type HotAddFunc func(phaseID, title string, dependsOn []string)

// Option configures a WorkerGroup.
type Option func(*WorkerGroup)

// WithRunner sets the phase runner. Required before calling Run, but may be
// set after construction when the runner depends on the WorkerGroup itself.
func WithRunner(r PhaseRunner) Option {
	return func(wg *WorkerGroup) { wg.Runner = r }
}

// WithMaxWorkers sets the maximum number of concurrent phase workers.
func WithMaxWorkers(n int) Option {
	return func(wg *WorkerGroup) { wg.MaxWorkers = n }
}

// WithWatcher enables in-flight file watching for live edits.
func WithWatcher(w *Watcher) Option {
	return func(wg *WorkerGroup) { wg.Watcher = w }
}

// WithCommitter enables phase-boundary git commits.
func WithCommitter(c GitCommitter) Option {
	return func(wg *WorkerGroup) { wg.Committer = c }
}

// WithGater sets the gate prompt handler. Nil means trust mode.
func WithGater(g Gater) Option {
	return func(wg *WorkerGroup) { wg.Gater = g }
}

// WithDashboard enables dashboard output coordination in watch mode.
func WithDashboard(d *Dashboard) Option {
	return func(wg *WorkerGroup) { wg.Dashboard = d }
}

// WithBeadsClient sets the beads client for hot-added phase bead creation.
func WithBeadsClient(c beads.Client) Option {
	return func(wg *WorkerGroup) { wg.BeadsClient = c }
}

// WithGlobalCycles sets the default max review cycles for phases.
func WithGlobalCycles(n int) Option {
	return func(wg *WorkerGroup) { wg.GlobalCycles = n }
}

// WithGlobalBudget sets the default max budget (USD) for phases.
func WithGlobalBudget(b float64) Option {
	return func(wg *WorkerGroup) { wg.GlobalBudget = b }
}

// WithGlobalModel sets the default model override for phases.
func WithGlobalModel(m string) Option {
	return func(wg *WorkerGroup) { wg.GlobalModel = m }
}

// WithOnProgress sets a callback invoked after each phase status change.
func WithOnProgress(f ProgressFunc) Option {
	return func(wg *WorkerGroup) { wg.OnProgress = f }
}

// WithOnRefactor sets a callback invoked when a refactor is pending or dispatched.
func WithOnRefactor(f func(phaseID string, pending bool)) Option {
	return func(wg *WorkerGroup) { wg.OnRefactor = f }
}

// WithOnHotAdd sets a callback invoked after a phase is dynamically inserted.
func WithOnHotAdd(f HotAddFunc) Option {
	return func(wg *WorkerGroup) { wg.OnHotAdd = f }
}

// WithMetrics enables metrics collection.
func WithMetrics(m *Metrics) Option {
	return func(wg *WorkerGroup) { wg.Metrics = m }
}

// WithLogger sets the log output writer. Nil defaults to os.Stderr.
func WithLogger(w io.Writer) Option {
	return func(wg *WorkerGroup) { wg.Logger = w }
}

// NewWorkerGroup creates a WorkerGroup with required dependencies and optional
// configuration. Required parameters are the nebula definition and execution
// state; everything else is configured via Option functions.
func NewWorkerGroup(n *Nebula, state *State, opts ...Option) *WorkerGroup {
	wg := &WorkerGroup{
		Nebula:     n,
		State:      state,
		MaxWorkers: 1,
	}
	for _, opt := range opts {
		opt(wg)
	}
	return wg
}

// WorkerGroup executes phases in dependency order using a pool of workers.
type WorkerGroup struct {
	Runner       PhaseRunner
	Nebula       *Nebula
	State        *State
	MaxWorkers   int
	Watcher      *Watcher     // nil = no in-flight editing
	Committer    GitCommitter // nil = no phase-boundary commits
	Gater        Gater        // nil = trust mode (no prompts)
	Dashboard    *Dashboard   // nil = no dashboard; used to coordinate watch-mode output
	BeadsClient  beads.Client // nil = hot-added phases cannot create beads
	GlobalCycles int
	GlobalBudget float64
	GlobalModel  string
	OnProgress   ProgressFunc                       // optional progress callback
	OnRefactor   func(phaseID string, pending bool) // optional callback for refactor notifications
	OnHotAdd     HotAddFunc                         // optional callback for hot-added phases
	Metrics      *Metrics                           // optional; nil = no collection
	Logger       io.Writer                          // optional; nil = os.Stderr

	mu               sync.Mutex
	outputMu         sync.Mutex // serializes checkpoint + dashboard output in watch mode
	results          []WorkerResult
	gateSignals      []gateSignal                // collected after each batch
	phaseLoops       map[string]*phaseLoopHandle // running phase → refactor handle
	pendingRefactors map[string]string           // phaseID → updated body (not yet dispatched)
	liveGraph        *Graph                      // DAG updated at runtime for hot-adds
	livePhasesByID   map[string]*PhaseSpec       // all phases indexed by ID
	liveDone         map[string]bool             // phases that have completed
	liveFailed       map[string]bool             // phases that have failed
	liveInFlight     map[string]bool             // phases currently executing
	hotAdded         chan string                 // signals newly ready hot-added phase IDs
	hotAddWg         sync.WaitGroup              // tracks in-flight handlePhaseAdded calls
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
// logger returns the effective log writer (os.Stderr if Logger is nil).
func (wg *WorkerGroup) logger() io.Writer {
	if wg.Logger != nil {
		return wg.Logger
	}
	return os.Stderr
}

// SnapshotNebula returns a deep copy of the Nebula under the WorkerGroup's
// mutex, making it safe to call from any goroutine. The returned snapshot is
// independent of the live Nebula and will not be affected by concurrent
// mutations (e.g. hot-added phases).
func (wg *WorkerGroup) SnapshotNebula() *Nebula {
	wg.mu.Lock()
	defer wg.mu.Unlock()
	return wg.Nebula.Snapshot()
}

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
	phasesByID = PhasesByID(wg.Nebula.Phases)

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
//
// In watch mode, the output mutex serializes checkpoint rendering across concurrent
// goroutines so that checkpoint blocks from parallel phases never interleave.
// The dashboard is paused during checkpoint rendering and resumed afterward.
func (wg *WorkerGroup) applyGate(ctx context.Context, phase *PhaseSpec, cp *Checkpoint) GateAction {
	mode := wg.resolveGateMode(phase)

	switch mode {
	case GateModeTrust:
		return GateActionAccept

	case GateModeWatch:
		// Render checkpoint but don't block.
		// Serialize output so concurrent phase completions don't interleave.
		if cp != nil {
			wg.outputMu.Lock()
			if wg.Dashboard != nil {
				wg.Dashboard.Pause()
			}
			RenderCheckpoint(wg.logger(), cp)
			if wg.Dashboard != nil {
				// Hold wg.mu so the Dashboard.Render triggered by Resume
				// doesn't race with concurrent State mutations in recordResult.
				wg.mu.Lock()
				wg.Dashboard.Resume()
				wg.mu.Unlock()
			}
			wg.outputMu.Unlock()
		}
		return GateActionAccept

	case GateModeReview, GateModeApprove:
		// Render checkpoint and prompt for decision.
		if cp != nil {
			RenderCheckpoint(wg.logger(), cp)
		}
		action, err := wg.Gater.Prompt(ctx, cp)
		if err != nil {
			fmt.Fprintf(wg.logger(), "warning: gate prompt failed: %v (defaulting to accept)\n", err)
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
	waveNumber int,
	phasesByID map[string]*PhaseSpec,
	done, failed, inFlight map[string]bool,
) {
	phase := phasesByID[phaseID]
	ps := wg.State.Phases[phaseID]
	if phase == nil || ps == nil || ps.BeadID == "" {
		wg.recordFailure(phaseID, done, failed, inFlight)
		return
	}

	if wg.Metrics != nil {
		wg.Metrics.RecordPhaseStart(phaseID, waveNumber)
	}

	wg.mu.Lock()
	wg.State.SetPhaseState(phaseID, ps.BeadID, PhaseStatusInProgress)
	if err := SaveState(wg.Nebula.Dir, wg.State); err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to save state: %v\n", err)
	}
	wg.reportProgress()
	wg.mu.Unlock()

	exec := ResolveExecution(wg.GlobalCycles, wg.GlobalBudget, wg.GlobalModel, &wg.Nebula.Manifest.Execution, phase)
	prompt := buildPhasePrompt(phase, &wg.Nebula.Manifest.Context)
	phaseResult, err := wg.Runner.RunExistingPhase(ctx, phaseID, ps.BeadID, prompt, exec)

	if wg.Metrics != nil && phaseResult != nil {
		wg.Metrics.RecordPhaseComplete(phaseID, *phaseResult)
	}

	// Commit phase changes on success so reviewers see clean diffs.
	if err == nil && wg.Committer != nil {
		if commitErr := wg.Committer.CommitPhase(ctx, wg.Nebula.Manifest.Nebula.Name, phaseID); commitErr != nil {
			fmt.Fprintf(wg.logger(), "warning: failed to commit phase %q: %v\n", phaseID, commitErr)
		}
	}

	// Build checkpoint after successful phase completion.
	var cp *Checkpoint
	if err == nil && phaseResult != nil && wg.Committer != nil {
		var cpErr error
		cp, cpErr = BuildCheckpoint(ctx, wg.Committer, phaseID, *phaseResult, wg.Nebula)
		if cpErr != nil {
			fmt.Fprintf(wg.logger(), "warning: failed to build checkpoint for %q: %v\n", phaseID, cpErr)
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
				fmt.Fprintf(wg.logger(), "warning: failed to save state: %v\n", saveErr)
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
		fmt.Fprintf(wg.logger(), "warning: failed to save state: %v\n", err)
	}
	wg.reportProgress()

	// Check if any hot-added phases are now unblocked by this completion.
	wg.checkHotAddedReady()
}

// checkHotAddedReady signals any hot-added phases whose dependencies are now satisfied.
// Must be called with wg.mu held.
func (wg *WorkerGroup) checkHotAddedReady() {
	if wg.liveGraph == nil || wg.hotAdded == nil {
		return
	}
	for _, id := range wg.liveGraph.Ready(wg.liveDone) {
		if wg.liveInFlight[id] || wg.liveFailed[id] {
			continue
		}
		// Only signal phases that were hot-added (not in original wave plan).
		ps := wg.State.Phases[id]
		if ps == nil || ps.Status != PhaseStatusPending {
			continue
		}
		select {
		case wg.hotAdded <- id:
		default:
		}
	}
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
// significant pending intervention (stop > retry > pause > none).
func (wg *WorkerGroup) checkInterventions(done, failed, inFlight map[string]bool) InterventionKind {
	if wg.Watcher == nil {
		return ""
	}
	var latest InterventionKind
	for {
		select {
		case kind := <-wg.Watcher.Interventions:
			// Stop takes priority over everything.
			if kind == InterventionStop {
				return InterventionStop
			}
			if kind == InterventionRetry {
				wg.handleRetry(done, failed, inFlight)
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
// It watches the Interventions channel for a resume signal.
func (wg *WorkerGroup) handlePause() {
	pausePath := filepath.Join(wg.Nebula.Dir, "PAUSE")
	fmt.Fprintf(wg.logger(), "\n── Nebula paused ──────────────────────────────────\n")
	fmt.Fprintf(wg.logger(), "   Remove the PAUSE file to continue:\n")
	fmt.Fprintf(wg.logger(), "   rm %s\n", pausePath)
	fmt.Fprintf(wg.logger(), "───────────────────────────────────────────────────\n\n")

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
		fmt.Fprintf(wg.logger(), "warning: failed to save state: %v\n", err)
	}
	wg.mu.Unlock()

	// Clean up the STOP file.
	stopPath := filepath.Join(wg.Nebula.Dir, "STOP")
	if err := os.Remove(stopPath); err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to remove STOP file: %v\n", err)
	}

	fmt.Fprintf(wg.logger(), "\n── Nebula stopped by user ─────────────────────────\n")
	fmt.Fprintf(wg.logger(), "   State saved. Resume with: quasar nebula apply\n")
	fmt.Fprintf(wg.logger(), "───────────────────────────────────────────────────\n\n")
}

// handleRetry reads the RETRY file to get the phase ID, resets the phase from failed
// to in-progress so it will be re-dispatched, and removes the RETRY file.
func (wg *WorkerGroup) handleRetry(done, failed, inFlight map[string]bool) {
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

	// Clean up the RETRY file.
	if err := os.Remove(retryPath); err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to remove RETRY file: %v\n", err)
	}

	wg.mu.Lock()
	defer wg.mu.Unlock()

	// Only retry phases that are actually failed.
	if !failed[phaseID] {
		fmt.Fprintf(wg.logger(), "warning: phase %q is not failed, ignoring retry\n", phaseID)
		return
	}

	// Reset the phase state so the dispatch loop can re-queue it.
	delete(failed, phaseID)
	delete(done, phaseID)
	delete(inFlight, phaseID)

	ps := wg.State.Phases[phaseID]
	if ps != nil {
		wg.State.SetPhaseState(phaseID, ps.BeadID, PhaseStatusInProgress)
		if saveErr := SaveState(wg.Nebula.Dir, wg.State); saveErr != nil {
			fmt.Fprintf(wg.logger(), "warning: failed to save state: %v\n", saveErr)
		}
	}

	fmt.Fprintf(wg.logger(), "\n── Retrying phase %q ──────────────────────────────\n\n", phaseID)
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

	RenderPlan(wg.logger(), wg.Nebula.Manifest.Nebula.Name, waves, len(wg.Nebula.Phases), wg.GlobalBudget, mode)

	// Build a plan-level checkpoint (no diff, just plan metadata).
	cp := &Checkpoint{
		PhaseID:    PlanPhaseID,
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

// Run dispatches phases respecting dependency order with per-wave semaphore sizing.
// It computes waves upfront and sizes the worker semaphore per wave using
// EffectiveParallelism, which accounts for scope overlaps between phases.
// It returns after all eligible phases have been executed or the context is canceled.
// If a STOP file is detected, it returns ErrManualStop after finishing current phases.
// Gate signals (reject, skip) from phase boundaries also cause graceful termination.
// In approve mode, the execution plan is displayed and requires human approval before
// any phases begin executing.
func (wg *WorkerGroup) Run(ctx context.Context) ([]WorkerResult, error) {
	if wg.MaxWorkers <= 0 {
		wg.MaxWorkers = 1
	}

	// Initialize phase-loop tracking maps.
	wg.mu.Lock()
	wg.phaseLoops = make(map[string]*phaseLoopHandle)
	wg.pendingRefactors = make(map[string]string)
	wg.mu.Unlock()

	// Consume file-change events from the watcher in a background goroutine.
	if wg.Watcher != nil {
		go wg.consumeChanges(ctx)
	}

	phasesByID, done, failed := wg.initPhaseState()
	graph := NewGraph(wg.Nebula.Phases)

	// Expose live state for hot-add support.
	wg.mu.Lock()
	wg.liveGraph = graph
	wg.livePhasesByID = phasesByID
	wg.liveDone = done
	wg.liveFailed = failed
	wg.hotAdded = make(chan string, 16)
	wg.mu.Unlock()

	// Gate the execution plan before dispatching any phases.
	if err := wg.gatePlan(ctx, graph); err != nil {
		return nil, err
	}

	// Compute waves upfront for per-wave semaphore sizing.
	waves, err := graph.ComputeWaves()
	if err != nil {
		return nil, fmt.Errorf("failed to compute waves: %w", err)
	}

	inFlight := make(map[string]bool)
	wg.mu.Lock()
	wg.liveInFlight = inFlight
	wg.mu.Unlock()
	var wgSync sync.WaitGroup

	for _, wave := range waves {
		if ctx.Err() != nil {
			break
		}

		// Compute effective parallelism for this wave based on scope overlaps.
		ep := EffectiveParallelism(wave, wg.Nebula.Phases, graph, wg.MaxWorkers)
		workerCount := ep // Already capped at maxWorkers by EffectiveParallelism.
		if workerCount <= 0 {
			continue
		}
		fmt.Fprintf(wg.logger(), "Wave %d: %d workers (effective parallelism: %d/%d)\n",
			wave.Number, workerCount, ep, len(wave.PhaseIDs))

		sem := make(chan struct{}, workerCount)

		// Track peak actual parallelism during this wave via atomic counter.
		var actualConcurrent, peakConcurrent int64

		// Build a set of phase IDs belonging to this wave so the inner
		// dispatch loop only considers phases from the current wave.
		// Without this filter, graph.Ready(done) would return phases from
		// future waves once their dependencies are satisfied.
		wavePhaseSet := make(map[string]bool, len(wave.PhaseIDs))
		for _, id := range wave.PhaseIDs {
			wavePhaseSet[id] = true
		}

		// Dispatch loop within the wave: keep looking for eligible phases
		// until all wave phases are done or in-flight.
		for ctx.Err() == nil {
			// Check for interventions between batches.
			switch wg.checkInterventions(done, failed, inFlight) {
			case InterventionStop:
				wg.handleStop()
				wg.mu.Lock()
				results := wg.results
				wg.mu.Unlock()
				return results, ErrManualStop
			case InterventionPause:
				wg.handlePause()
				// After resume, re-check for stop.
				if wg.checkInterventions(done, failed, inFlight) == InterventionStop {
					wg.handleStop()
					wg.mu.Lock()
					results := wg.results
					wg.mu.Unlock()
					return results, ErrManualStop
				}
			}

			wg.mu.Lock()
			eligible := filterEligible(graph.Ready(done), inFlight, failed, graph)
			// Restrict to phases belonging to this wave. graph.Ready returns
			// all phases whose dependencies are met, which may include phases
			// from later waves once earlier waves complete.
			var waveEligible []string
			for _, id := range eligible {
				if wavePhaseSet[id] {
					waveEligible = append(waveEligible, id)
				}
			}
			eligible = waveEligible
			anyInFlight := false
			for id := range inFlight {
				if wavePhaseSet[id] {
					anyInFlight = true
					break
				}
			}
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
					defer func() {
						atomic.AddInt64(&actualConcurrent, -1)
						<-sem
						wgSync.Done()
					}()
					cur := atomic.AddInt64(&actualConcurrent, 1)
					for {
						peak := atomic.LoadInt64(&peakConcurrent)
						if cur <= peak || atomic.CompareAndSwapInt64(&peakConcurrent, peak, cur) {
							break
						}
					}
					wg.executePhase(ctx, phaseID, wave.Number, phasesByID, done, failed, inFlight)
				}(id)
			}
			wgSync.Wait() // wait for batch before looking for more ready phases

			// Process gate signals after batch completes.
			stop, retErr := wg.processGateSignals(done)
			if stop {
				return wg.collectResults(), retErr
			}
		}

		// Record wave completion metrics.
		if wg.Metrics != nil {
			wg.Metrics.RecordWaveComplete(wave.Number, ep, int(atomic.LoadInt64(&peakConcurrent)))
		}
	}
	wgSync.Wait()

	// Process any hot-added phases that became ready during or after waves.
	wg.drainHotAdded(ctx, &wgSync, done, failed, inFlight, phasesByID)

	// Wait for any in-flight handlePhaseAdded call to finish, then drain
	// one more time. This closes the race window where handlePhaseAdded
	// sends to hotAdded right after drainHotAdded returns on an empty
	// channel.
	wg.hotAddWg.Wait()
	wg.drainHotAdded(ctx, &wgSync, done, failed, inFlight, phasesByID)

	wg.mu.Lock()
	results := wg.results
	wg.mu.Unlock()
	return results, nil
}

// drainHotAdded dispatches hot-added phases that are ready to execute.
// It keeps draining until no more phases arrive within a short window.
func (wg *WorkerGroup) drainHotAdded(
	ctx context.Context,
	wgSync *sync.WaitGroup,
	done, failed, inFlight map[string]bool,
	phasesByID map[string]*PhaseSpec,
) {
	for {
		select {
		case phaseID := <-wg.hotAdded:
			if ctx.Err() != nil {
				return
			}
			wg.mu.Lock()
			if done[phaseID] || inFlight[phaseID] || failed[phaseID] {
				wg.mu.Unlock()
				continue
			}
			inFlight[phaseID] = true
			wg.mu.Unlock()

			wgSync.Add(1)
			go func(id string) {
				defer wgSync.Done()
				wg.executePhase(ctx, id, 0, phasesByID, done, failed, inFlight)
			}(phaseID)
			wgSync.Wait()

			// Re-evaluate readiness after each phase completes, in case
			// a previously dropped signal left a phase stuck.
			wg.mu.Lock()
			wg.checkHotAddedReady()
			wg.mu.Unlock()
		default:
			return
		}
	}
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
				fmt.Fprintf(wg.logger(), "warning: failed to save state: %v\n", saveErr)
			}
			wg.mu.Unlock()
			return true, fmt.Errorf("phase %q rejected at gate", sig.phaseID)

		case GateActionSkip:
			wg.mu.Lock()
			wg.markRemainingSkipped(done)
			if saveErr := SaveState(wg.Nebula.Dir, wg.State); saveErr != nil {
				fmt.Fprintf(wg.logger(), "warning: failed to save state: %v\n", saveErr)
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

// consumeChanges reads from Watcher.Changes and dispatches to the appropriate
// handler. It runs until the channel is closed (watcher stopped).
func (wg *WorkerGroup) consumeChanges(ctx context.Context) {
	for change := range wg.Watcher.Changes {
		switch change.Kind {
		case ChangeModified:
			wg.handlePhaseModified(change)
		case ChangeAdded:
			wg.hotAddWg.Add(1)
			wg.handlePhaseAdded(ctx, change)
			wg.hotAddWg.Done()
		case ChangeRemoved:
			fmt.Fprintf(wg.logger(), "warning: phase file removed: %s (ignored)\n", change.File)
		}
	}
}

// handlePhaseModified re-parses the modified phase file and, if the phase is
// currently running, sends the updated body on its refactor channel. If the
// phase has not started yet, the body is stored in pendingRefactors for later.
func (wg *WorkerGroup) handlePhaseModified(change Change) {
	phase, err := parsePhaseFile(change.File, Defaults{})
	if err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to re-parse modified phase %q: %v\n", change.PhaseID, err)
		return
	}

	newBody := phase.Body

	wg.mu.Lock()
	handle, running := wg.phaseLoops[change.PhaseID]
	wg.pendingRefactors[change.PhaseID] = newBody
	wg.mu.Unlock()

	if wg.OnRefactor != nil {
		wg.OnRefactor(change.PhaseID, true)
	}

	if running {
		// Non-blocking send — if the channel already has a value the loop
		// will pick up the latest via its drain loop.
		select {
		case handle.RefactorCh <- newBody:
		default:
		}
	}

	fmt.Fprintf(wg.logger(), "phase %q modified — refactor queued\n", change.PhaseID)
}

// handlePhaseAdded parses a newly added phase file, validates it, and inserts
// it into the live DAG. If the phase's dependencies are already satisfied it
// is immediately queued for execution via the hotAdded channel.
func (wg *WorkerGroup) handlePhaseAdded(ctx context.Context, change Change) {
	var defaults Defaults
	if wg.Nebula != nil {
		defaults = wg.Nebula.Manifest.Defaults
	}
	phase, err := parsePhaseFile(change.File, defaults)
	if err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to parse new phase %q: %v\n", change.PhaseID, err)
		return
	}
	phase.SourceFile = filepath.Base(change.File)

	wg.mu.Lock()
	defer wg.mu.Unlock()

	// Bail out if live state is not yet initialized (Run hasn't started).
	if wg.liveGraph == nil || wg.Nebula == nil {
		wg.pendingRefactors[change.PhaseID] = ""
		fmt.Fprintf(wg.logger(), "phase %q added (file: %s) — noted for future DAG insertion\n", phase.ID, filepath.Base(change.File))
		return
	}

	// Build the set of existing IDs for validation.
	existingIDs := make(map[string]bool, len(wg.livePhasesByID))
	for id := range wg.livePhasesByID {
		existingIDs[id] = true
	}

	// Validate the hot-add.
	vErrs := ValidateHotAdd(phase, existingIDs, wg.liveGraph)
	if len(vErrs) > 0 {
		for _, ve := range vErrs {
			fmt.Fprintf(wg.logger(), "warning: hot-add rejected: %s\n", ve.Error())
		}
		return
	}

	// Handle reverse dependencies (blocks field).
	// ValidateHotAdd added all blocks edges for cycle detection; now remove
	// edges for blocked phases that are already in-flight or done, since we
	// cannot modify their dependencies.
	for _, blockedID := range phase.Blocks {
		if wg.liveInFlight[blockedID] || wg.liveDone[blockedID] {
			fmt.Fprintf(wg.logger(), "warning: phase %q is already started/done — ignoring blocks entry for %q\n", blockedID, phase.ID)
			// Remove the phantom edge that ValidateHotAdd added.
			wg.liveGraph.RemoveEdge(blockedID, phase.ID)
			continue
		}
		// Edge is already in the graph from ValidateHotAdd — update the
		// blocked phase's DependsOn slice for consistency.
		if bp, ok := wg.livePhasesByID[blockedID]; ok {
			bp.DependsOn = append(bp.DependsOn, phase.ID)
		}
	}

	// Register the phase in all live data structures.
	wg.Nebula.Phases = append(wg.Nebula.Phases, phase)
	wg.livePhasesByID[phase.ID] = &wg.Nebula.Phases[len(wg.Nebula.Phases)-1]

	// Create a bead for the hot-added phase so that executePhase can use it.
	beadID := ""
	if wg.BeadsClient != nil {
		wg.mu.Unlock()
		var createErr error
		beadID, createErr = wg.BeadsClient.Create(ctx, phase.Title, beads.CreateOpts{
			Description: phase.Body,
			Type:        phase.Type,
			Labels:      phase.Labels,
			Assignee:    phase.Assignee,
			Priority:    priorityStr(phase.Priority),
		})
		wg.mu.Lock()
		if createErr != nil {
			fmt.Fprintf(wg.logger(), "warning: failed to create bead for hot-added phase %q: %v\n", phase.ID, createErr)
			wg.liveFailed[phase.ID] = true
			wg.liveDone[phase.ID] = true
			wg.State.SetPhaseState(phase.ID, "", PhaseStatusFailed)
			if saveErr := SaveState(wg.Nebula.Dir, wg.State); saveErr != nil {
				fmt.Fprintf(wg.logger(), "warning: failed to save state: %v\n", saveErr)
			}
			// Check if this failure unblocks any hot-added phases waiting on it.
			wg.checkHotAddedReady()
			return
		}
	}

	// Create state entry with bead ID.
	wg.State.SetPhaseState(phase.ID, beadID, PhaseStatusPending)
	if saveErr := SaveState(wg.Nebula.Dir, wg.State); saveErr != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to save state after hot-add: %v\n", saveErr)
	}

	// Update progress counts.
	wg.reportProgress()

	// Notify TUI.
	if wg.OnHotAdd != nil {
		wg.OnHotAdd(phase.ID, phase.Title, phase.DependsOn)
	}

	fmt.Fprintf(wg.logger(), "phase %q hot-added to nebula DAG\n", phase.ID)

	// Check if the phase is immediately ready to execute.
	allDeps := wg.liveGraph.Ready(wg.liveDone)
	for _, id := range allDeps {
		if id == phase.ID {
			select {
			case wg.hotAdded <- phase.ID:
			default:
			}
			break
		}
	}
}

// RegisterPhaseLoop records a running phase's refactor channel so that
// handlePhaseModified can forward updated descriptions to the loop.
func (wg *WorkerGroup) RegisterPhaseLoop(phaseID string, refactorCh chan<- string) {
	wg.mu.Lock()
	defer wg.mu.Unlock()
	wg.phaseLoops[phaseID] = &phaseLoopHandle{RefactorCh: refactorCh}

	// If there is already a pending refactor for this phase (file was edited
	// before the loop started), send it immediately.
	if body, ok := wg.pendingRefactors[phaseID]; ok && body != "" {
		select {
		case refactorCh <- body:
		default:
		}
	}
}

// UnregisterPhaseLoop removes a phase's loop handle after completion.
func (wg *WorkerGroup) UnregisterPhaseLoop(phaseID string) {
	wg.mu.Lock()
	defer wg.mu.Unlock()
	delete(wg.phaseLoops, phaseID)
}
