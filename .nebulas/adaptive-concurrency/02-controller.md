+++
id = "controller"
title = "Implement AIMD feedback controller"
type = "feature"
priority = 1
depends_on = ["strategy-config"]
scope = ["internal/nebula/controller.go"]
+++

## Problem

Worker concurrency is static — set once at the start and never adjusted. The controller needs to read runtime metrics after each wave and adjust the worker count for the next wave.

## Solution

Create `internal/nebula/controller.go` with the feedback controller.

### Core type

```go
// Controller adjusts worker concurrency between waves based on runtime metrics.
type Controller struct {
    params  StrategyParams
    ceiling int     // Layer 1 effective parallelism (absolute max)
    current int     // current concurrency level
    history []WaveDecision
}

// WaveDecision records the controller's decision for a wave.
type WaveDecision struct {
    WaveNumber    int
    Ceiling       int // effective parallelism for this wave
    Chosen        int // what the controller picked
    Conflicts     int // conflicts observed
    AvgSatisfaction string
    Reason        string // human-readable explanation
}

func NewController(params StrategyParams, initialCeiling int) *Controller
```

### Decision function

```go
// Decide computes the worker count for the next wave.
// It takes the ceiling (Layer 1 effective parallelism) and metrics from the
// previous wave. Returns the new concurrency level and a human-readable reason.
func (c *Controller) Decide(ceiling int, prev *WaveMetrics, phaseMetrics []PhaseMetrics) (workers int, reason string)
```

### AIMD logic (balanced strategy)

1. Start at `min(params.InitialWorkers, ceiling)` (or ceiling if InitialWorkers is 0)
2. After a clean wave (conflicts < threshold, satisfaction >= floor):
   - `current = min(current + params.AdditiveIncrease, ceiling)`
   - Reason: "clean wave, increasing +N"
3. After a conflicted wave (conflicts >= threshold):
   - `current = max(1, int(float64(current) * params.MultiplicativeDecrease))`
   - Reason: "N conflicts, reducing to M"
4. After an expensive wave (avg cost/phase > cost ceiling):
   - `current = max(1, current - 1)`
   - Reason: "cost ceiling exceeded, reducing to M"
5. Never exceed ceiling. Never go below 1.

### Strategy variants

- **speed**: high initial, aggressive increase (+2), gentle decrease (0.75), high conflict threshold (3)
- **cost**: low initial (1), cautious increase (+1), aggressive decrease (0.5), low cost ceiling
- **quality**: start at ceiling, no increase, decrease on low satisfaction
- **balanced**: moderate initial, +1 increase, 0.5 decrease, threshold of 1

### Warm start

```go
// WarmStart initializes the controller from historical metrics.
// Uses the last run's final concurrency level as the starting point.
func (c *Controller) WarmStart(history []WaveMetrics)
```

## Files to Modify

- `internal/nebula/controller.go` — New file: Controller, WaveDecision, Decide, WarmStart

## Acceptance Criteria

- [ ] Clean wave → concurrency increases (bounded by ceiling)
- [ ] Conflicted wave → concurrency decreases (multiplicative)
- [ ] Never exceeds ceiling, never below 1
- [ ] All four strategies produce distinct behavior
- [ ] WarmStart uses historical data
- [ ] WaveDecision history is recorded for debugging
- [ ] `go vet ./...` passes
