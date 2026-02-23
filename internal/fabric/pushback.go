package fabric

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PushbackAction indicates what the dispatch loop should do with a blocked phase.
type PushbackAction string

const (
	// ActionRetry means the phase should wait and re-poll after fabric changes.
	ActionRetry PushbackAction = "retry"

	// ActionEscalate means the situation cannot be auto-resolved and requires
	// human judgment (transitions to HUMAN_DECISION_REQUIRED).
	ActionEscalate PushbackAction = "escalate"

	// ActionProceed means the phase should override the block and start anyway.
	ActionProceed PushbackAction = "proceed"

	// DefaultMaxRetries is the default number of auto-retries before escalating.
	DefaultMaxRetries = 3
)

// PushbackHandler decides how to handle phases that push back during polling.
// It distinguishes transient blocks (a dependency will publish soon) from
// permanent ones (missing decomposition or genuine conflict) and routes
// accordingly.
//
// Retry limits: DefaultMaxRetries (used here) controls how many auto-retries
// the handler allows before escalating. This supersedes the older MaxPollRetries
// constant in poller.go, which BlockedTracker.NeedsEscalation still references
// for backward compatibility. New code should use PushbackHandler for retry
// decisions rather than calling NeedsEscalation directly.
type PushbackHandler struct {
	// Fabric provides access to phase state and entanglement queries.
	// TODO: used in step 3 (codebase check for existing symbols) — not yet implemented.
	Fabric Fabric

	// MaxRetries is the maximum number of auto-retries before escalating
	// when no plausible producer exists. When a plausible producer is found,
	// the hard cap is 2*MaxRetries to allow extra time for the producer to
	// finish, while still preventing infinite retry loops.
	// Zero value defaults to DefaultMaxRetries.
	MaxRetries int

	// RetryDelay is the minimum time between retries. Zero means immediate
	// retry on fabric change.
	// TODO: not yet referenced in retry logic — reserved for future rate-limiting.
	RetryDelay time.Duration
}

// maxRetries returns the effective max retries, applying the default when zero.
func (h *PushbackHandler) maxRetries() int {
	if h.MaxRetries <= 0 {
		return DefaultMaxRetries
	}
	return h.MaxRetries
}

// Handle processes a PollResult for a blocked phase and returns the action
// the dispatch loop should take. The inProgress parameter lists phase IDs
// that are currently executing — used to determine if a missing dependency
// might appear soon. The snap parameter provides a read-only view of the
// current fabric state for conflict classification.
func (h *PushbackHandler) Handle(ctx context.Context, bp *BlockedPhase, inProgress []string, snap FabricSnapshot) PushbackAction {
	switch bp.LastResult.Decision {
	case PollNeedInfo:
		return h.handleNeedInfo(ctx, bp, inProgress)
	case PollConflict:
		return h.handleConflict(ctx, bp, snap)
	default:
		return ActionProceed
	}
}

// handleNeedInfo processes NEED_INFO pushback. If an in-progress phase could
// plausibly produce the missing info, the phase retries — but only up to
// 2*MaxRetries to prevent infinite loops from loose heuristic matches.
// Without a plausible producer, escalation happens at MaxRetries.
func (h *PushbackHandler) handleNeedInfo(_ context.Context, bp *BlockedPhase, inProgress []string) PushbackAction {
	max := h.maxRetries()
	if hasPlausibleProducer(bp.LastResult.MissingInfo, inProgress) {
		// A running phase might produce what we need, allow extra retries
		// but still cap at 2x to avoid infinite loops from loose matching.
		if bp.RetryCount >= 2*max {
			return ActionEscalate
		}
		return ActionRetry
	}
	// No in-progress phase can help — check retry budget.
	if bp.RetryCount >= max {
		return ActionEscalate
	}
	return ActionRetry
}

// handleConflict processes CONFLICT pushback. File-claim conflicts are
// transient (the owning phase will release claims on completion) so we retry.
// Interface/entanglement conflicts are structural and escalate immediately.
func (h *PushbackHandler) handleConflict(_ context.Context, bp *BlockedPhase, snap FabricSnapshot) PushbackAction {
	if isFileClaimConflict(bp.LastResult.ConflictWith, snap) {
		return ActionRetry
	}
	// Interface or entanglement conflict — requires human judgment.
	return ActionEscalate
}

// minProducerMatchLen is the minimum length a phase ID must have to be
// considered in substring matching. Short IDs like "db" or "id" would
// otherwise match almost every missing-info string.
const minProducerMatchLen = 4

// hasPlausibleProducer returns true if any in-progress phase ID appears as a
// substring within a missing-info entry. Only the forward direction is checked
// (phase ID contained in missing-info string) to avoid false positives from
// short info tokens matching long phase IDs. Phase IDs shorter than
// minProducerMatchLen are skipped to prevent spurious matches.
func hasPlausibleProducer(missingInfo []string, inProgress []string) bool {
	for _, phaseID := range inProgress {
		if len(phaseID) < minProducerMatchLen {
			continue
		}
		phLower := strings.ToLower(phaseID)
		for _, info := range missingInfo {
			lower := strings.ToLower(info)
			if strings.Contains(lower, phLower) {
				return true
			}
		}
	}
	return false
}

// isFileClaimConflict returns true if the conflicting phase holds any file
// claims in the snapshot. File-claim conflicts are transient because the
// claiming phase will release files when it completes.
func isFileClaimConflict(conflictWith string, snap FabricSnapshot) bool {
	if conflictWith == "" {
		return false
	}
	for _, owner := range snap.FileClaims {
		if owner == conflictWith {
			return true
		}
	}
	return false
}

// EscalationMessage builds a structured message for human review when a
// blocked phase needs escalation. The message includes the phase ID, reason,
// retry history, and a suggested action.
func EscalationMessage(bp *BlockedPhase, maxRetries int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "PHASE BLOCKED: %s\n", bp.PhaseID)
	fmt.Fprintf(&b, "Reason: %s\n", bp.LastResult.Decision)
	fmt.Fprintf(&b, "Details: %s\n", bp.LastResult.Reason)
	fmt.Fprintf(&b, "Retries: %d/%d\n", bp.RetryCount, maxRetries)
	fmt.Fprintf(&b, "Suggestion: %s\n", suggestion(bp.LastResult))
	return b.String()
}

// suggestion returns a recommended action based on the poll result.
func suggestion(r PollResult) string {
	switch r.Decision {
	case PollNeedInfo:
		return "add missing dependency to nebula or provide the entanglement manually"
	case PollConflict:
		if r.ConflictWith != "" {
			return fmt.Sprintf("resolve conflict with phase %q or adjust phase scopes", r.ConflictWith)
		}
		return "resolve the conflicting entanglements or adjust phase scopes"
	default:
		return "review phase configuration"
	}
}
