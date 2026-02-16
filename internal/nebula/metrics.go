package nebula

import (
	"sync"
	"time"
)

// PhaseMetrics captures runtime measurements for a single phase execution.
type PhaseMetrics struct {
	PhaseID      string
	WaveNumber   int
	StartedAt    time.Time
	CompletedAt  time.Time
	Duration     time.Duration
	CyclesUsed   int
	CostUSD      float64
	Restarts     int           // conflict-triggered restarts
	LockWaitTime time.Duration // time spent waiting to acquire scope lock
	Satisfaction string        // from ReviewReport
	Conflict     bool          // true if this phase experienced a conflict
}

// WaveMetrics captures aggregate measurements for a wave of parallel phases.
type WaveMetrics struct {
	WaveNumber           int
	EffectiveParallelism int
	ActualParallelism    int // phases that actually ran concurrently
	PhaseCount           int
	TotalDuration        time.Duration // wall-clock time for the wave
	Conflicts            int
}

// Metrics captures all runtime measurements for a nebula execution.
type Metrics struct {
	NebulaName     string
	StartedAt      time.Time
	CompletedAt    time.Time
	TotalCostUSD   float64
	TotalPhases    int
	TotalWaves     int
	TotalConflicts int
	TotalRestarts  int
	Phases         []PhaseMetrics
	Waves          []WaveMetrics
	mu             sync.Mutex
}

// NewMetrics creates a Metrics instance for the given nebula name.
func NewMetrics(nebulaName string) *Metrics {
	return &Metrics{
		NebulaName: nebulaName,
		StartedAt:  time.Now(),
		Phases:     make([]PhaseMetrics, 0),
		Waves:      make([]WaveMetrics, 0),
	}
}

// RecordPhaseStart records the start of a phase execution.
func (m *Metrics) RecordPhaseStart(phaseID string, wave int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Phases = append(m.Phases, PhaseMetrics{
		PhaseID:    phaseID,
		WaveNumber: wave,
		StartedAt:  time.Now(),
	})
	m.TotalPhases++
}

// RecordPhaseComplete records the completion of a phase execution.
func (m *Metrics) RecordPhaseComplete(phaseID string, result PhaseRunnerResult) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := len(m.Phases) - 1; i >= 0; i-- {
		if m.Phases[i].PhaseID == phaseID {
			now := time.Now()
			m.Phases[i].CompletedAt = now
			m.Phases[i].Duration = now.Sub(m.Phases[i].StartedAt)
			m.Phases[i].CyclesUsed = result.CyclesUsed
			m.Phases[i].CostUSD = result.TotalCostUSD
			if result.Report != nil {
				m.Phases[i].Satisfaction = result.Report.Satisfaction
			}
			m.TotalCostUSD += result.TotalCostUSD
			break
		}
	}
}

// RecordConflict records that a phase experienced a scope conflict.
func (m *Metrics) RecordConflict(phaseID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalConflicts++
	for i := len(m.Phases) - 1; i >= 0; i-- {
		if m.Phases[i].PhaseID == phaseID {
			m.Phases[i].Conflict = true
			break
		}
	}
}

// RecordRestart records that a phase was restarted due to a conflict.
func (m *Metrics) RecordRestart(phaseID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalRestarts++
	for i := len(m.Phases) - 1; i >= 0; i-- {
		if m.Phases[i].PhaseID == phaseID {
			m.Phases[i].Restarts++
			break
		}
	}
}

// RecordLockWait records the time a phase spent waiting to acquire a scope lock.
func (m *Metrics) RecordLockWait(phaseID string, waited time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := len(m.Phases) - 1; i >= 0; i-- {
		if m.Phases[i].PhaseID == phaseID {
			m.Phases[i].LockWaitTime += waited
			break
		}
	}
}

// RecordWaveComplete records the completion of a wave of parallel phases.
func (m *Metrics) RecordWaveComplete(wave int, effective, actual int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var phaseCount, conflicts int
	var maxDuration time.Duration
	for _, p := range m.Phases {
		if p.WaveNumber == wave {
			phaseCount++
			if p.Conflict {
				conflicts++
			}
			if p.Duration > maxDuration {
				maxDuration = p.Duration
			}
		}
	}

	m.Waves = append(m.Waves, WaveMetrics{
		WaveNumber:           wave,
		EffectiveParallelism: effective,
		ActualParallelism:    actual,
		PhaseCount:           phaseCount,
		TotalDuration:        maxDuration,
		Conflicts:            conflicts,
	})
	m.TotalWaves++
}

// Snapshot returns a thread-safe deep copy of the current metrics for reading.
// The returned pointer is a new Metrics value with a fresh (unlocked) mutex.
func (m *Metrics) Snapshot() *Metrics {
	m.mu.Lock()
	defer m.mu.Unlock()

	snap := &Metrics{
		NebulaName:     m.NebulaName,
		StartedAt:      m.StartedAt,
		CompletedAt:    m.CompletedAt,
		TotalCostUSD:   m.TotalCostUSD,
		TotalPhases:    m.TotalPhases,
		TotalWaves:     m.TotalWaves,
		TotalConflicts: m.TotalConflicts,
		TotalRestarts:  m.TotalRestarts,
	}

	snap.Phases = make([]PhaseMetrics, len(m.Phases))
	copy(snap.Phases, m.Phases)

	snap.Waves = make([]WaveMetrics, len(m.Waves))
	copy(snap.Waves, m.Waves)

	return snap
}
