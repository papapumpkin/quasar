package worker

import (
	"fmt"
	"sort"

	"github.com/papapumpkin/quasar/internal/dag"
)

// Scheduler bridges the dag.TaskAnalyzer to the nebula worker group,
// providing impact-aware scheduling and track-based parallelism.
// It builds the DAG engine's graph from nebula phase specs, runs impact
// scoring and track partitioning, and exposes ready-task queries that
// return phases sorted by composite impact score.
type Scheduler struct {
	analyzer *dag.TaskAnalyzer
	scores   map[string]float64
	tracks   []dag.Track
	trackMap map[string]int // taskID -> track ID
}

// NewScheduler creates a Scheduler from a set of phase specs. It builds
// the underlying TaskAnalyzer, runs impact scoring and track partitioning,
// and prepares the scheduler for ready-task queries.
func NewScheduler(phases []PhaseSpec) (*Scheduler, error) {
	ta := dag.NewTaskAnalyzer()

	for _, p := range phases {
		if err := ta.AddTask(p.ID, p.Priority, nil); err != nil {
			return nil, fmt.Errorf("adding task %q: %w", p.ID, err)
		}
	}
	for _, p := range phases {
		for _, dep := range p.DependsOn {
			if err := ta.AddDependency(p.ID, dep); err != nil {
				return nil, fmt.Errorf("adding dependency %q -> %q: %w", p.ID, dep, err)
			}
		}
	}

	if err := ta.Analyze(); err != nil {
		return nil, fmt.Errorf("analyzing DAG: %w", err)
	}

	tracks := ta.Tracks()
	trackMap := make(map[string]int, ta.Len())
	for _, t := range tracks {
		for _, id := range t.NodeIDs {
			trackMap[id] = t.ID
		}
	}

	return &Scheduler{
		analyzer: ta,
		scores:   ta.ImpactScores(),
		tracks:   tracks,
		trackMap: trackMap,
	}, nil
}

// ReadyTasks returns task IDs whose dependencies are all in the done set,
// sorted by composite impact score (highest first). This ensures
// high-impact bottleneck phases are scheduled before leaf nodes.
func (s *Scheduler) ReadyTasks(done map[string]bool) []string {
	ready := s.analyzer.ReadyWithDone(done)

	// Sort by impact score descending for impact-aware scheduling.
	scores := s.scores
	sort.Slice(ready, func(i, j int) bool {
		return scores[ready[i]] > scores[ready[j]]
	})
	return ready
}

// AllPending returns all phase IDs that are not in the done set,
// sorted by impact score (highest first). Unlike ReadyTasks, this
// does not filter by DAG dependency satisfaction — all non-complete
// phases are candidates. This enables soft-DAG dispatch when the
// fabric's wave scanner and contract poller handle ordering and safety.
func (s *Scheduler) AllPending(done map[string]bool) []string {
	var pending []string
	for _, id := range s.analyzer.DAG().Nodes() {
		if !done[id] {
			pending = append(pending, id)
		}
	}
	// Sort by impact score descending.
	scores := s.scores
	sort.Slice(pending, func(i, j int) bool {
		return scores[pending[i]] > scores[pending[j]]
	})
	return pending
}

// Tracks returns the independent parallel tracks. Each track can be
// assigned to a separate worker without risk of dependency conflict.
func (s *Scheduler) Tracks() []dag.Track {
	return s.tracks
}

// ImpactScores returns the composite impact score for every task.
func (s *Scheduler) ImpactScores() map[string]float64 {
	return s.scores
}

// TrackForTask returns the track ID that a task belongs to.
// Returns -1 if the task is not found.
func (s *Scheduler) TrackForTask(taskID string) int {
	if id, ok := s.trackMap[taskID]; ok {
		return id
	}
	return -1
}

// Analyzer returns the underlying TaskAnalyzer for advanced queries.
func (s *Scheduler) Analyzer() *dag.TaskAnalyzer {
	return s.analyzer
}

// TrackParallelism computes the effective number of parallel workers
// based on independent tracks. The result is the minimum of the number
// of tracks and maxWorkers, ensuring we don't over-allocate workers
// beyond the available parallelism.
func TrackParallelism(tracks []dag.Track, maxWorkers int) int {
	n := len(tracks)
	if n == 0 {
		return 0
	}
	if maxWorkers < n {
		return maxWorkers
	}
	return n
}

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
