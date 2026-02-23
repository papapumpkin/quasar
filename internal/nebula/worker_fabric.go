package nebula

import (
	"context"
	"fmt"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// fabricBlocked returns the number of blocked phases, or 0 if the fabric
// is disabled (Fabric field is nil).
func (wg *WorkerGroup) fabricBlocked() int {
	if wg.blockedTracker == nil {
		return 0
	}
	return wg.blockedTracker.Len()
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

// pollEligible filters the eligible list through fabric polling. Phases that
// poll PROCEED remain eligible for dispatch; others are handed to the
// pushback handler and either blocked or escalated.
// Must NOT be called with wg.mu held.
func (wg *WorkerGroup) pollEligible(ctx context.Context, eligible []string) []string {
	wg.mu.Lock()
	snap, err := wg.buildFabricSnapshot(ctx)
	wg.mu.Unlock()

	if err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to build fabric snapshot: %v\n", err)
		return eligible // proceed without filtering
	}

	var proceed []string
	for _, id := range eligible {
		// Skip already-blocked phases — they wait for re-evaluation.
		if wg.blockedTracker.Get(id) != nil {
			continue
		}

		// Phases previously overridden by the pushback handler skip
		// polling entirely — they proceed without re-interrogation.
		if wg.blockedTracker.IsOverridden(id) {
			proceed = append(proceed, id)
			continue
		}

		result, pollErr := wg.Poller.Poll(ctx, id, snap)
		if pollErr != nil {
			fmt.Fprintf(wg.logger(), "warning: poll failed for phase %q: %v\n", id, pollErr)
			proceed = append(proceed, id) // on error, proceed optimistically
			continue
		}

		switch result.Decision {
		case fabric.PollProceed:
			if setErr := wg.Fabric.SetPhaseState(ctx, id, fabric.StateRunning); setErr != nil {
				fmt.Fprintf(wg.logger(), "warning: failed to set fabric state for %q: %v\n", id, setErr)
			}
			proceed = append(proceed, id)
		default:
			wg.handlePollBlock(ctx, id, result, snap)
		}
	}
	return proceed
}

// handlePollBlock processes a phase that did not poll PROCEED. It records the
// phase in the blocked tracker, sets the fabric state, and runs the pushback
// handler to decide whether to retry or escalate.
func (wg *WorkerGroup) handlePollBlock(ctx context.Context, phaseID string, result fabric.PollResult, snap fabric.FabricSnapshot) {
	wg.blockedTracker.Block(phaseID, result)
	bp := wg.blockedTracker.Get(phaseID)

	if setErr := wg.Fabric.SetPhaseState(ctx, phaseID, fabric.StateBlocked); setErr != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to set fabric state for %q: %v\n", phaseID, setErr)
	}

	action := wg.pushbackHandler.Handle(ctx, bp, snap.InProgress, snap)

	switch action {
	case fabric.ActionRetry:
		fmt.Fprintf(wg.logger(), "  Phase %q blocked: %s (retry %d)\n",
			phaseID, result.Reason, bp.RetryCount)
	case fabric.ActionEscalate:
		wg.escalatePhase(ctx, phaseID, bp)
	case fabric.ActionProceed:
		// Pushback handler overrode the block — unblock and mark as
		// overridden so future dispatch cycles skip polling for this phase
		// (preventing retry counter reset in a block-unblock loop).
		wg.blockedTracker.Unblock(phaseID)
		wg.blockedTracker.Override(phaseID)
	}
}

// escalatePhase transitions a blocked phase to HUMAN_DECISION_REQUIRED.
// It unblocks the phase, updates the fabric state, logs the escalation message,
// and emits a gate signal so the existing Gater can handle it.
func (wg *WorkerGroup) escalatePhase(ctx context.Context, phaseID string, bp *fabric.BlockedPhase) {
	wg.blockedTracker.Unblock(phaseID)

	if setErr := wg.Fabric.SetPhaseState(ctx, phaseID, fabric.StateHumanDecision); setErr != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to set fabric state for %q: %v\n", phaseID, setErr)
	}

	maxRetries := fabric.DefaultMaxRetries
	if wg.pushbackHandler.MaxRetries > 0 {
		maxRetries = wg.pushbackHandler.MaxRetries
	}
	msg := fabric.EscalationMessage(bp, maxRetries)

	fmt.Fprintf(wg.logger(), "\n── Fabric Escalation ──────────────────────────────\n%s───────────────────────────────────────────────────\n\n", msg)

	// Mark the phase as failed so the DAG treats it as done.
	wg.mu.Lock()
	wg.tracker.Done()[phaseID] = true
	wg.tracker.Failed()[phaseID] = true
	wg.gateSignals = append(wg.gateSignals, gateSignal{
		phaseID: phaseID,
		action:  GateActionReject,
	})
	wg.mu.Unlock()
}

// fabricPhaseComplete is called from the executePhase goroutine after a
// successful phase completion. It publishes entanglements and updates the fabric
// state. File claims are released so subsequent phases can claim them.
// Must NOT be called with wg.mu held.
func (wg *WorkerGroup) fabricPhaseComplete(ctx context.Context, phaseID string, result *PhaseRunnerResult) {
	if wg.Fabric == nil {
		return
	}

	// Publish entanglements from the phase's changes.
	if wg.Publisher != nil && result != nil {
		if err := wg.Publisher.PublishPhase(ctx, phaseID, result.BaseCommitSHA, result.FinalCommitSHA); err != nil {
			fmt.Fprintf(wg.logger(), "warning: failed to publish entanglements for %q: %v\n", phaseID, err)
		}
	}

	// Mark phase done on the fabric.
	if err := wg.Fabric.SetPhaseState(ctx, phaseID, fabric.StateDone); err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to set fabric done state for %q: %v\n", phaseID, err)
	}

	// Release file claims so blocked phases can proceed.
	if err := wg.Fabric.ReleaseClaims(ctx, phaseID); err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to release claims for %q: %v\n", phaseID, err)
	}
}

// reevaluateBlocked re-polls all blocked phases with a fresh fabric snapshot.
// Phases that now poll PROCEED are unblocked and will be picked up by the
// next dispatch iteration. Phases still blocked are run through the pushback
// handler again (incrementing their retry count).
// Must NOT be called with wg.mu held.
func (wg *WorkerGroup) reevaluateBlocked(ctx context.Context) {
	if wg.blockedTracker == nil || wg.blockedTracker.Len() == 0 {
		return
	}

	wg.mu.Lock()
	snap, err := wg.buildFabricSnapshot(ctx)
	wg.mu.Unlock()

	if err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to build fabric snapshot for re-evaluation: %v\n", err)
		return
	}

	for _, bp := range wg.blockedTracker.All() {
		result, pollErr := wg.Poller.Poll(ctx, bp.PhaseID, snap)
		if pollErr != nil {
			fmt.Fprintf(wg.logger(), "warning: re-poll failed for blocked phase %q: %v\n", bp.PhaseID, pollErr)
			continue
		}

		if result.Decision == fabric.PollProceed {
			wg.blockedTracker.Unblock(bp.PhaseID)
			if setErr := wg.Fabric.SetPhaseState(ctx, bp.PhaseID, fabric.StateScanning); setErr != nil {
				fmt.Fprintf(wg.logger(), "warning: failed to set fabric state for %q: %v\n", bp.PhaseID, setErr)
			}
			fmt.Fprintf(wg.logger(), "  Phase %q unblocked — entanglements now available\n", bp.PhaseID)
		} else {
			// Still blocked — re-run pushback handler.
			wg.handlePollBlock(ctx, bp.PhaseID, result, snap)
		}
	}
}

// escalateAllBlocked escalates every remaining blocked phase. This is called
// when nothing is in-flight and all ready phases are blocked — a dead end
// that requires human intervention.
func (wg *WorkerGroup) escalateAllBlocked(ctx context.Context) {
	if wg.blockedTracker == nil {
		return
	}
	for _, bp := range wg.blockedTracker.All() {
		ptr := wg.blockedTracker.Get(bp.PhaseID)
		if ptr != nil {
			wg.escalatePhase(ctx, bp.PhaseID, ptr)
		}
	}
}
