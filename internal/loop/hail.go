package loop

import (
	"fmt"
	"sync"
	"time"
)

// HailKind classifies the reason an agent is requesting human input.
type HailKind string

const (
	// HailDecisionNeeded indicates the agent needs a choice made by the human.
	HailDecisionNeeded HailKind = "decision_needed"
	// HailAmbiguity indicates the agent encountered unclear requirements.
	HailAmbiguity HailKind = "ambiguity"
	// HailBlocker indicates the agent cannot proceed without human input.
	HailBlocker HailKind = "blocker"
	// HailHumanReviewFlag indicates the reviewer flagged work for human eyes.
	HailHumanReviewFlag HailKind = "human_review"
)

// validHailKinds enumerates the recognized HailKind values.
var validHailKinds = map[HailKind]bool{
	HailDecisionNeeded:  true,
	HailAmbiguity:       true,
	HailBlocker:         true,
	HailHumanReviewFlag: true,
}

// ValidateHailKind returns an error if kind is not a recognized hail kind.
func ValidateHailKind(kind HailKind) error {
	if !validHailKinds[kind] {
		return fmt.Errorf("invalid hail kind %q: must be one of decision_needed, ambiguity, blocker, human_review", kind)
	}
	return nil
}

// Hail represents a structured request from an agent to the human operator.
// Hails are queued during execution and consumed asynchronously — they do not
// block the agent's current cycle.
type Hail struct {
	ID           string    // Unique identifier for this hail.
	PhaseID      string    // Phase that raised this hail (empty in single-task loop mode).
	Cycle        int       // Cycle number when the hail was created.
	SourceRole   string    // Role that raised the hail ("coder" or "reviewer").
	Kind         HailKind  // Classification of the hail.
	Summary      string    // One-line human-readable description.
	Detail       string    // Full context for the human to make a decision.
	Options      []string  // Optional: choices the human can pick from.
	Resolution   string    // Filled when a human responds or auto-resolved on timeout.
	ResolvedAt   time.Time // Timestamp of resolution (zero value if unresolved).
	CreatedAt    time.Time // Timestamp when the hail was posted.
	RelayedAt    time.Time // Timestamp when the resolution was relayed to an agent (zero if not yet relayed).
	AutoResolved bool      // True when resolved by timeout rather than human response.
}

// IsResolved reports whether this hail has been resolved (by human or timeout).
func (h *Hail) IsResolved() bool {
	return !h.ResolvedAt.IsZero()
}

// IsAutoResolved reports whether this hail was auto-resolved due to timeout
// rather than an explicit human response.
func (h *Hail) IsAutoResolved() bool {
	return h.IsResolved() && h.AutoResolved
}

// IsRelayed reports whether this hail's resolution has been relayed to an agent.
func (h *Hail) IsRelayed() bool {
	return !h.RelayedAt.IsZero()
}

// HailQueue manages the lifecycle of hails: posting, querying, and resolving.
type HailQueue interface {
	// Post adds a new hail to the queue. The hail's CreatedAt is set
	// automatically if zero.
	Post(h Hail) error

	// Unresolved returns all hails that have not yet been resolved,
	// ordered by creation time (oldest first).
	Unresolved() []Hail

	// Resolve marks the hail with the given ID as resolved with the
	// provided resolution text. Returns an error if the ID is not found.
	Resolve(id string, resolution string) error

	// UnrelayedResolved returns all hails that have been resolved but not
	// yet relayed to an agent, ordered by resolution time (oldest first).
	UnrelayedResolved() []Hail

	// MarkRelayed sets the RelayedAt timestamp on the hails with the given
	// IDs, indicating their resolutions have been injected into an agent
	// prompt. Returns an error if any ID is not found.
	MarkRelayed(ids []string) error

	// SweepExpired auto-resolves any unresolved hails that have exceeded the
	// configured timeout. Returns the hails that were just auto-resolved.
	// If timeout is 0 or no hails are expired, returns nil.
	SweepExpired() []Hail
}

// autoResolveMessage is the standard resolution text applied when a hail
// expires without human response.
const autoResolveMessage = "No human response within timeout. Agent proceeded with best judgment."

// MemoryHailQueue is a concurrency-safe, in-memory implementation of HailQueue.
// It is suitable for single-process use and does not persist across restarts.
type MemoryHailQueue struct {
	mu      sync.Mutex
	hails   []Hail
	seq     int              // monotonic counter for generating IDs when empty
	timeout time.Duration    // auto-resolve timeout; 0 disables expiry
	now     func() time.Time // clock for testability; defaults to time.Now
}

// NewMemoryHailQueue creates a ready-to-use in-memory hail queue with no
// timeout (hails wait indefinitely for human resolution).
func NewMemoryHailQueue() *MemoryHailQueue {
	return &MemoryHailQueue{now: time.Now}
}

// NewMemoryHailQueueWithTimeout creates an in-memory hail queue that
// auto-resolves unresolved hails after the given timeout. A timeout of 0
// disables auto-resolution (equivalent to NewMemoryHailQueue).
func NewMemoryHailQueueWithTimeout(timeout time.Duration) *MemoryHailQueue {
	return &MemoryHailQueue{
		timeout: timeout,
		now:     time.Now,
	}
}

// Post adds a hail to the queue. If the hail's ID is empty, a sequential ID
// is generated. CreatedAt is set to the current time if zero.
func (q *MemoryHailQueue) Post(h Hail) error {
	if err := ValidateHailKind(h.Kind); err != nil {
		return err
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	if h.ID == "" {
		q.seq++
		h.ID = fmt.Sprintf("hail-%04d", q.seq)
	}
	if h.CreatedAt.IsZero() {
		h.CreatedAt = time.Now()
	}

	q.hails = append(q.hails, h)
	return nil
}

// Unresolved returns all hails that have not yet been resolved, ordered by
// creation time (oldest first). The returned slice is a deep copy; callers
// may modify it freely without affecting the queue's internal state.
func (q *MemoryHailQueue) Unresolved() []Hail {
	q.mu.Lock()
	defer q.mu.Unlock()

	var result []Hail
	for _, h := range q.hails {
		if !h.IsResolved() {
			h.Options = append([]string(nil), h.Options...)
			result = append(result, h)
		}
	}
	return result
}

// Resolve marks the hail with the given ID as resolved. Returns an error if
// the ID does not exist or is already resolved.
func (q *MemoryHailQueue) Resolve(id string, resolution string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i := range q.hails {
		if q.hails[i].ID == id {
			if q.hails[i].IsResolved() {
				return fmt.Errorf("hail %q is already resolved", id)
			}
			q.hails[i].Resolution = resolution
			q.hails[i].ResolvedAt = time.Now()
			return nil
		}
	}
	return fmt.Errorf("hail %q not found", id)
}

// All returns every hail in the queue (both resolved and unresolved).
// The returned slice is a deep copy; callers may modify it freely without
// affecting the queue's internal state.
func (q *MemoryHailQueue) All() []Hail {
	q.mu.Lock()
	defer q.mu.Unlock()

	out := make([]Hail, len(q.hails))
	copy(out, q.hails)
	for i := range out {
		out[i].Options = append([]string(nil), out[i].Options...)
	}
	return out
}

// UnrelayedResolved returns hails that have been resolved by a human but whose
// resolutions have not yet been relayed to an agent. Results are ordered by
// resolution time (oldest first). The returned slice is a deep copy.
func (q *MemoryHailQueue) UnrelayedResolved() []Hail {
	q.mu.Lock()
	defer q.mu.Unlock()

	var result []Hail
	for _, h := range q.hails {
		if h.IsResolved() && !h.IsRelayed() {
			h.Options = append([]string(nil), h.Options...)
			result = append(result, h)
		}
	}
	return result
}

// MarkRelayed sets the RelayedAt timestamp on the hails with the given IDs.
// Returns an error if any ID is not found. Already-relayed hails are silently
// skipped to support idempotent usage.
func (q *MemoryHailQueue) MarkRelayed(ids []string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	now := time.Now()
	found := 0
	for i := range q.hails {
		if idSet[q.hails[i].ID] {
			if q.hails[i].RelayedAt.IsZero() {
				q.hails[i].RelayedAt = now
			}
			found++
		}
	}

	if found != len(idSet) {
		return fmt.Errorf("MarkRelayed: %d of %d IDs not found in queue", len(idSet)-found, len(idSet))
	}
	return nil
}

// SweepExpired auto-resolves any unresolved hails whose age exceeds the
// configured timeout. Returns a deep copy of the hails that were just
// auto-resolved. If the timeout is 0 (disabled) or no hails are expired,
// returns nil.
func (q *MemoryHailQueue) SweepExpired() []Hail {
	if q.timeout <= 0 {
		return nil
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	now := q.now()
	cutoff := now.Add(-q.timeout)
	var swept []Hail

	for i := range q.hails {
		h := &q.hails[i]
		if h.IsResolved() || h.CreatedAt.After(cutoff) {
			continue
		}
		h.Resolution = autoResolveMessage
		h.ResolvedAt = now
		h.AutoResolved = true
		// Deep copy for return value.
		cp := *h
		cp.Options = append([]string(nil), h.Options...)
		swept = append(swept, cp)
	}

	return swept
}
