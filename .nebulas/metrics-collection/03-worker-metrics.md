+++
id = "worker-metrics"
title = "Instrument WorkerGroup with phase and wave metrics"
type = "feature"
priority = 1
depends_on = ["metrics-types"]
scope = ["internal/nebula/worker.go"]
+++

## Problem

WorkerGroup executes phases and records results, but discards timing data, cycle counts relative to limits, and conflict/restart information. This data exists transiently in `executePhase` but is never captured for analysis.

## Solution

Add an optional `*Metrics` field to `WorkerGroup`. Instrument the dispatch loop and phase execution to record all runtime signals.

### Phase-level instrumentation (in executePhase)

1. `RecordPhaseStart(phaseID, waveNumber)` — when phase begins execution
2. `RecordPhaseComplete(phaseID, result)` — after phase finishes (captures duration, cycles, cost, satisfaction)
3. `RecordConflict(phaseID)` — when conflict detection triggers
4. `RecordRestart(phaseID)` — when a phase is re-queued after conflict

### Wave-level instrumentation (in Run dispatch loop)

1. `RecordWaveComplete(waveNumber, effectiveParallelism, actualParallelism)` — after all phases in a wave finish
2. Actual parallelism = max concurrent goroutines observed during the wave (track via atomic counter)

### Data already available

Most signals are already computed but discarded:
- `PhaseRunnerResult.CyclesUsed` → cycles per phase
- `PhaseRunnerResult.TotalCostUSD` → cost per phase
- `PhaseRunnerResult.Report.Satisfaction` → quality signal
- `time.Now()` at start/end → duration
- Conflict restart count → from worker-integration's restart protocol

### WorkerGroup field

```go
type WorkerGroup struct {
    // ... existing fields ...
    Metrics *Metrics // optional, nil = no collection
}
```

## Files to Modify

- `internal/nebula/worker.go` — Add Metrics field, instrument executePhase and Run dispatch loop

## Acceptance Criteria

- [ ] Phase start/complete times recorded
- [ ] Cycles used, cost, satisfaction captured per phase
- [ ] Wave effective vs actual parallelism recorded
- [ ] Conflict and restart events captured
- [ ] No behavioral change when Metrics is nil
- [ ] Existing tests pass unchanged
- [ ] `go test ./internal/nebula/...` passes
- [ ] `go vet ./...` passes
