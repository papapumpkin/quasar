+++
id = "effective-parallelism"
title = "Compute effective parallelism per wave from graph width and scope overlap"
type = "feature"
priority = 1
depends_on = ["scope-validation"]
scope = ["internal/nebula/parallelism.go"]
+++

## Problem

WorkerGroup uses a flat `max_workers` semaphore for all waves. This wastes resources on narrow waves (a linear chain has max parallelism of 1) and can't account for scope-serialized phases that reduce true concurrency even within a wide wave.

## Solution

Create `internal/nebula/parallelism.go` with a function that computes the effective parallelism for a given wave, accounting for graph width and scope overlap.

### Core function

```go
// EffectiveParallelism computes the maximum useful workers for a wave.
// It starts with the wave width (number of phases), caps at maxWorkers,
// then reduces for phases that must serialize due to scope overlap
// without a dependency relationship.
func EffectiveParallelism(wave Wave, phases []PhaseSpec, graph *Graph, maxWorkers int) int
```

### Algorithm

1. Start with `min(len(wave.PhaseIDs), maxWorkers)` — can't use more workers than phases in the wave
2. Build a "conflict graph" among the wave's phases: two phases conflict if `scopesOverlap` returns true AND `!graph.Connected(a, b)` AND neither has `AllowScopeOverlap`
3. The effective parallelism is the size of the maximum independent set in the conflict graph (phases that can all run simultaneously without scope conflicts)
4. For small waves (typical case), this is tractable. For large waves, fall back to a greedy approximation: iterate phases, add to the independent set if no conflict with already-added phases

### Scope overlap reuse

Extract `scopesOverlap` from `validate.go` into a shared unexported function (or keep it in validate.go and call it from here — both files are in package `nebula`). The function is already package-private so no API change needed.

### Helper for WorkerGroup

```go
// WaveParallelism computes effective parallelism for each wave in order.
// Returns a slice parallel to waves with the max useful workers per wave.
func WaveParallelism(waves []Wave, phases []PhaseSpec, graph *Graph, maxWorkers int) []int
```

## Files to Modify

- `internal/nebula/parallelism.go` — New file: EffectiveParallelism + WaveParallelism functions

## Acceptance Criteria

- [ ] Wave with 3 independent, non-overlapping phases → effective parallelism = 3
- [ ] Wave with 3 phases, two overlapping scopes → effective parallelism = 2
- [ ] Wave with 1 phase → effective parallelism = 1 regardless of max_workers
- [ ] max_workers = 2 with 5-phase wave → effective parallelism = 2
- [ ] Phases with AllowScopeOverlap don't reduce parallelism
- [ ] `go test ./internal/nebula/...` passes
- [ ] `go vet ./...` passes
