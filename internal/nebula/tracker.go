package nebula

// PhaseTracker manages phase state tracking: which phases are done, failed,
// in-flight, and which are eligible for dispatch. It operates on shared maps
// that are passed in from the orchestrator so that all collaborators can see
// the same state.
type PhaseTracker struct {
	phasesByID map[string]*PhaseSpec
	done       map[string]bool
	failed     map[string]bool
	inFlight   map[string]bool
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

// FilterEligible returns phase IDs from ready that are not in-flight, not failed,
// and not blocked by a failed dependency.
func (pt *PhaseTracker) FilterEligible(ready []string, graph *Graph) []string {
	var eligible []string
	for _, id := range ready {
		if pt.inFlight[id] || pt.failed[id] {
			continue
		}
		if pt.hasFailedDep(id, graph) {
			continue
		}
		eligible = append(eligible, id)
	}
	return eligible
}

// hasFailedDep reports whether any direct dependency of phaseID has failed.
func (pt *PhaseTracker) hasFailedDep(phaseID string, graph *Graph) bool {
	deps, ok := graph.adjacency[phaseID]
	if !ok {
		return false
	}
	for dep := range deps {
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
