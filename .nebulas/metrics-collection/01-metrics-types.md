+++
id = "metrics-types"
title = "Define Metrics struct and per-phase MetricSnapshot"
type = "feature"
priority = 1
depends_on = []
scope = ["internal/nebula/metrics.go"]
+++

## Problem

There is no structured way to capture runtime signals from phase execution. Data like phase duration, review cycle counts, cost, conflict restarts, and lock wait times flows through WorkerGroup and Coordinator but is discarded after each run.

## Solution

Create `internal/nebula/metrics.go` with types that capture all signals needed for Layer 3 adaptive concurrency.

### Types

```go
// PhaseMetrics captures runtime measurements for a single phase execution.
type PhaseMetrics struct {
    PhaseID       string
    WaveNumber    int
    StartedAt     time.Time
    CompletedAt   time.Time
    Duration      time.Duration
    CyclesUsed    int
    CostUSD       float64
    Restarts      int           // conflict-triggered restarts
    LockWaitTime  time.Duration // time spent waiting to acquire scope lock
    Satisfaction  string        // from ReviewReport
    Conflict      bool          // true if this phase experienced a conflict
}

// WaveMetrics captures aggregate measurements for a wave of parallel phases.
type WaveMetrics struct {
    WaveNumber          int
    EffectiveParallelism int
    ActualParallelism    int     // phases that actually ran concurrently
    PhaseCount          int
    TotalDuration       time.Duration // wall-clock time for the wave
    Conflicts           int
}

// Metrics captures all runtime measurements for a nebula execution.
type Metrics struct {
    NebulaName    string
    StartedAt     time.Time
    CompletedAt   time.Time
    TotalCostUSD  float64
    TotalPhases   int
    TotalWaves    int
    TotalConflicts int
    TotalRestarts  int
    Phases        []PhaseMetrics
    Waves         []WaveMetrics
    mu            sync.Mutex
}
```

### Methods

```go
func NewMetrics(nebulaName string) *Metrics
func (m *Metrics) RecordPhaseStart(phaseID string, wave int)
func (m *Metrics) RecordPhaseComplete(phaseID string, result PhaseRunnerResult)
func (m *Metrics) RecordConflict(phaseID string)
func (m *Metrics) RecordRestart(phaseID string)
func (m *Metrics) RecordLockWait(phaseID string, waited time.Duration)
func (m *Metrics) RecordWaveComplete(wave int, effective, actual int)
func (m *Metrics) Snapshot() Metrics // thread-safe copy for reading
```

All record methods are mutex-guarded. `Snapshot()` returns a copy for safe concurrent reading.

## Files to Modify

- `internal/nebula/metrics.go` — New file: Metrics, PhaseMetrics, WaveMetrics types + record methods

## Acceptance Criteria

- [ ] All types compile with correct field types
- [ ] Record methods are thread-safe (mutex-guarded)
- [ ] Snapshot returns a deep copy
- [ ] Zero-value Metrics is safe to use (no nil map panics)
- [ ] `go vet ./...` passes
