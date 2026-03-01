package nebula

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/dag"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/tycho"
)

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
// It delegates phase state tracking to PhaseTracker, progress/metrics to
// ProgressReporter, and hot-reload concerns to HotReloader.
type WorkerGroup struct {
	Runner       PhaseRunner
	Nebula       *Nebula
	State        *State
	MaxWorkers   int
	Watcher      *Watcher          // nil = no in-flight editing
	Committer    GitCommitter      // nil = no phase-boundary commits
	Gater        Gater             // nil = built from Prompter + manifest at Run time
	Prompter     GatePrompter      // used to build Gater if Gater is nil
	Dashboard    *Dashboard        // nil = no dashboard; used to coordinate watch-mode output
	BeadsClient  beads.Client      // nil = hot-added phases cannot create beads
	Fabric       fabric.Fabric     // nil = no fabric (legacy behavior)
	Poller       fabric.Poller     // nil = skip polling (legacy behavior)
	Publisher    *fabric.Publisher // nil = no entanglement publishing
	GlobalCycles int
	GlobalBudget float64
	GlobalModel  string
	OnProgress   ProgressFunc                             // optional progress callback
	OnRefactor   func(phaseID string, pending bool)       // optional callback for refactor notifications
	OnHotAdd     HotAddFunc                               // optional callback for hot-added phases
	OnHail       func(phaseID string, d fabric.Discovery) // optional callback for hail surfacing
	OnScanning   func(phaseID string)                     // optional callback for fabric scanning notifications
	Metrics      *Metrics                                 // optional; nil = no collection
	Logger       io.Writer                                // optional; nil = os.Stderr

	mu          sync.Mutex
	outputMu    sync.Mutex // serializes checkpoint + dashboard output in watch mode
	results     []WorkerResult
	gateSignals []gateSignal // collected after each batch

	// Collaborators — constructed during Run.
	tracker         *PhaseTracker
	progress        *ProgressReporter
	hotReload       *HotReloader
	blockedTracker  *fabric.BlockedTracker  // nil when Fabric is nil
	pushbackHandler *fabric.PushbackHandler // nil when Fabric is nil
	tychoScheduler  *tycho.Scheduler        // nil when Fabric is nil
	routingCtx      *RoutingContext         // nil when auto-routing is disabled
}

// logger returns the effective log writer (os.Stderr if Logger is nil).
func (wg *WorkerGroup) logger() io.Writer {
	if wg.Logger != nil {
		return wg.Logger
	}
	return os.Stderr
}

// SnapshotNebula returns a deep copy of the Nebula under the WorkerGroup's
// mutex, making it safe to call from any goroutine.
func (wg *WorkerGroup) SnapshotNebula() *Nebula {
	wg.mu.Lock()
	defer wg.mu.Unlock()
	return wg.Nebula.Snapshot()
}

// RegisterPhaseLoop records a running phase's refactor channel so that
// handlePhaseModified can forward updated descriptions to the loop.
func (wg *WorkerGroup) RegisterPhaseLoop(phaseID string, refactorCh chan<- string) {
	if wg.hotReload != nil {
		wg.hotReload.RegisterPhaseLoop(phaseID, refactorCh)
	}
}

// UnregisterPhaseLoop removes a phase's loop handle after completion.
func (wg *WorkerGroup) UnregisterPhaseLoop(phaseID string) {
	if wg.hotReload != nil {
		wg.hotReload.UnregisterPhaseLoop(phaseID)
	}
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

// ensureGater builds the Gater from the Prompter and manifest if not already set.
func (wg *WorkerGroup) ensureGater() {
	if wg.Gater != nil {
		return
	}
	if wg.Prompter == nil {
		wg.Gater = trustGater{}
		return
	}
	wg.Gater = NewGater(wg.Nebula.Manifest.Execution, wg.Prompter, GaterDeps{
		Logger:    wg.logger(),
		OutputMu:  &wg.outputMu,
		Mu:        &wg.mu,
		Dashboard: wg.Dashboard,
	})
}

// gatePlan displays the execution plan and gates it for approval via the Gater.
func (wg *WorkerGroup) gatePlan(ctx context.Context, d *dag.DAG) error {
	waves, err := d.ComputeWaves()
	if err != nil {
		return fmt.Errorf("failed to compute execution waves: %w", err)
	}

	mode := wg.Nebula.Manifest.Execution.Gate
	if mode == "" {
		mode = GateModeTrust
	}
	RenderPlan(wg.logger(), wg.Nebula.Manifest.Nebula.Name, waves, len(wg.Nebula.Phases), wg.GlobalBudget, mode)

	cp := &Checkpoint{
		PhaseID:    PlanPhaseID,
		PhaseTitle: "Execution Plan",
		NebulaName: wg.Nebula.Manifest.Nebula.Name,
	}
	return wg.Gater.PlanGate(ctx, cp)
}

// drainGateSignals returns and clears any pending gate signals.
// Must be called with wg.mu held.
func (wg *WorkerGroup) drainGateSignals() []gateSignal {
	signals := wg.gateSignals
	wg.gateSignals = nil
	return signals
}

// collectResults returns a snapshot of the current results.
func (wg *WorkerGroup) collectResults() []WorkerResult {
	wg.mu.Lock()
	defer wg.mu.Unlock()
	return wg.results
}

// awaitCompletion blocks until one goroutine sends on completionCh and
// decrements activeCount. This is the core mechanism that replaces the
// old batch-barrier wgSync.Wait(): instead of waiting for ALL goroutines
// to finish, we wake up as soon as ANY one completes.
func (wg *WorkerGroup) awaitCompletion(completionCh <-chan string, activeCount *int64) {
	<-completionCh
	atomic.AddInt64(activeCount, -1)
}

// drainActive waits for all remaining in-flight goroutines to complete
// by reading from completionCh until activeCount reaches zero.
func (wg *WorkerGroup) drainActive(completionCh <-chan string, activeCount *int64) {
	for atomic.LoadInt64(activeCount) > 0 {
		<-completionCh
		atomic.AddInt64(activeCount, -1)
	}
}

// Run dispatches phases using impact-aware scheduling with track-based
// parallelism. The DAG engine's TaskAnalyzer computes composite impact
// scores (PageRank + Betweenness Centrality) and partitions the graph
// into independent tracks via Union-Find. Phases are dispatched as their
// dependencies become satisfied, sorted by impact score so that
// bottleneck phases run first. Independent tracks execute in parallel
// when max_workers > 1.
//
// Dispatch is truly continuous: when any single goroutine completes, the
// loop immediately re-evaluates for newly-ready phases. There are no
// wave or batch barriers.
func (wg *WorkerGroup) Run(ctx context.Context) ([]WorkerResult, error) {
	if wg.MaxWorkers <= 0 {
		wg.MaxWorkers = 1
	}

	wg.ensureGater()

	// Construct collaborators.
	wg.tracker = NewPhaseTracker(wg.Nebula.Phases, wg.State)
	wg.progress = NewProgressReporter(wg.Nebula, wg.State, wg.OnProgress, wg.Metrics, wg.logger())
	wg.hotReload = NewHotReloader(HotReloaderConfig{
		Watcher:     wg.Watcher,
		BeadsClient: wg.BeadsClient,
		Nebula:      wg.Nebula,
		State:       wg.State,
		Tracker:     wg.tracker,
		Progress:    wg.progress,
		OnRefactor:  wg.OnRefactor,
		OnHotAdd:    wg.OnHotAdd,
		Logger:      wg.logger(),
		Mu:          &wg.mu,
		OutputMu:    &wg.outputMu,
	})

	if wg.Watcher != nil {
		go wg.hotReload.ConsumeChanges(ctx)
	}

	// Build impact-aware scheduler from phases using the DAG engine.
	scheduler, err := NewScheduler(wg.Nebula.Phases)
	if err != nil {
		return nil, fmt.Errorf("building scheduler: %w", err)
	}

	// Initialize fabric collaborators when the fabric is configured.
	if wg.Fabric != nil && wg.Poller != nil {
		wg.blockedTracker = fabric.NewBlockedTracker()
		wg.pushbackHandler = &fabric.PushbackHandler{Fabric: wg.Fabric}
	}

	// Pre-compute waves for wave-aware scanning. Used by the WaveScanner
	// to walk phases layer-by-layer, pruning descendants of blocked phases.
	dagGraph := scheduler.Analyzer().DAG()
	waves, wavesErr := dagGraph.ComputeWaves()
	if wavesErr != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to compute waves: %v\n", wavesErr)
	}

	// Build routing context for adaptive model selection. When routing is
	// enabled in the manifest and no blanket model override is set, phases
	// will be scored for complexity and routed to an appropriate tier.
	if wg.Nebula.Manifest.Execution.Routing.Enabled {
		wg.routingCtx = &RoutingContext{
			Routing: wg.Nebula.Manifest.Execution.Routing,
			DAG:     dagGraph,
		}
	}

	// Always create the Tycho scheduler for DAG resolution. When fabric
	// components are nil, Scan/Reevaluate/etc. become no-ops, preserving
	// legacy (no-fabric) behavior.
	wg.tychoScheduler = &tycho.Scheduler{
		Fabric:   wg.Fabric,          // may be nil
		Poller:   wg.Poller,          // may be nil
		Blocked:  wg.blockedTracker,  // may be nil
		Pushback: wg.pushbackHandler, // may be nil
		Logger:   wg.logger(),
		Resolver: &workerEligibleResolver{
			wg:        wg,
			scheduler: scheduler,
		},
		OnHail: wg.OnHail, // may be nil — surfaced via cockpit TUI when set
		Waves:  waves,     // may be nil if ComputeWaves failed
		DAG:    dagGraph,
	}

	// Wire wave-aware scanner when fabric components are available and
	// waves were computed successfully.
	if wg.Fabric != nil && wg.Poller != nil && len(waves) > 0 {
		wg.tychoScheduler.WaveScanner = &tycho.WaveScanner{
			Poller:     wg.Poller,
			Blocked:    wg.blockedTracker,
			Pushback:   wg.pushbackHandler,
			Fabric:     wg.Fabric,
			DAG:        dagGraph,
			Logger:     wg.logger(),
			OnEscalate: wg.tychoScheduler.HandleEscalation,
		}
	}

	wg.mu.Lock()
	wg.hotReload.InitLiveState(scheduler.Analyzer().DAG(), wg.tracker.PhasesByIDMap())
	wg.mu.Unlock()

	if err := wg.gatePlan(ctx, scheduler.Analyzer().DAG()); err != nil {
		return nil, err
	}

	// Determine effective parallelism. When the fabric is active, the
	// Poller handles readiness checks per-phase, so we can use the full
	// max_workers count even within a single track. Without fabric, fall
	// back to the conservative track-based cap that prevents same-track
	// concurrency.
	tracks := scheduler.Tracks()
	var workerCount int
	if wg.Fabric != nil {
		workerCount = wg.MaxWorkers
	} else {
		workerCount = TrackParallelism(tracks, wg.MaxWorkers)
	}
	if workerCount <= 0 {
		workerCount = 1
	}

	fmt.Fprintf(wg.logger(), "Scheduler: %d tracks, %d workers (max: %d)\n",
		len(tracks), workerCount, wg.MaxWorkers)
	for _, t := range tracks {
		fmt.Fprintf(wg.logger(), "  Track %d: %v (impact: %.2f)\n",
			t.ID, t.NodeIDs, t.AggregateImpact)
	}

	inFlight := wg.tracker.InFlight()

	sem := make(chan struct{}, workerCount)
	// completionCh receives a phase ID each time a goroutine finishes,
	// allowing the dispatch loop to re-evaluate immediately instead of
	// waiting for an entire batch.
	completionCh := make(chan string, workerCount)
	var activeCount int64
	var peakConcurrent int64

	// Continuous dispatch loop: phases are dispatched as soon as their
	// dependencies complete. When any goroutine finishes, the loop
	// immediately re-evaluates for newly-ready tasks — no wave barriers.
	for ctx.Err() == nil {
		switch wg.checkInterventions() {
		case InterventionStop:
			wg.handleStop()
			wg.drainActive(completionCh, &activeCount)
			return wg.collectResults(), ErrManualStop
		case InterventionPause:
			wg.handlePause()
			if wg.checkInterventions() == InterventionStop {
				wg.handleStop()
				wg.drainActive(completionCh, &activeCount)
				return wg.collectResults(), ErrManualStop
			}
		}

		// Delegate DAG resolution and tracker filtering to Tycho.
		wg.mu.Lock()
		eligible, _ := wg.tychoScheduler.Eligible(ctx)
		anyInFlight := wg.tychoScheduler.AnyInFlight()
		wg.mu.Unlock()

		// Notify the TUI that eligible phases are entering the fabric scan gate.
		// Only fires when fabric is configured (OnScanning is wired) so legacy
		// mode never produces scanning toasts.
		if wg.OnScanning != nil && wg.Fabric != nil {
			for _, id := range eligible {
				wg.OnScanning(id)
			}
		}

		// Delegate fabric-aware scanning to Tycho. When fabric is not
		// configured, Scan returns eligible unchanged (no-op).
		if len(eligible) > 0 {
			eligible, _ = wg.tychoScheduler.Scan(ctx, eligible, wg.snapshotBuilder())
		}

		if len(eligible) == 0 {
			anyBlocked := wg.fabricBlocked() > 0
			if !anyInFlight && !anyBlocked {
				break // nothing running, nothing blocked, nothing to dispatch — done
			}
			if !anyInFlight && anyBlocked {
				// Dead end: blocked phases with nothing running to produce
				// the missing contracts. Escalate all to human decision.
				wg.escalateAllBlocked(ctx)
				break
			}
			// Wait for any one in-flight phase to complete, then re-evaluate.
			wg.awaitCompletion(completionCh, &activeCount)
			wg.reevaluateBlocked(ctx)
			stop, retErr := wg.processGateSignals()
			if stop {
				wg.drainActive(completionCh, &activeCount)
				return wg.collectResults(), retErr
			}
			continue
		}

		// Dispatch all currently eligible phases.
		for _, id := range eligible {
			if ctx.Err() != nil {
				break
			}
			wg.mu.Lock()
			inFlight[id] = true
			wg.mu.Unlock()

			sem <- struct{}{} // block if at worker capacity
			atomic.AddInt64(&activeCount, 1)
			go func(phaseID string) {
				defer func() {
					<-sem
					completionCh <- phaseID
				}()
				// Track peak concurrency.
				for {
					peak := atomic.LoadInt64(&peakConcurrent)
					cur := atomic.LoadInt64(&activeCount)
					if cur <= peak || atomic.CompareAndSwapInt64(&peakConcurrent, peak, cur) {
						break
					}
				}
				trackID := scheduler.TrackForTask(phaseID)
				wg.executePhase(ctx, phaseID, trackID)
			}(id)
		}

		// After dispatching, wait for any one goroutine to finish before
		// re-evaluating. This avoids busy-spinning and ensures newly-ready
		// phases are picked up as soon as any dependency completes.
		wg.awaitCompletion(completionCh, &activeCount)
		wg.reevaluateBlocked(ctx)
		stop, retErr := wg.processGateSignals()
		if stop {
			wg.drainActive(completionCh, &activeCount)
			return wg.collectResults(), retErr
		}
	}

	// Drain remaining in-flight goroutines on context cancellation or
	// post-loop exit (e.g., all-blocked escalation).
	wg.drainActive(completionCh, &activeCount)

	// Process any gate signals accumulated during or after the loop
	// (e.g., from escalateAllBlocked). This ensures escalated phases
	// trigger MarkRemainingSkipped and produce proper error returns.
	stop, retErr := wg.processGateSignals()
	if stop {
		return wg.collectResults(), retErr
	}

	// Record track completion as a single aggregate wave for metrics
	// compatibility. The wave number is 0, effective parallelism is the
	// track-based worker count, and peak is the observed peak concurrency.
	wg.progress.RecordWaveComplete(0, workerCount, int(atomic.LoadInt64(&peakConcurrent)))

	var wgSync sync.WaitGroup
	wg.hotReload.DrainHotAdded(ctx, &wgSync, func(c context.Context, phaseID string, waveNumber int) {
		wg.executePhase(c, phaseID, waveNumber)
	})

	wg.hotReload.WaitHotAddWg()
	wg.hotReload.DrainHotAdded(ctx, &wgSync, func(c context.Context, phaseID string, waveNumber int) {
		wg.executePhase(c, phaseID, waveNumber)
	})

	// Purge fulfilled entanglements now that the nebula is complete.
	// Disputed/pending entanglements are preserved for human review.
	if wg.Fabric != nil {
		if purgeErr := wg.Fabric.PurgeFulfilledEntanglements(ctx); purgeErr != nil {
			fmt.Fprintf(wg.logger(), "warning: failed to purge fulfilled entanglements: %v\n", purgeErr)
		}
	}

	wg.mu.Lock()
	results := wg.results
	wg.mu.Unlock()
	return results, nil
}
