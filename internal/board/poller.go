// Package board provides polling and blocked-phase tracking for the contract board.
//
// The Poller evaluates whether a phase has sufficient board context to proceed
// after its DAG dependencies are satisfied. This enables a checkpoint between
// "deps satisfied" and "start working" where the worker can inspect the board
// and decide whether it has enough context.

package board

import (
	"context"
	"time"
)

// PollDecision represents the outcome category of a polling check.
type PollDecision string

const (
	// PollProceed means the board has everything the phase needs to start.
	PollProceed PollDecision = "PROCEED"

	// PollNeedInfo means the phase is missing contracts it depends on
	// (e.g., types or interfaces that haven't been published yet).
	PollNeedInfo PollDecision = "NEED_INFO"

	// PollConflict means the phase detected a file or interface conflict
	// with another phase's published contracts or claims.
	PollConflict PollDecision = "CONFLICT"
)

// PollResult represents the outcome of a phase polling the board.
type PollResult struct {
	// Decision is the high-level verdict: proceed, need info, or conflict.
	Decision PollDecision

	// Reason is a human-readable explanation of the decision.
	Reason string

	// MissingInfo lists what's needed when Decision is PollNeedInfo
	// (e.g., contract names, type signatures, package paths).
	MissingInfo []string

	// ConflictWith identifies the phase ID causing a conflict when
	// Decision is PollConflict.
	ConflictWith string
}

// Poller evaluates whether a phase has sufficient board context to proceed.
// Implementations inspect the board snapshot and determine if the phase can
// start executing or needs to wait for more contracts.
type Poller interface {
	// Poll checks whether phaseID has enough context from the board to run.
	// It returns a PollResult indicating the decision and any details about
	// what's missing or conflicting. The snap parameter provides a read-only
	// view of the current board state.
	Poll(ctx context.Context, phaseID string, snap BoardSnapshot) (PollResult, error)
}

// BlockedPhase tracks a phase that is waiting for more board context before
// it can proceed to execution. The dispatch loop maintains a set of these
// and retries them when new contracts appear on the board.
type BlockedPhase struct {
	// PhaseID is the unique identifier of the blocked phase.
	PhaseID string

	// BlockedAt records when the phase first entered the BLOCKED state.
	BlockedAt time.Time

	// RetryCount tracks how many times the phase has been re-polled.
	RetryCount int

	// LastResult holds the most recent PollResult that kept this phase blocked.
	LastResult PollResult
}

// MaxPollRetries is the legacy default number of re-polls before a blocked phase
// escalates to HUMAN_DECISION_REQUIRED. It is used by BlockedTracker.NeedsEscalation
// for backward compatibility. New code should use PushbackHandler (which defaults to
// DefaultMaxRetries) for retry decisions instead of calling NeedsEscalation directly.
const MaxPollRetries = 5

// BlockedTracker manages the set of phases that are waiting for board context.
// It is not safe for concurrent use; callers must provide their own synchronization
// if access from multiple goroutines is needed.
type BlockedTracker struct {
	phases     map[string]*BlockedPhase
	overridden map[string]bool // phases where pushback handler chose ActionProceed
}

// NewBlockedTracker creates an empty BlockedTracker.
func NewBlockedTracker() *BlockedTracker {
	return &BlockedTracker{
		phases:     make(map[string]*BlockedPhase),
		overridden: make(map[string]bool),
	}
}

// Block records a phase as blocked with the given poll result. If the phase
// is already tracked, its retry count is incremented and the result is updated.
func (bt *BlockedTracker) Block(phaseID string, result PollResult) {
	if bp, ok := bt.phases[phaseID]; ok {
		bp.RetryCount++
		bp.LastResult = result
		return
	}
	bt.phases[phaseID] = &BlockedPhase{
		PhaseID:    phaseID,
		BlockedAt:  time.Now(),
		RetryCount: 0,
		LastResult: result,
	}
}

// Unblock removes a phase from the blocked set (e.g., when it proceeds or
// escalates).
func (bt *BlockedTracker) Unblock(phaseID string) {
	delete(bt.phases, phaseID)
}

// Override marks a phase as overridden by the pushback handler (ActionProceed
// despite a non-PROCEED poll result). Overridden phases skip future polling
// to avoid resetting retry counters in a block-unblock loop.
func (bt *BlockedTracker) Override(phaseID string) {
	bt.overridden[phaseID] = true
}

// IsOverridden returns true if the phase was previously overridden by the
// pushback handler and should skip polling.
func (bt *BlockedTracker) IsOverridden(phaseID string) bool {
	return bt.overridden[phaseID]
}

// Get returns the BlockedPhase for the given ID, or nil if not tracked.
func (bt *BlockedTracker) Get(phaseID string) *BlockedPhase {
	return bt.phases[phaseID]
}

// All returns a snapshot of all currently blocked phases. The returned slice
// contains copies, so mutating them does not affect the tracker's internal state.
func (bt *BlockedTracker) All() []BlockedPhase {
	result := make([]BlockedPhase, 0, len(bt.phases))
	for _, bp := range bt.phases {
		result = append(result, *bp)
	}
	return result
}

// NeedsEscalation returns true if the phase has exceeded MaxPollRetries,
// indicating it should transition to HUMAN_DECISION_REQUIRED.
func (bt *BlockedTracker) NeedsEscalation(phaseID string) bool {
	bp, ok := bt.phases[phaseID]
	if !ok {
		return false
	}
	return bp.RetryCount >= MaxPollRetries
}

// Len returns the number of currently blocked phases.
func (bt *BlockedTracker) Len() int {
	return len(bt.phases)
}
