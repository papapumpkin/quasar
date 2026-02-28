// Package tycho provides DAG scheduling for the nebula orchestrator.
// Named after Tycho Brahe, the master observer who tracked positions
// without theorizing, the Scheduler observes fabric state and resolves
// the DAG to determine which tasks are eligible for execution.
package tycho

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/papapumpkin/quasar/internal/dag"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// SnapshotBuilder constructs a fabric.Snapshot from the current system state.
// It abstracts the WorkerGroup's internal bookkeeping (tracker, mutex, Fabric
// I/O) so that Tycho can request snapshots without coupling to WorkerGroup.
type SnapshotBuilder interface {
	BuildSnapshot(ctx context.Context) (fabric.Snapshot, error)
}

// EligibleResolver provides DAG-aware eligible task resolution. It combines
// topological sort with impact scoring and tracker-based filtering (in-flight,
// failed, scope-conflict exclusion) into a single pipeline. Implementations
// are responsible for their own locking when accessing shared tracker state.
type EligibleResolver interface {
	// ResolveEligible returns task IDs sorted by impact score that have
	// all DAG dependencies satisfied and pass tracker filtering (not
	// in-flight, not failed, no failed dependencies, no scope conflicts).
	ResolveEligible() []string

	// AnyInFlight reports whether any tasks are currently executing.
	AnyInFlight() bool
}

// StaleItem describes a claim or task that appears stuck.
type StaleItem struct {
	Kind    string        // "claim" or "task"
	ID      string        // filepath or task_id
	Age     time.Duration // time since creation/last transition
	Details string        // human-readable context
}

// Scheduler observes fabric state and resolves the DAG to determine
// which tasks are eligible for execution. It encapsulates DAG resolution,
// scanning gate logic, blocked-task re-polling, stale detection, and hail
// triggering. When fabric components (Fabric, Poller, etc.) are nil, fabric
// operations are skipped, preserving legacy (no-fabric) behavior.
type Scheduler struct {
	Fabric   fabric.Fabric
	Poller   fabric.Poller
	Blocked  *fabric.BlockedTracker
	Pushback *fabric.PushbackHandler
	Logger   io.Writer
	Resolver EligibleResolver // provides DAG + tracker resolution

	// WaveScanner enables wave-aware scanning. When non-nil, Scan()
	// delegates to ScanWaves() for topology-aware pruning instead of
	// iterating a flat eligible list.
	WaveScanner *WaveScanner

	// Waves holds the pre-computed wave ordering from the DAG. Used
	// by WaveScanner to walk phases layer-by-layer.
	Waves []dag.Wave

	// DAG provides the dependency graph for Descendants() lookups
	// during wave pruning. Passed through to WaveScanner.
	DAG *dag.DAG

	// OnHail is called when a blocked task requires human intervention.
	// If nil, escalations are logged but not surfaced.
	OnHail func(phaseID string, discovery fabric.Discovery)
}

// Eligible returns task IDs that have all DAG dependencies satisfied and
// are not currently running, blocked, or done. It delegates to the
// EligibleResolver, which encapsulates DAG topological sort, impact
// scoring, and tracker-based filtering. The caller must hold any required
// locks for the resolver's state (typically the WorkerGroup mutex).
// Returns nil when no Resolver is configured.
func (s *Scheduler) Eligible(_ context.Context) ([]string, error) {
	if s.Resolver == nil {
		return nil, nil
	}
	return s.Resolver.ResolveEligible(), nil
}

// AnyInFlight reports whether any tasks are currently executing. It
// delegates to the EligibleResolver. Returns false when no Resolver is
// configured.
func (s *Scheduler) AnyInFlight() bool {
	if s.Resolver == nil {
		return false
	}
	return s.Resolver.AnyInFlight()
}

// Scan filters eligible task IDs through fabric polling. Tasks that poll
// PROCEED remain eligible; others are handed to the pushback handler and
// either blocked or escalated. The snapshot builder is used to construct
// the fabric snapshot for polling context.
//
// When fabric components (Poller, Blocked) are nil, all eligible tasks are
// returned unchanged — this preserves legacy (no-fabric) behavior.
//
// Tasks already tracked as blocked are skipped (they await re-evaluation).
// Tasks previously overridden by the pushback handler skip polling entirely.
func (s *Scheduler) Scan(ctx context.Context, eligible []string, sb SnapshotBuilder) ([]string, error) {
	// When fabric components are not configured, all eligible tasks proceed.
	if s.Poller == nil || s.Blocked == nil || sb == nil {
		return eligible, nil
	}

	snap, err := sb.BuildSnapshot(ctx)
	if err != nil {
		fmt.Fprintf(s.logger(), "warning: failed to build fabric snapshot: %v\n", err)
		return eligible, nil // proceed without filtering
	}

	// Delegate to wave-aware scanning when WaveScanner is configured.
	if s.WaveScanner != nil && len(s.Waves) > 0 {
		proceed, _ := s.WaveScanner.ScanWaves(ctx, s.Waves, toSet(eligible), snap)
		return proceed, nil
	}

	// Flat scan fallback: iterate eligible phases without wave awareness.
	return s.flatScan(ctx, eligible, snap)
}

// flatScan iterates eligible phases in order, polling each independently.
// This is the legacy code path used when no WaveScanner is configured.
func (s *Scheduler) flatScan(ctx context.Context, eligible []string, snap fabric.Snapshot) ([]string, error) {
	var proceed []string
	for _, id := range eligible {
		// Skip already-blocked phases — they wait for re-evaluation.
		if s.Blocked.Get(id) != nil {
			continue
		}

		// Phases previously overridden by the pushback handler skip
		// polling entirely — they proceed without re-interrogation.
		if s.Blocked.IsOverridden(id) {
			proceed = append(proceed, id)
			continue
		}

		result, pollErr := s.Poller.Poll(ctx, id, snap)
		if pollErr != nil {
			fmt.Fprintf(s.logger(), "warning: poll failed for phase %q: %v\n", id, pollErr)
			proceed = append(proceed, id) // on error, proceed optimistically
			continue
		}

		switch result.Decision {
		case fabric.PollProceed:
			if setErr := s.Fabric.SetPhaseState(ctx, id, fabric.StateRunning); setErr != nil {
				fmt.Fprintf(s.logger(), "warning: failed to set fabric state for %q: %v\n", id, setErr)
			}
			proceed = append(proceed, id)
		default:
			s.HandlePollBlock(ctx, id, result, snap)
		}
	}
	return proceed, nil
}

// toSet converts a string slice to a set (map[string]bool).
func toSet(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// Reevaluate re-polls all blocked tasks against current fabric state.
// Tasks whose blockers are resolved transition back to eligible (returned
// as unblocked). Tasks still blocked are run through the pushback handler
// again.
func (s *Scheduler) Reevaluate(ctx context.Context, sb SnapshotBuilder) (unblocked []string, err error) {
	if s.Blocked == nil || s.Blocked.Len() == 0 {
		return nil, nil
	}

	snap, err := sb.BuildSnapshot(ctx)
	if err != nil {
		fmt.Fprintf(s.logger(), "warning: failed to build fabric snapshot for re-evaluation: %v\n", err)
		return nil, nil
	}

	for _, bp := range s.Blocked.All() {
		result, pollErr := s.Poller.Poll(ctx, bp.PhaseID, snap)
		if pollErr != nil {
			fmt.Fprintf(s.logger(), "warning: re-poll failed for blocked phase %q: %v\n", bp.PhaseID, pollErr)
			continue
		}

		if result.Decision == fabric.PollProceed {
			s.Blocked.Unblock(bp.PhaseID)
			if setErr := s.Fabric.SetPhaseState(ctx, bp.PhaseID, fabric.StateScanning); setErr != nil {
				fmt.Fprintf(s.logger(), "warning: failed to set fabric state for %q: %v\n", bp.PhaseID, setErr)
			}
			fmt.Fprintf(s.logger(), "  Phase %q unblocked — entanglements now available\n", bp.PhaseID)
			unblocked = append(unblocked, bp.PhaseID)
		} else {
			// Still blocked — re-run pushback handler.
			s.HandlePollBlock(ctx, bp.PhaseID, result, snap)
		}
	}
	return unblocked, nil
}

// BlockedCount returns the number of blocked phases, or 0 if the blocked
// tracker is nil.
func (s *Scheduler) BlockedCount() int {
	if s.Blocked == nil {
		return 0
	}
	return s.Blocked.Len()
}

// StaleCheck identifies tasks and claims that appear stuck.
// Claims older than staleClaim with no corresponding running task, and tasks
// with no state transition within staleTask, are flagged.
func (s *Scheduler) StaleCheck(ctx context.Context, staleClaim, staleTask time.Duration) ([]StaleItem, error) {
	var items []StaleItem

	// Check file claims for staleness.
	claims, err := s.Fabric.AllClaims(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching claims: %w", err)
	}

	states, err := s.Fabric.AllPhaseStates(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching phase states: %w", err)
	}

	now := time.Now()
	for _, c := range claims {
		age := now.Sub(c.ClaimedAt)
		if age < staleClaim {
			continue
		}
		// A claim is stale if the owning task is not running.
		ownerState := states[c.OwnerTask]
		if ownerState == fabric.StateRunning {
			continue
		}
		items = append(items, StaleItem{
			Kind:    "claim",
			ID:      c.Filepath,
			Age:     age,
			Details: fmt.Sprintf("owner %q in state %q", c.OwnerTask, ownerState),
		})
	}

	// Check blocked tasks for staleness.
	if s.Blocked != nil {
		for _, bp := range s.Blocked.All() {
			age := now.Sub(bp.BlockedAt)
			if age >= staleTask {
				items = append(items, StaleItem{
					Kind:    "task",
					ID:      bp.PhaseID,
					Age:     age,
					Details: fmt.Sprintf("blocked: %s (retries: %d)", bp.LastResult.Reason, bp.RetryCount),
				})
			}
		}
	}

	return items, nil
}

// EscalateAllBlocked escalates every remaining blocked phase. This is
// called when nothing is in-flight and all ready phases are blocked — a
// dead end that requires human intervention.
func (s *Scheduler) EscalateAllBlocked(ctx context.Context, markFailed func(phaseID string)) {
	if s.Blocked == nil {
		return
	}
	for _, bp := range s.Blocked.All() {
		ptr := s.Blocked.Get(bp.PhaseID)
		if ptr != nil {
			s.escalatePhase(ctx, bp.PhaseID, ptr, markFailed)
		}
	}
}

// PhaseComplete updates fabric state after a successful phase completion.
// It publishes entanglements (via the publisher), marks the phase done,
// and releases file claims.
func (s *Scheduler) PhaseComplete(ctx context.Context, phaseID string, publisher *fabric.Publisher, baseCommit, finalCommit string) {
	if s.Fabric == nil {
		return
	}

	// Publish entanglements from the phase's changes.
	if publisher != nil {
		if err := publisher.PublishPhase(ctx, phaseID, baseCommit, finalCommit); err != nil {
			fmt.Fprintf(s.logger(), "warning: failed to publish entanglements for %q: %v\n", phaseID, err)
		}
	}

	// Mark phase done on the fabric.
	if err := s.Fabric.SetPhaseState(ctx, phaseID, fabric.StateDone); err != nil {
		fmt.Fprintf(s.logger(), "warning: failed to set fabric done state for %q: %v\n", phaseID, err)
	}

	// Release file claims so blocked phases can proceed.
	if err := s.Fabric.ReleaseClaims(ctx, phaseID); err != nil {
		fmt.Fprintf(s.logger(), "warning: failed to release claims for %q: %v\n", phaseID, err)
	}
}

// HandlePollBlock processes a phase that did not poll PROCEED. It records the
// phase in the blocked tracker, sets the fabric state, and runs the pushback
// handler to decide whether to retry or escalate.
func (s *Scheduler) HandlePollBlock(ctx context.Context, phaseID string, result fabric.PollResult, snap fabric.Snapshot) {
	s.Blocked.Block(phaseID, result)
	bp := s.Blocked.Get(phaseID)

	if setErr := s.Fabric.SetPhaseState(ctx, phaseID, fabric.StateBlocked); setErr != nil {
		fmt.Fprintf(s.logger(), "warning: failed to set fabric state for %q: %v\n", phaseID, setErr)
	}

	action := s.Pushback.Handle(ctx, bp, snap.InProgress, snap)

	switch action {
	case fabric.ActionRetry:
		fmt.Fprintf(s.logger(), "  Phase %q blocked: %s (retry %d)\n",
			phaseID, result.Reason, bp.RetryCount)
	case fabric.ActionEscalate:
		s.escalatePhase(ctx, phaseID, bp, nil)
	case fabric.ActionProceed:
		// Pushback handler overrode the block — unblock and mark as
		// overridden so future dispatch cycles skip polling for this phase.
		s.Blocked.Unblock(phaseID)
		s.Blocked.Override(phaseID)
	}
}

// HandleEscalation is the exported entry point for escalation from external
// callers like the WaveScanner's OnEscalate callback. It delegates to
// escalatePhase with no markFailed handler, since wave-scanned phases are
// not yet in-flight and have no phase tracker entry to mark.
func (s *Scheduler) HandleEscalation(ctx context.Context, phaseID string, bp *fabric.BlockedPhase) {
	s.escalatePhase(ctx, phaseID, bp, nil)
}

// escalatePhase transitions a blocked phase to HUMAN_DECISION_REQUIRED.
// It unblocks the phase, updates the fabric state, logs the escalation
// message, and optionally calls markFailed to update the phase tracker.
// When OnHail is set, a discovery is posted and surfaced.
func (s *Scheduler) escalatePhase(ctx context.Context, phaseID string, bp *fabric.BlockedPhase, markFailed func(phaseID string)) {
	s.Blocked.Unblock(phaseID)

	if setErr := s.Fabric.SetPhaseState(ctx, phaseID, fabric.StateHumanDecision); setErr != nil {
		fmt.Fprintf(s.logger(), "warning: failed to set fabric state for %q: %v\n", phaseID, setErr)
	}

	maxRetries := fabric.DefaultMaxRetries
	if s.Pushback.MaxRetries > 0 {
		maxRetries = s.Pushback.MaxRetries
	}
	msg := fabric.EscalationMessage(bp, maxRetries)

	fmt.Fprintf(s.logger(), "\n── Fabric Escalation ──────────────────────────────\n%s───────────────────────────────────────────────────\n\n", msg)

	// Surface via OnHail callback if configured.
	if s.OnHail != nil {
		disc := fabric.Discovery{
			SourceTask: phaseID,
			Kind:       fabric.DiscoveryRequirementsAmbiguity,
			Detail:     fmt.Sprintf("Phase %q escalated: %s", phaseID, bp.LastResult.Reason),
		}
		if _, postErr := s.Fabric.PostDiscovery(ctx, disc); postErr != nil {
			fmt.Fprintf(s.logger(), "warning: failed to post escalation discovery for %q: %v\n", phaseID, postErr)
		}
		s.OnHail(phaseID, disc)
	}

	if markFailed != nil {
		markFailed(phaseID)
	}
}

// logger returns the effective log writer (io.Discard if Logger is nil).
func (s *Scheduler) logger() io.Writer {
	if s.Logger != nil {
		return s.Logger
	}
	return io.Discard
}
