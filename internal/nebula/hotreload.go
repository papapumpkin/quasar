package nebula

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sync"

	"github.com/papapumpkin/quasar/internal/beads"
)

// HotReloader handles in-flight file watching, phase modification, hot-add
// of new phases into the live DAG, and phase loop registration for refactors.
type HotReloader struct {
	watcher     *Watcher
	beadsClient beads.Client
	nebula      *Nebula
	state       *State
	tracker     *PhaseTracker
	progress    *ProgressReporter
	onRefactor  func(phaseID string, pending bool)
	onHotAdd    HotAddFunc
	logger      io.Writer

	// mu is a pointer to the WorkerGroup's mutex so all collaborators
	// share the same lock for coordinating access to shared state.
	mu       *sync.Mutex
	outputMu *sync.Mutex

	phaseLoops       map[string]*phaseLoopHandle
	pendingRefactors map[string]string

	// Live DAG state — shared with the orchestrator via the tracker.
	liveGraph    *Graph
	livePhasesByID map[string]*PhaseSpec
	hotAdded     chan string
	hotAddWg     sync.WaitGroup
}

// HotReloaderConfig holds the configuration for creating a HotReloader.
type HotReloaderConfig struct {
	Watcher     *Watcher
	BeadsClient beads.Client
	Nebula      *Nebula
	State       *State
	Tracker     *PhaseTracker
	Progress    *ProgressReporter
	OnRefactor  func(phaseID string, pending bool)
	OnHotAdd    HotAddFunc
	Logger      io.Writer
	Mu          *sync.Mutex
	OutputMu    *sync.Mutex
}

// NewHotReloader creates a HotReloader with the given configuration.
func NewHotReloader(cfg HotReloaderConfig) *HotReloader {
	return &HotReloader{
		watcher:          cfg.Watcher,
		beadsClient:      cfg.BeadsClient,
		nebula:           cfg.Nebula,
		state:            cfg.State,
		tracker:          cfg.Tracker,
		progress:         cfg.Progress,
		onRefactor:       cfg.OnRefactor,
		onHotAdd:         cfg.OnHotAdd,
		logger:           cfg.Logger,
		mu:               cfg.Mu,
		outputMu:         cfg.OutputMu,
		phaseLoops:       make(map[string]*phaseLoopHandle),
		pendingRefactors: make(map[string]string),
	}
}

// InitLiveState sets up the live DAG state for hot-add support.
// Must be called with mu held.
func (hr *HotReloader) InitLiveState(graph *Graph, phasesByID map[string]*PhaseSpec) {
	hr.liveGraph = graph
	hr.livePhasesByID = phasesByID
	hr.hotAdded = make(chan string, 16)
}

// HotAdded returns the channel that signals newly ready hot-added phase IDs.
func (hr *HotReloader) HotAdded() chan string {
	return hr.hotAdded
}

// WaitHotAddWg waits for any in-flight handlePhaseAdded calls to finish.
func (hr *HotReloader) WaitHotAddWg() {
	hr.hotAddWg.Wait()
}

// ConsumeChanges reads from Watcher.Changes and dispatches to the appropriate
// handler. It runs until the channel is closed (watcher stopped).
func (hr *HotReloader) ConsumeChanges(ctx context.Context) {
	for change := range hr.watcher.Changes {
		switch change.Kind {
		case ChangeModified:
			hr.handlePhaseModified(change)
		case ChangeAdded:
			hr.hotAddWg.Add(1)
			hr.handlePhaseAdded(ctx, change)
			hr.hotAddWg.Done()
		case ChangeRemoved:
			fmt.Fprintf(hr.logger, "warning: phase file removed: %s (ignored)\n", change.File)
		}
	}
}

// handlePhaseModified re-parses the modified phase file and, if the phase is
// currently running, sends the updated body on its refactor channel. If the
// phase has not started yet, the body is stored in pendingRefactors for later.
func (hr *HotReloader) handlePhaseModified(change Change) {
	phase, err := parsePhaseFile(change.File, Defaults{})
	if err != nil {
		fmt.Fprintf(hr.logger, "warning: failed to re-parse modified phase %q: %v\n", change.PhaseID, err)
		return
	}

	newBody := phase.Body

	hr.mu.Lock()
	handle, running := hr.phaseLoops[change.PhaseID]
	hr.pendingRefactors[change.PhaseID] = newBody
	hr.mu.Unlock()

	if hr.onRefactor != nil {
		hr.onRefactor(change.PhaseID, true)
	}

	if running {
		// Non-blocking send — if the channel already has a value the loop
		// will pick up the latest via its drain loop.
		select {
		case handle.RefactorCh <- newBody:
		default:
		}
	}

	fmt.Fprintf(hr.logger, "phase %q modified — refactor queued\n", change.PhaseID)
}

// handlePhaseAdded parses a newly added phase file, validates it, and inserts
// it into the live DAG. If the phase's dependencies are already satisfied it
// is immediately queued for execution via the hotAdded channel.
func (hr *HotReloader) handlePhaseAdded(ctx context.Context, change Change) {
	var defaults Defaults
	if hr.nebula != nil {
		defaults = hr.nebula.Manifest.Defaults
	}
	phase, err := parsePhaseFile(change.File, defaults)
	if err != nil {
		fmt.Fprintf(hr.logger, "warning: failed to parse new phase %q: %v\n", change.PhaseID, err)
		return
	}
	phase.SourceFile = filepath.Base(change.File)

	hr.mu.Lock()
	defer hr.mu.Unlock()

	// Bail out if live state is not yet initialized (Run hasn't started).
	if hr.liveGraph == nil || hr.nebula == nil {
		hr.pendingRefactors[change.PhaseID] = ""
		fmt.Fprintf(hr.logger, "phase %q added (file: %s) — noted for future DAG insertion\n", phase.ID, filepath.Base(change.File))
		return
	}

	// Build the set of existing IDs for validation.
	existingIDs := make(map[string]bool, len(hr.livePhasesByID))
	for id := range hr.livePhasesByID {
		existingIDs[id] = true
	}

	// Validate the hot-add.
	vErrs := ValidateHotAdd(phase, existingIDs, hr.liveGraph)
	if len(vErrs) > 0 {
		for _, ve := range vErrs {
			fmt.Fprintf(hr.logger, "warning: hot-add rejected: %s\n", ve.Error())
		}
		return
	}

	// Handle reverse dependencies (blocks field).
	for _, blockedID := range phase.Blocks {
		if hr.tracker.inFlight[blockedID] || hr.tracker.done[blockedID] {
			fmt.Fprintf(hr.logger, "warning: phase %q is already started/done — ignoring blocks entry for %q\n", blockedID, phase.ID)
			hr.liveGraph.RemoveEdge(blockedID, phase.ID)
			continue
		}
		if bp, ok := hr.livePhasesByID[blockedID]; ok {
			bp.DependsOn = append(bp.DependsOn, phase.ID)
		}
	}

	// Register the phase in all live data structures.
	hr.nebula.Phases = append(hr.nebula.Phases, phase)
	hr.livePhasesByID[phase.ID] = &hr.nebula.Phases[len(hr.nebula.Phases)-1]

	// Create a bead for the hot-added phase so that executePhase can use it.
	beadID := ""
	if hr.beadsClient != nil {
		hr.mu.Unlock()
		var createErr error
		beadID, createErr = hr.beadsClient.Create(ctx, phase.Title, beads.CreateOpts{
			Description: phase.Body,
			Type:        phase.Type,
			Labels:      phase.Labels,
			Assignee:    phase.Assignee,
			Priority:    priorityStr(phase.Priority),
		})
		hr.mu.Lock()
		if createErr != nil {
			fmt.Fprintf(hr.logger, "warning: failed to create bead for hot-added phase %q: %v\n", phase.ID, createErr)
			hr.tracker.failed[phase.ID] = true
			hr.tracker.done[phase.ID] = true
			hr.state.SetPhaseState(phase.ID, "", PhaseStatusFailed)
			hr.progress.SaveState()
			hr.checkHotAddedReady()
			return
		}
	}

	// Create state entry with bead ID.
	hr.state.SetPhaseState(phase.ID, beadID, PhaseStatusPending)
	hr.progress.SaveState()

	// Update progress counts.
	hr.progress.ReportProgress()

	// Notify TUI.
	if hr.onHotAdd != nil {
		hr.onHotAdd(phase.ID, phase.Title, phase.DependsOn)
	}

	fmt.Fprintf(hr.logger, "phase %q hot-added to nebula DAG\n", phase.ID)

	// Check if the phase is immediately ready to execute.
	allDeps := hr.liveGraph.Ready(hr.tracker.done)
	for _, id := range allDeps {
		if id == phase.ID {
			select {
			case hr.hotAdded <- phase.ID:
			default:
			}
			break
		}
	}
}

// CheckHotAddedReady signals any hot-added phases whose dependencies are now satisfied.
// Must be called with mu held.
func (hr *HotReloader) CheckHotAddedReady() {
	hr.checkHotAddedReady()
}

// checkHotAddedReady is the internal implementation.
// Must be called with mu held.
func (hr *HotReloader) checkHotAddedReady() {
	if hr.liveGraph == nil || hr.hotAdded == nil {
		return
	}
	for _, id := range hr.liveGraph.Ready(hr.tracker.done) {
		if hr.tracker.inFlight[id] || hr.tracker.failed[id] {
			continue
		}
		// Only signal phases that were hot-added (not in original wave plan).
		ps := hr.state.Phases[id]
		if ps == nil || ps.Status != PhaseStatusPending {
			continue
		}
		select {
		case hr.hotAdded <- id:
		default:
		}
	}
}

// RegisterPhaseLoop records a running phase's refactor channel so that
// handlePhaseModified can forward updated descriptions to the loop.
func (hr *HotReloader) RegisterPhaseLoop(phaseID string, refactorCh chan<- string) {
	hr.mu.Lock()
	defer hr.mu.Unlock()
	hr.phaseLoops[phaseID] = &phaseLoopHandle{RefactorCh: refactorCh}

	// If there is already a pending refactor for this phase (file was edited
	// before the loop started), send it immediately.
	if body, ok := hr.pendingRefactors[phaseID]; ok && body != "" {
		select {
		case refactorCh <- body:
		default:
		}
	}
}

// UnregisterPhaseLoop removes a phase's loop handle after completion.
func (hr *HotReloader) UnregisterPhaseLoop(phaseID string) {
	hr.mu.Lock()
	defer hr.mu.Unlock()
	delete(hr.phaseLoops, phaseID)
}

// DrainHotAdded dispatches hot-added phases that are ready to execute.
// It keeps draining until no more phases arrive within a short window.
func (hr *HotReloader) DrainHotAdded(
	ctx context.Context,
	wgSync *sync.WaitGroup,
	executePhase func(ctx context.Context, phaseID string, waveNumber int),
) {
	for {
		select {
		case phaseID := <-hr.hotAdded:
			if ctx.Err() != nil {
				return
			}
			hr.mu.Lock()
			if hr.tracker.done[phaseID] || hr.tracker.inFlight[phaseID] || hr.tracker.failed[phaseID] {
				hr.mu.Unlock()
				continue
			}
			hr.tracker.inFlight[phaseID] = true
			hr.mu.Unlock()

			wgSync.Add(1)
			go func(id string) {
				defer wgSync.Done()
				executePhase(ctx, id, 0)
			}(phaseID)
			wgSync.Wait()

			// Re-evaluate readiness after each phase completes, in case
			// a previously dropped signal left a phase stuck.
			hr.mu.Lock()
			hr.checkHotAddedReady()
			hr.mu.Unlock()
		default:
			return
		}
	}
}
