package nebula

import (
	"context"
	"fmt"

	"github.com/papapumpkin/quasar/internal/board"
)

// boardBlocked returns the number of blocked phases, or 0 if the board
// is disabled (Board field is nil).
func (wg *WorkerGroup) boardBlocked() int {
	if wg.blockedTracker == nil {
		return 0
	}
	return wg.blockedTracker.Len()
}

// buildBoardSnapshot constructs a BoardSnapshot from the board and tracker
// state. Must be called with wg.mu held (reads tracker maps).
func (wg *WorkerGroup) buildBoardSnapshot(ctx context.Context) (board.BoardSnapshot, error) {
	snap := board.BoardSnapshot{
		FileClaims: make(map[string]string),
	}

	contracts, err := wg.Board.AllContracts(ctx)
	if err != nil {
		return snap, fmt.Errorf("fetching contracts: %w", err)
	}
	snap.Contracts = contracts

	done := wg.tracker.Done()
	failed := wg.tracker.Failed()
	inFlight := wg.tracker.InFlight()

	for id := range done {
		if !failed[id] {
			snap.Completed = append(snap.Completed, id)
		}
	}
	for id := range inFlight {
		snap.InProgress = append(snap.InProgress, id)
	}

	// Collect file claims for in-progress phases.
	for id := range inFlight {
		claims, claimErr := wg.Board.ClaimsFor(ctx, id)
		if claimErr != nil {
			continue // non-fatal
		}
		for _, fp := range claims {
			snap.FileClaims[fp] = id
		}
	}

	return snap, nil
}

// pollEligible filters the eligible list through board polling. Phases that
// poll PROCEED remain eligible for dispatch; others are handed to the
// pushback handler and either blocked or escalated.
// Must NOT be called with wg.mu held.
func (wg *WorkerGroup) pollEligible(ctx context.Context, eligible []string) []string {
	wg.mu.Lock()
	snap, err := wg.buildBoardSnapshot(ctx)
	wg.mu.Unlock()

	if err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to build board snapshot: %v\n", err)
		return eligible // proceed without filtering
	}

	var proceed []string
	for _, id := range eligible {
		// Skip already-blocked phases — they wait for re-evaluation.
		if wg.blockedTracker.Get(id) != nil {
			continue
		}

		result, pollErr := wg.Poller.Poll(ctx, id, snap)
		if pollErr != nil {
			fmt.Fprintf(wg.logger(), "warning: poll failed for phase %q: %v\n", id, pollErr)
			proceed = append(proceed, id) // on error, proceed optimistically
			continue
		}

		switch result.Decision {
		case board.PollProceed:
			if setErr := wg.Board.SetPhaseState(ctx, id, board.StateRunning); setErr != nil {
				fmt.Fprintf(wg.logger(), "warning: failed to set board state for %q: %v\n", id, setErr)
			}
			proceed = append(proceed, id)
		default:
			wg.handlePollBlock(ctx, id, result, snap)
		}
	}
	return proceed
}

// handlePollBlock processes a phase that did not poll PROCEED. It records the
// phase in the blocked tracker, sets the board state, and runs the pushback
// handler to decide whether to retry or escalate.
func (wg *WorkerGroup) handlePollBlock(ctx context.Context, phaseID string, result board.PollResult, snap board.BoardSnapshot) {
	wg.blockedTracker.Block(phaseID, result)
	bp := wg.blockedTracker.Get(phaseID)

	if setErr := wg.Board.SetPhaseState(ctx, phaseID, board.StateBlocked); setErr != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to set board state for %q: %v\n", phaseID, setErr)
	}

	action := wg.pushbackHandler.Handle(ctx, bp, snap.InProgress, snap)

	switch action {
	case board.ActionRetry:
		fmt.Fprintf(wg.logger(), "  Phase %q blocked: %s (retry %d)\n",
			phaseID, result.Reason, bp.RetryCount)
	case board.ActionEscalate:
		wg.escalatePhase(ctx, phaseID, bp)
	case board.ActionProceed:
		// Pushback handler overrode the block — unblock and let the next
		// iteration pick the phase up via normal eligibility.
		wg.blockedTracker.Unblock(phaseID)
	}
}

// escalatePhase transitions a blocked phase to HUMAN_DECISION_REQUIRED.
// It unblocks the phase, updates the board state, logs the escalation message,
// and emits a gate signal so the existing Gater can handle it.
func (wg *WorkerGroup) escalatePhase(ctx context.Context, phaseID string, bp *board.BlockedPhase) {
	wg.blockedTracker.Unblock(phaseID)

	if setErr := wg.Board.SetPhaseState(ctx, phaseID, board.StateHumanDecision); setErr != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to set board state for %q: %v\n", phaseID, setErr)
	}

	maxRetries := board.DefaultMaxRetries
	if wg.pushbackHandler.MaxRetries > 0 {
		maxRetries = wg.pushbackHandler.MaxRetries
	}
	msg := board.EscalationMessage(bp, maxRetries)

	fmt.Fprintf(wg.logger(), "\n── Board Escalation ───────────────────────────────\n%s───────────────────────────────────────────────────\n\n", msg)

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

// boardPhaseComplete is called from the executePhase goroutine after a
// successful phase completion. It publishes contracts and updates the board
// state. File claims are released so subsequent phases can claim them.
// Must NOT be called with wg.mu held.
func (wg *WorkerGroup) boardPhaseComplete(ctx context.Context, phaseID string, result *PhaseRunnerResult) {
	if wg.Board == nil {
		return
	}

	// Publish contracts from the phase's changes.
	if wg.Publisher != nil && result != nil {
		if err := wg.Publisher.PublishPhase(ctx, phaseID, result.BaseCommitSHA, result.FinalCommitSHA); err != nil {
			fmt.Fprintf(wg.logger(), "warning: failed to publish contracts for %q: %v\n", phaseID, err)
		}
	}

	// Mark phase done on the board.
	if err := wg.Board.SetPhaseState(ctx, phaseID, board.StateDone); err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to set board done state for %q: %v\n", phaseID, err)
	}

	// Release file claims so blocked phases can proceed.
	if err := wg.Board.ReleaseClaims(ctx, phaseID); err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to release claims for %q: %v\n", phaseID, err)
	}
}

// reevaluateBlocked re-polls all blocked phases with a fresh board snapshot.
// Phases that now poll PROCEED are unblocked and will be picked up by the
// next dispatch iteration. Phases still blocked are run through the pushback
// handler again (incrementing their retry count).
// Must NOT be called with wg.mu held.
func (wg *WorkerGroup) reevaluateBlocked(ctx context.Context) {
	if wg.blockedTracker == nil || wg.blockedTracker.Len() == 0 {
		return
	}

	wg.mu.Lock()
	snap, err := wg.buildBoardSnapshot(ctx)
	wg.mu.Unlock()

	if err != nil {
		fmt.Fprintf(wg.logger(), "warning: failed to build board snapshot for re-evaluation: %v\n", err)
		return
	}

	for _, bp := range wg.blockedTracker.All() {
		result, pollErr := wg.Poller.Poll(ctx, bp.PhaseID, snap)
		if pollErr != nil {
			fmt.Fprintf(wg.logger(), "warning: re-poll failed for blocked phase %q: %v\n", bp.PhaseID, pollErr)
			continue
		}

		if result.Decision == board.PollProceed {
			wg.blockedTracker.Unblock(bp.PhaseID)
			if setErr := wg.Board.SetPhaseState(ctx, bp.PhaseID, board.StatePolling); setErr != nil {
				fmt.Fprintf(wg.logger(), "warning: failed to set board state for %q: %v\n", bp.PhaseID, setErr)
			}
			fmt.Fprintf(wg.logger(), "  Phase %q unblocked — contracts now available\n", bp.PhaseID)
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
