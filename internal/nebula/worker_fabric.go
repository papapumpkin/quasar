package nebula

import (
	"context"
	"fmt"

	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/tycho"
)

// workerEligibleResolver implements tycho.EligibleResolver by combining
// the nebula Scheduler's DAG-aware ready-task selection with the
// PhaseTracker's filtering. The caller must hold wg.mu when calling
// ResolveEligible and AnyInFlight, since they read tracker state.
type workerEligibleResolver struct {
	wg        *WorkerGroup
	scheduler *Scheduler // nebula's impact-aware DAG scheduler
}

// ResolveEligible returns task IDs sorted by impact score that have all
// DAG dependencies satisfied and pass tracker filtering. Must be called
// with wg.mu held.
func (r *workerEligibleResolver) ResolveEligible() []string {
	done := r.wg.tracker.Done()
	ready := r.scheduler.ReadyTasks(done)
	return r.wg.tracker.FilterEligible(ready, r.scheduler.Analyzer().DAG())
}

// AnyInFlight reports whether any tasks are currently executing. Must be
// called with wg.mu held.
func (r *workerEligibleResolver) AnyInFlight() bool {
	return len(r.wg.tracker.InFlight()) > 0
}

// workerSnapshotBuilder implements tycho.SnapshotBuilder by reading
// from the WorkerGroup's tracker and Fabric. It captures the mutex
// and tracker references so Tycho can request snapshots without
// coupling to WorkerGroup internals.
type workerSnapshotBuilder struct {
	wg *WorkerGroup
}

// BuildSnapshot constructs a FabricSnapshot from the worker group's
// tracker and fabric state. The WorkerGroup mutex is acquired during
// in-memory reads and temporarily released during Fabric I/O.
func (b *workerSnapshotBuilder) BuildSnapshot(ctx context.Context) (fabric.FabricSnapshot, error) {
	wg := b.wg
	wg.mu.Lock()
	snap, err := wg.buildFabricSnapshot(ctx)
	wg.mu.Unlock()
	return snap, err
}

// fabricBlocked returns the number of blocked phases via the Tycho
// scheduler, or 0 if the scheduler is not configured.
func (wg *WorkerGroup) fabricBlocked() int {
	if wg.tychoScheduler == nil {
		if wg.blockedTracker == nil {
			return 0
		}
		return wg.blockedTracker.Len()
	}
	return wg.tychoScheduler.BlockedCount()
}

// buildFabricSnapshot constructs a FabricSnapshot from the fabric and tracker
// state. Must be called with wg.mu held. The lock is temporarily released
// during Fabric I/O (SQLite queries) to avoid blocking worker goroutines
// that need the mutex for recordResult calls.
func (wg *WorkerGroup) buildFabricSnapshot(ctx context.Context) (fabric.FabricSnapshot, error) {
	snap := fabric.FabricSnapshot{
		FileClaims: make(map[string]string),
	}

	// Read tracker maps under the lock — these are fast, in-memory reads.
	done := wg.tracker.Done()
	failed := wg.tracker.Failed()
	inFlight := wg.tracker.InFlight()

	for id := range done {
		if !failed[id] {
			snap.Completed = append(snap.Completed, id)
		}
	}
	var inFlightIDs []string
	for id := range inFlight {
		snap.InProgress = append(snap.InProgress, id)
		inFlightIDs = append(inFlightIDs, id)
	}

	// Release the lock before performing Fabric I/O (SQLite queries under
	// WAL mode). This allows worker goroutines to acquire wg.mu for
	// recordResult calls while we wait on the database.
	wg.mu.Unlock()

	entanglements, err := wg.Fabric.AllEntanglements(ctx)
	if err != nil {
		wg.mu.Lock() // re-acquire before returning
		return snap, fmt.Errorf("fetching entanglements: %w", err)
	}
	snap.Entanglements = entanglements

	// Collect file claims for in-progress phases.
	for _, id := range inFlightIDs {
		claims, claimErr := wg.Fabric.ClaimsFor(ctx, id)
		if claimErr != nil {
			continue // non-fatal
		}
		for _, fp := range claims {
			snap.FileClaims[fp] = id
		}
	}

	wg.mu.Lock() // re-acquire before returning
	return snap, nil
}

// pollEligible filters the eligible list through the Tycho scheduler's Scan
// method. Phases that poll PROCEED remain eligible for dispatch; others are
// handed to the pushback handler and either blocked or escalated.
// Must NOT be called with wg.mu held.
func (wg *WorkerGroup) pollEligible(ctx context.Context, eligible []string) []string {
	proceed, _ := wg.tychoScheduler.Scan(ctx, eligible, wg.snapshotBuilder())
	return proceed
}

// fabricPhaseComplete is called from the executePhase goroutine after a
// successful phase completion. It delegates to the Tycho scheduler's
// PhaseComplete method to publish entanglements, mark the phase done,
// and release file claims.
// Must NOT be called with wg.mu held.
func (wg *WorkerGroup) fabricPhaseComplete(ctx context.Context, phaseID string, result *PhaseRunnerResult) {
	if wg.Fabric == nil {
		return
	}

	var baseCommit, finalCommit string
	if result != nil {
		baseCommit = result.BaseCommitSHA
		finalCommit = result.FinalCommitSHA
	}

	if wg.tychoScheduler != nil {
		wg.tychoScheduler.PhaseComplete(ctx, phaseID, wg.Publisher, baseCommit, finalCommit)
		return
	}

	// Fallback for nil scheduler (should not happen in normal flow).
	if wg.Publisher != nil && result != nil {
		if err := wg.Publisher.PublishPhase(ctx, phaseID, baseCommit, finalCommit); err != nil {
			fmt.Fprintf(wg.logger(), "warning: failed to publish entanglements for %q: %v\n", phaseID, err)
		}
	}
	if err := wg.Fabric.SetPhaseState(ctx, phaseID, fabric.StateDone); err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to set fabric done state for %q: %v\n", phaseID, err)
	}
	if err := wg.Fabric.ReleaseClaims(ctx, phaseID); err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to release claims for %q: %v\n", phaseID, err)
	}
}

// reevaluateBlocked re-polls all blocked phases via the Tycho scheduler.
// Must NOT be called with wg.mu held.
func (wg *WorkerGroup) reevaluateBlocked(ctx context.Context) {
	if wg.tychoScheduler == nil {
		return
	}
	_, _ = wg.tychoScheduler.Reevaluate(ctx, wg.snapshotBuilder())
}

// escalateAllBlocked escalates every remaining blocked phase via the Tycho
// scheduler. This is called when nothing is in-flight and all ready phases
// are blocked — a dead end that requires human intervention.
func (wg *WorkerGroup) escalateAllBlocked(ctx context.Context) {
	if wg.tychoScheduler == nil {
		return
	}
	wg.tychoScheduler.EscalateAllBlocked(ctx, wg.markPhaseFailedWithSignal)
}

// markPhaseFailedWithSignal marks a phase as failed in the tracker and
// emits a gate signal. This is passed as a callback to the Tycho scheduler
// for escalation handling.
func (wg *WorkerGroup) markPhaseFailedWithSignal(phaseID string) {
	wg.mu.Lock()
	wg.tracker.Done()[phaseID] = true
	wg.tracker.Failed()[phaseID] = true
	wg.gateSignals = append(wg.gateSignals, gateSignal{
		phaseID: phaseID,
		action:  GateActionReject,
	})
	wg.mu.Unlock()
}

// snapshotBuilder returns a tycho.SnapshotBuilder backed by this WorkerGroup.
func (wg *WorkerGroup) snapshotBuilder() tycho.SnapshotBuilder {
	return &workerSnapshotBuilder{wg: wg}
}
