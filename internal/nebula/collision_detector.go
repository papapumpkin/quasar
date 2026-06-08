package nebula

import "fmt"

// Collision kinds classify what two phases contend over.
const (
	// CollisionKindScope marks a conflict where two phases' scope globs
	// intersect, so they would write into overlapping files concurrently.
	CollisionKindScope = "scope"
)

// Recommended collision actions. These are advisory hints surfaced to
// operators and the scheduler; the runtime currently always defers (waits).
const (
	// CollisionActionWait means the candidate should wait until the
	// conflicting phase completes, then run against the updated tree.
	CollisionActionWait = "wait"
	// CollisionActionAbandon means the candidate is fully subsumed and need
	// not run. Reserved for future use.
	CollisionActionAbandon = "abandon"
	// CollisionActionMergeAfter means both may run and reconcile at a merge
	// gate. Reserved for future use.
	CollisionActionMergeAfter = "merge_after"
)

// Collision describes a coordination conflict between a candidate phase and a
// phase it would otherwise run concurrently with. It is the structured form of
// the scope-overlap rule: emitting it lets the scheduler defer the candidate
// deterministically and lets the TUI explain *why* a phase is waiting, instead
// of two coders racing into the same file and one silently losing its work.
type Collision struct {
	// Symbol is the overlapping scope pattern (e.g. "internal/runtime/**").
	// When the two phases match via different patterns, both are shown.
	Symbol string
	// PhaseID is the candidate phase being deferred — the phase that would
	// lose its work if it ran concurrently. Reporting surfaces (e.g. the TUI)
	// use it to name which phase is waiting.
	PhaseID string
	// OtherPhaseID is the conflicting phase — already in flight or about to
	// dispatch in the same batch.
	OtherPhaseID string
	// Kind classifies the collision (see CollisionKind* constants).
	Kind string
	// Action is the recommended resolution (see CollisionAction* constants).
	Action string
	// Reason is a human-readable explanation suitable for operator display.
	Reason string
}

// CollisionDetector is the single source of truth for the scope-overlap rule:
// two phases whose scope globs intersect must not run concurrently unless one
// opts in via AllowScopeOverlap. It is consulted by the PhaseTracker to defer
// dispatch and by reporting surfaces (e.g. the TUI) to explain deferrals.
type CollisionDetector struct {
	phasesByID map[string]*PhaseSpec
}

// NewCollisionDetector builds a detector over the given phase lookup. The map
// is used only by the ID-based helpers; WouldCollide itself is pure.
func NewCollisionDetector(phasesByID map[string]*PhaseSpec) *CollisionDetector {
	return &CollisionDetector{phasesByID: phasesByID}
}

// WouldCollide returns one Collision for each phase in others whose scope
// overlaps the candidate's. A phase with empty scope, or either party setting
// AllowScopeOverlap, never collides. The candidate is skipped if it appears in
// others (a phase cannot collide with itself).
func (c *CollisionDetector) WouldCollide(candidate *PhaseSpec, others []*PhaseSpec) []Collision {
	if candidate == nil || len(candidate.Scope) == 0 || candidate.AllowScopeOverlap {
		return nil
	}

	var collisions []Collision
	for _, other := range others {
		if other == nil || other.ID == candidate.ID {
			continue
		}
		if len(other.Scope) == 0 || other.AllowScopeOverlap {
			continue
		}
		patA, patB, overlaps := scopesOverlap(candidate.Scope, other.Scope)
		if !overlaps {
			continue
		}
		symbol := patA
		if patA != patB {
			symbol = patA + " / " + patB
		}
		collisions = append(collisions, Collision{
			Symbol:       symbol,
			PhaseID:      candidate.ID,
			OtherPhaseID: other.ID,
			Kind:         CollisionKindScope,
			Action:       CollisionActionWait,
			Reason: fmt.Sprintf("phases %q and %q both claim scope %s",
				candidate.ID, other.ID, symbol),
		})
	}
	return collisions
}

// wouldCollideIDs resolves the given phase IDs to specs and delegates to
// WouldCollide. IDs with no known spec are skipped.
func (c *CollisionDetector) wouldCollideIDs(candidate *PhaseSpec, ids []string) []Collision {
	if candidate == nil {
		return nil
	}
	others := make([]*PhaseSpec, 0, len(ids))
	for _, id := range ids {
		if spec := c.phasesByID[id]; spec != nil {
			others = append(others, spec)
		}
	}
	return c.WouldCollide(candidate, others)
}
