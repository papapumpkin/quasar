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
	ID         string    // Unique identifier for this hail.
	PhaseID    string    // Phase that raised this hail (empty in single-task loop mode).
	Cycle      int       // Cycle number when the hail was created.
	SourceRole string    // Role that raised the hail ("coder" or "reviewer").
	Kind       HailKind  // Classification of the hail.
	Summary    string    // One-line human-readable description.
	Detail     string    // Full context for the human to make a decision.
	Options    []string  // Optional: choices the human can pick from.
	Resolution string    // Filled when a human responds.
	ResolvedAt time.Time // Timestamp of human response (zero value if unresolved).
	CreatedAt  time.Time // Timestamp when the hail was posted.
}

// IsResolved reports whether this hail has been resolved by a human.
func (h *Hail) IsResolved() bool {
	return !h.ResolvedAt.IsZero()
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
}

// MemoryHailQueue is a concurrency-safe, in-memory implementation of HailQueue.
// It is suitable for single-process use and does not persist across restarts.
type MemoryHailQueue struct {
	mu    sync.Mutex
	hails []Hail
	seq   int // monotonic counter for generating IDs when empty
}

// NewMemoryHailQueue creates a ready-to-use in-memory hail queue.
func NewMemoryHailQueue() *MemoryHailQueue {
	return &MemoryHailQueue{}
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
