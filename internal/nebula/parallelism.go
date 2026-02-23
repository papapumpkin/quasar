package nebula

import "github.com/papapumpkin/quasar/internal/dag"

// EffectiveParallelism computes the maximum useful workers for a wave.
// It starts with the wave width (number of phases), caps at maxWorkers,
// then reduces for phases that must serialize due to scope overlap
// without a dependency relationship.
func EffectiveParallelism(wave Wave, phases []PhaseSpec, d *dag.DAG, maxWorkers int) int {
	n := len(wave.NodeIDs)
	if n == 0 {
		return 0
	}
	if maxWorkers < n {
		n = maxWorkers
	}

	// Build a lookup from phase ID to spec for the wave's phases.
	specByID := make(map[string]PhaseSpec, len(phases))
	for _, p := range phases {
		specByID[p.ID] = p
	}

	// Collect the wave's phase specs.
	waveSpecs := make([]PhaseSpec, 0, len(wave.NodeIDs))
	for _, id := range wave.NodeIDs {
		if spec, ok := specByID[id]; ok {
			waveSpecs = append(waveSpecs, spec)
		}
	}

	// Build a conflict graph: two phases conflict if their scopes overlap,
	// they are not connected by a dependency, and neither opts out.
	conflicts := make(map[string]map[string]bool)
	for i := 0; i < len(waveSpecs); i++ {
		for j := i + 1; j < len(waveSpecs); j++ {
			a, b := waveSpecs[i], waveSpecs[j]

			// Either phase opts out of overlap checking.
			if a.AllowScopeOverlap || b.AllowScopeOverlap {
				continue
			}

			// Connected by dependency — serialized, no conflict.
			if d.Connected(a.ID, b.ID) {
				continue
			}

			if _, _, overlaps := scopesOverlap(a.Scope, b.Scope); overlaps {
				if conflicts[a.ID] == nil {
					conflicts[a.ID] = make(map[string]bool)
				}
				if conflicts[b.ID] == nil {
					conflicts[b.ID] = make(map[string]bool)
				}
				conflicts[a.ID][b.ID] = true
				conflicts[b.ID][a.ID] = true
			}
		}
	}

	// No conflicts — full parallelism up to maxWorkers.
	if len(conflicts) == 0 {
		return n
	}

	// Greedy maximum independent set: iterate phases in wave order,
	// add to the independent set if no conflict with already-added phases.
	// For small waves (typical) this is optimal or near-optimal.
	independent := make(map[string]bool)
	for _, spec := range waveSpecs {
		conflict := false
		for member := range independent {
			if conflicts[spec.ID][member] {
				conflict = true
				break
			}
		}
		if !conflict {
			independent[spec.ID] = true
		}
	}

	result := len(independent)
	if result > n {
		result = n
	}
	return result
}

// WaveParallelism computes effective parallelism for each wave in order.
// Returns a slice parallel to waves with the max useful workers per wave.
func WaveParallelism(waves []Wave, phases []PhaseSpec, d *dag.DAG, maxWorkers int) []int {
	result := make([]int, len(waves))
	for i, wave := range waves {
		result[i] = EffectiveParallelism(wave, phases, d, maxWorkers)
	}
	return result
}
