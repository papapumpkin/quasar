package nebula

import (
	"sort"

	"github.com/papapumpkin/quasar/internal/dag"
)

// PhaseTracker manages phase state tracking: which phases are done, failed,
// in-flight, and which are eligible for dispatch. It operates on shared maps
// that are passed in from the orchestrator so that all collaborators can see
// the same state.
type PhaseTracker struct {
	phasesByID map[string]*PhaseSpec
	done       map[string]bool
	failed     map[string]bool
	inFlight   map[string]bool
	collisions *CollisionDetector
}

// NewPhaseTracker creates a PhaseTracker from the current nebula and state.
// It builds lookup maps and identifies which phases are already done or failed.
func NewPhaseTracker(phases []PhaseSpec, state *State) *PhaseTracker {
	pt := &PhaseTracker{
		phasesByID: PhasesByID(phases),
		done:       make(map[string]bool),
		failed:     make(map[string]bool),
		inFlight:   make(map[string]bool),
	}
	pt.collisions = NewCollisionDetector(pt.phasesByID)
	for id, ps := range state.Phases {
		if ps.Status == PhaseStatusDone {
			pt.done[id] = true
		}
		if ps.Status == PhaseStatusFailed {
			pt.failed[id] = true
			pt.done[id] = true
		}
	}
	return pt
}

// PhasesByIDMap returns the phase-spec lookup map.
func (pt *PhaseTracker) PhasesByIDMap() map[string]*PhaseSpec {
	return pt.phasesByID
}

// Done returns the set of completed phase IDs.
func (pt *PhaseTracker) Done() map[string]bool {
	return pt.done
}

// Failed returns the set of failed phase IDs.
func (pt *PhaseTracker) Failed() map[string]bool {
	return pt.failed
}

// InFlight returns the set of currently executing phase IDs.
func (pt *PhaseTracker) InFlight() map[string]bool {
	return pt.inFlight
}

// FilterEligible returns phase IDs from ready that are not in-flight, not
// failed, not blocked by a failed dependency, and not in scope-conflict with
// any currently in-flight phase. When two eligible phases would conflict on
// scope, the first one (highest impact, since ready is impact-sorted) is
// admitted and subsequent conflicting phases are deferred until the next
// dispatch cycle.
func (pt *PhaseTracker) FilterEligible(ready []string, d *dag.DAG) []string {
	inFlight := pt.inFlightIDs()
	var eligible []string
	for _, id := range ready {
		if pt.inFlight[id] || pt.failed[id] {
			continue
		}
		if pt.hasFailedDep(id, d) {
			continue
		}
		spec := pt.phasesByID[id]
		// Defer if this phase would collide with an in-flight phase, or with
		// a phase already admitted in this same batch — both would run
		// concurrently into overlapping scope.
		if len(pt.collisions.wouldCollideIDs(spec, inFlight)) > 0 {
			continue
		}
		if len(pt.collisions.wouldCollideIDs(spec, eligible)) > 0 {
			continue
		}
		eligible = append(eligible, id)
	}
	return eligible
}

// inFlightIDs returns the in-flight phase IDs in sorted order for
// deterministic collision reporting.
func (pt *PhaseTracker) inFlightIDs() []string {
	ids := make([]string, 0, len(pt.inFlight))
	for id := range pt.inFlight {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Collisions returns the scope collisions between each ready (not-yet-admitted)
// phase and the currently in-flight set. It is a read-only reporting surface
// for operators (e.g. the TUI) that explains why a phase is being deferred; it
// does not mutate scheduler state.
func (pt *PhaseTracker) Collisions(ready []string) []Collision {
	inFlight := pt.inFlightIDs()
	var all []Collision
	for _, id := range ready {
		if pt.inFlight[id] || pt.failed[id] || pt.done[id] {
			continue
		}
		all = append(all, pt.collisions.wouldCollideIDs(pt.phasesByID[id], inFlight)...)
	}
	return all
}

// hasFailedDep reports whether any direct dependency of phaseID has failed.
func (pt *PhaseTracker) hasFailedDep(phaseID string, d *dag.DAG) bool {
	for _, dep := range d.DepsFor(phaseID) {
		if pt.failed[dep] {
			return true
		}
	}
	return false
}

// MarkRemainingSkipped sets all pending/created phases to skipped status.
// Must be called with the WorkerGroup mutex held.
func (pt *PhaseTracker) MarkRemainingSkipped(phases []PhaseSpec, state *State) {
	for _, phase := range phases {
		if pt.done[phase.ID] {
			continue
		}
		ps := state.Phases[phase.ID]
		if ps == nil {
			continue
		}
		if ps.Status == PhaseStatusPending || ps.Status == PhaseStatusCreated {
			state.SetPhaseState(phase.ID, ps.BeadID, PhaseStatusSkipped)
		}
	}
}
