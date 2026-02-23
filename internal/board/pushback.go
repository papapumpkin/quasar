package board

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PushbackAction indicates what the dispatch loop should do with a blocked phase.
type PushbackAction string

const (
	// ActionRetry means the phase should wait and re-poll after board changes.
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
type PushbackHandler struct {
	// Board provides access to phase state and contract queries.
	Board Board

	// MaxRetries is the maximum number of auto-retries before escalating.
	// Zero value defaults to DefaultMaxRetries.
	MaxRetries int

	// RetryDelay is the minimum time between retries. Zero means immediate
	// retry on board change.
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
// current board state for conflict classification.
func (h *PushbackHandler) Handle(ctx context.Context, bp *BlockedPhase, inProgress []string, snap BoardSnapshot) PushbackAction {
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
// plausibly produce the missing info, the phase retries. Otherwise it escalates
// after exhausting retries.
func (h *PushbackHandler) handleNeedInfo(_ context.Context, bp *BlockedPhase, inProgress []string) PushbackAction {
	if hasPlausibleProducer(bp.LastResult.MissingInfo, inProgress) {
		return ActionRetry
	}
	// No in-progress phase can help — check retry budget.
	if bp.RetryCount >= h.maxRetries() {
		return ActionEscalate
	}
	return ActionRetry
}

// handleConflict processes CONFLICT pushback. File-claim conflicts are
// transient (the owning phase will release claims on completion) so we retry.
// Interface/contract conflicts are structural and escalate immediately.
func (h *PushbackHandler) handleConflict(_ context.Context, bp *BlockedPhase, snap BoardSnapshot) PushbackAction {
	if isFileClaimConflict(bp.LastResult.ConflictWith, snap) {
		return ActionRetry
	}
	// Interface or contract conflict — requires human judgment.
	return ActionEscalate
}

// hasPlausibleProducer returns true if any in-progress phase ID appears to
// match the missing info tokens. This is a heuristic: if a missing info item
// contains a phase ID (or vice versa), that phase might produce the needed
// contract. The caller should treat this as an optimistic signal.
func hasPlausibleProducer(missingInfo []string, inProgress []string) bool {
	for _, phaseID := range inProgress {
		for _, info := range missingInfo {
			lower := strings.ToLower(info)
			phLower := strings.ToLower(phaseID)
			if strings.Contains(lower, phLower) || strings.Contains(phLower, lower) {
				return true
			}
		}
	}
	return false
}

// isFileClaimConflict returns true if the conflicting phase holds any file
// claims in the snapshot. File-claim conflicts are transient because the
// claiming phase will release files when it completes.
func isFileClaimConflict(conflictWith string, snap BoardSnapshot) bool {
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
		return "add missing dependency to nebula or provide the contract manually"
	case PollConflict:
		if r.ConflictWith != "" {
			return fmt.Sprintf("resolve conflict with phase %q or adjust phase scopes", r.ConflictWith)
		}
		return "resolve the conflicting contracts or adjust phase scopes"
	default:
		return "review phase configuration"
	}
}
