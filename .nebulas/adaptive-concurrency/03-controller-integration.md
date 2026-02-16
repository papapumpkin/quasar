+++
id = "controller-integration"
title = "Integrate Controller into WorkerGroup dispatch loop"
type = "feature"
priority = 1
depends_on = ["controller"]
scope = ["internal/nebula/worker.go"]
+++

## Problem

The Controller exists but isn't wired into the dispatch loop. WorkerGroup needs to call `Decide()` between waves and resize the semaphore accordingly.

## Solution

Add an optional `*Controller` field to `WorkerGroup`. When present, the dispatch loop queries the controller between waves.

### Dispatch loop changes

In `WorkerGroup.Run`, between waves:

1. After all phases in a wave complete, gather `WaveMetrics` and `[]PhaseMetrics` from `wg.Metrics`
2. Compute the next wave's effective parallelism ceiling via `EffectiveParallelism()`
3. Call `controller.Decide(ceiling, waveMetrics, phaseMetrics)` → get new worker count + reason
4. Resize the semaphore to the new worker count
5. Log the decision via `OnProgress` or stderr: "Wave 2: concurrency adjusted to 3 (clean wave, increasing +1)"

### Backward compatibility

- `Controller` nil → use static Layer 1 effective parallelism (current behavior after local-parallelism nebula)
- `Controller` non-nil but `Metrics` nil → controller has no data, uses initial workers from strategy params

### Warm start integration

At the start of `Run`, if both `Controller` and `Metrics` are set:
1. Call `LoadMetrics(nebula.Dir)` to get historical data
2. Call `controller.WarmStart(historicalMetrics.Waves)` to initialize from last run

### WorkerGroup field

```go
type WorkerGroup struct {
    // ... existing fields ...
    Controller *Controller // optional, nil = static parallelism
}
```

### Logging

Each wave decision is logged to stderr:
```
  Wave 1: 3 workers (ceiling: 3, strategy: balanced)
  Wave 2: 2 workers (1 conflict in wave 1, reducing — balanced)
  Wave 3: 3 workers (clean wave 2, increasing +1 — balanced)
```

## Files to Modify

- `internal/nebula/worker.go` — Add Controller field, call Decide between waves, resize semaphore

## Acceptance Criteria

- [ ] Nil Controller → same behavior as Layer 1
- [ ] Controller adjusts semaphore between waves
- [ ] Warm start loads historical metrics
- [ ] Wave decisions logged to stderr
- [ ] Semaphore never exceeds effective parallelism ceiling
- [ ] Existing tests pass unchanged
- [ ] `go test ./internal/nebula/...` passes
- [ ] `go vet ./...` passes
