+++
id = "dag-scheduler"
title = "DAG scheduler dispatching nebulas in dependency order"
type = "feature"
priority = 1
depends_on = ["validation"]
scope = ["internal/constellation/scheduler.go", "internal/constellation/scheduler_test.go"]
+++

## Problem

A validated `Constellation` has a DAG of nebulas with dependency relationships but no execution machinery. Each nebula is itself a multi-phase execution that can take significant time and resources. The constellation scheduler must dispatch nebulas in dependency order, respecting the DAG constraints: a nebula can only start when all its `depends_on` nebulas have completed successfully.

The existing `internal/dag/` package provides `DAG`, `Wave`, `AddNode`, `AddEdge`, and wave computation — these must be reused for the nebula-level graph rather than reimplemented.

The existing `internal/nebula/engine.go` (from the pulsar nebula) provides `Engine.Run(ctx) *EngineResult` for executing individual nebulas — the scheduler creates an `Engine` per nebula and orchestrates their lifecycle.

## Solution

### Scheduler

```go
// internal/constellation/scheduler.go

package constellation

import (
    "context"
    "fmt"
    "sync"

    "github.com/aaronsalm/quasar/internal/bus"
    "github.com/aaronsalm/quasar/internal/dag"
    "github.com/aaronsalm/quasar/internal/nebula"
)

// NebulaStatus tracks the execution state of a single nebula within
// the constellation.
type NebulaStatus string

const (
    NebulaStatusPending   NebulaStatus = "pending"
    NebulaStatusRunning   NebulaStatus = "running"
    NebulaStatusDone      NebulaStatus = "done"
    NebulaStatusFailed    NebulaStatus = "failed"
    NebulaStatusSkipped   NebulaStatus = "skipped"
    NebulaStatusRetrying  NebulaStatus = "retrying"
)

// NebulaOutcome records the result of a single nebula execution.
type NebulaOutcome struct {
    Name    string
    Status  NebulaStatus
    Result  *nebula.EngineResult
    Attempt int
}

// Scheduler manages the DAG-based execution of nebulas within a
// constellation. It dispatches nebulas in wave order, running
// independent nebulas concurrently up to the configured worker limit.
type Scheduler struct {
    constellation *Constellation
    dag           *dag.DAG
    waves         []dag.Wave
    bus           bus.Bus

    // Factory creates an Engine for a given nebula reference.
    engineFactory EngineFactory

    maxConcurrent int
    outcomes      map[string]*NebulaOutcome
    mu            sync.Mutex
}

// EngineFactory creates a configured Engine for a nebula reference.
// The constellation scheduler calls this for each nebula it dispatches.
type EngineFactory func(ref NebulaRef, b bus.Bus) (*nebula.Engine, error)

// NewScheduler creates a scheduler for the given constellation.
func NewScheduler(c *Constellation, factory EngineFactory, b bus.Bus) (*Scheduler, error) {
    // Build the nebula-level DAG.
    g := dag.New()
    for _, ref := range c.Nebulas {
        g.AddNode(dag.Node{ID: ref.Name, Priority: 1})
    }
    for _, ref := range c.Nebulas {
        for _, dep := range ref.DependsOn {
            if err := g.AddEdge(ref.Name, dep); err != nil {
                return nil, fmt.Errorf("DAG edge %s -> %s: %w", ref.Name, dep, err)
            }
        }
    }

    waves := g.Waves()
    outcomes := make(map[string]*NebulaOutcome, len(c.Nebulas))
    for _, ref := range c.Nebulas {
        outcomes[ref.Name] = &NebulaOutcome{
            Name:   ref.Name,
            Status: NebulaStatusPending,
        }
    }

    maxConcurrent := c.Coordination.MaxConcurrentNebulas
    if maxConcurrent <= 0 {
        maxConcurrent = 1
    }

    return &Scheduler{
        constellation: c,
        dag:           g,
        waves:         waves,
        bus:           b,
        engineFactory: factory,
        maxConcurrent: maxConcurrent,
        outcomes:      outcomes,
    }, nil
}
```

### Run

```go
// Run executes the constellation DAG wave by wave. Within each wave,
// independent nebulas run concurrently up to maxConcurrent. Returns
// all outcomes when the constellation is complete or fatally errored.
func (s *Scheduler) Run(ctx context.Context) ([]NebulaOutcome, error) {
    for waveIdx, wave := range s.waves {
        s.publishWaveStart(waveIdx, wave)

        // Semaphore for concurrency control within a wave.
        sem := make(chan struct{}, s.maxConcurrent)
        var wg sync.WaitGroup
        var waveErr error
        var errOnce sync.Once

        for _, nodeID := range wave.NodeIDs() {
            ref := s.findRef(nodeID)
            if ref == nil {
                continue
            }

            // Check if blocked by a failed dependency.
            if s.isBlockedByFailure(ref) {
                s.setStatus(ref.Name, NebulaStatusSkipped)
                continue
            }

            sem <- struct{}{} // Acquire semaphore slot.
            wg.Add(1)
            go func(r NebulaRef) {
                defer wg.Done()
                defer func() { <-sem }() // Release slot.

                outcome := s.executeNebula(ctx, r)
                s.mu.Lock()
                s.outcomes[r.Name] = outcome
                s.mu.Unlock()

                if outcome.Status == NebulaStatusFailed {
                    strategy := s.constellation.Coordination.FailureStrategy
                    if strategy == FailureStrategyAbort {
                        errOnce.Do(func() {
                            waveErr = fmt.Errorf("nebula %q failed, aborting constellation", r.Name)
                        })
                    }
                }
            }(*ref)
        }

        wg.Wait()
        if waveErr != nil {
            return s.allOutcomes(), waveErr
        }
    }

    return s.allOutcomes(), nil
}
```

### Execute single nebula

```go
func (s *Scheduler) executeNebula(ctx context.Context, ref NebulaRef) *NebulaOutcome {
    s.setStatus(ref.Name, NebulaStatusRunning)
    s.publishNebulaStart(ref.Name)

    engine, err := s.engineFactory(ref, s.bus)
    if err != nil {
        return &NebulaOutcome{
            Name:   ref.Name,
            Status: NebulaStatusFailed,
            Result: &nebula.EngineResult{Err: err},
        }
    }

    result := engine.Run(ctx)
    status := NebulaStatusDone
    if result.Err != nil {
        status = NebulaStatusFailed
    }

    s.publishNebulaDone(ref.Name, status)
    return &NebulaOutcome{
        Name:    ref.Name,
        Status:  status,
        Result:  result,
        Attempt: 1,
    }
}
```

### Helper methods

```go
func (s *Scheduler) findRef(name string) *NebulaRef {
    for i := range s.constellation.Nebulas {
        if s.constellation.Nebulas[i].Name == name {
            return &s.constellation.Nebulas[i]
        }
    }
    return nil
}

func (s *Scheduler) isBlockedByFailure(ref *NebulaRef) bool {
    s.mu.Lock()
    defer s.mu.Unlock()
    for _, dep := range ref.DependsOn {
        if o, ok := s.outcomes[dep]; ok && o.Status == NebulaStatusFailed {
            return true
        }
    }
    return false
}

func (s *Scheduler) setStatus(name string, status NebulaStatus) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.outcomes[name].Status = status
}

func (s *Scheduler) allOutcomes() []NebulaOutcome {
    s.mu.Lock()
    defer s.mu.Unlock()
    out := make([]NebulaOutcome, 0, len(s.outcomes))
    for _, o := range s.outcomes {
        out = append(out, *o)
    }
    return out
}

// Outcomes returns a snapshot of all nebula outcomes (safe for concurrent reads).
func (s *Scheduler) Outcomes() []NebulaOutcome {
    return s.allOutcomes()
}
```

## Files

- `internal/constellation/scheduler.go` — `Scheduler`, `NebulaStatus`, `NebulaOutcome`, `EngineFactory`, `NewScheduler`, `Run`, `executeNebula`, wave/concurrency management
- `internal/constellation/scheduler_test.go` — tests for: linear dependency chain (A→B→C runs in order), parallel independent nebulas (A∥B then C), failed nebula skips downstream, abort strategy stops constellation, max concurrent respects limit, empty constellation succeeds

## Acceptance Criteria

- [ ] `NewScheduler` builds a `dag.DAG` from constellation nebula references
- [ ] `Run` executes nebulas wave by wave, respecting dependency order
- [ ] Independent nebulas within a wave run concurrently up to `maxConcurrent`
- [ ] Failed nebula causes downstream dependents to be skipped
- [ ] `FailureStrategyAbort` stops the entire constellation on first failure
- [ ] `FailureStrategyContinue` skips dependents but continues with independent nebulas
- [ ] Each nebula execution creates an `Engine` via the `EngineFactory`
- [ ] Bus events are published for wave start, nebula start, and nebula done
- [ ] `Outcomes()` returns the current state of all nebulas (safe for concurrent reads)
- [ ] `go test ./internal/constellation/...` passes
- [ ] `go vet ./internal/constellation/...` passes
