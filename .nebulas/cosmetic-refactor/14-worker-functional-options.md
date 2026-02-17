+++
id = "worker-functional-options"
title = "Apply functional options pattern to WorkerGroup construction"
type = "task"
priority = 2
depends_on = ["nebula-extract-phases-by-id"]
scope = ["internal/nebula/worker.go", "internal/nebula/apply.go"]
+++

## Problem

`WorkerGroup` in `internal/nebula/worker.go` is constructed with 17+ public fields:

```go
wg := &WorkerGroup{
    Phases:           phases,
    Graph:            g,
    Runner:           runner,
    Beads:            beads,
    UI:               ui,
    State:            state,
    Gate:             gateMode,
    Model:            model,
    MaxReviewCycles:  maxCycles,
    MaxBudgetUSD:     budget,
    WorkDir:          workDir,
    Metrics:          metrics,
    Dashboard:        dashboard,
    Watcher:          watcher,
    Architect:        architect,
    MaxWorkers:       maxWorkers,
    AutoCheckpoint:   autoCP,
    // ...
}
```

This makes construction verbose, hard to read, and fragile — every new option requires updating all call sites. Default values are implicit (zero values) rather than explicit.

## Solution

Apply the idiomatic Go functional options pattern:

1. Define an `Option` type and option constructor functions:
   ```go
   // Option configures a WorkerGroup.
   type Option func(*WorkerGroup)

   func WithGate(mode string) Option {
       return func(wg *WorkerGroup) { wg.Gate = mode }
   }

   func WithModel(model string) Option {
       return func(wg *WorkerGroup) { wg.Model = model }
   }

   func WithMaxWorkers(n int) Option {
       return func(wg *WorkerGroup) { wg.MaxWorkers = n }
   }

   func WithMaxReviewCycles(n int) Option {
       return func(wg *WorkerGroup) { wg.MaxReviewCycles = n }
   }

   func WithMaxBudgetUSD(b float64) Option {
       return func(wg *WorkerGroup) { wg.MaxBudgetUSD = b }
   }

   func WithDashboard(d *Dashboard) Option {
       return func(wg *WorkerGroup) { wg.Dashboard = d }
   }

   func WithWatcher(w *Watcher) Option {
       return func(wg *WorkerGroup) { wg.Watcher = w }
   }

   func WithAutoCheckpoint(enabled bool) Option {
       return func(wg *WorkerGroup) { wg.AutoCheckpoint = enabled }
   }

   func WithArchitect(a *Architect) Option {
       return func(wg *WorkerGroup) { wg.Architect = a }
   }
   ```

2. Create a `NewWorkerGroup` constructor that takes required params positionally and options variadically:
   ```go
   // NewWorkerGroup creates a WorkerGroup with required dependencies and optional configuration.
   func NewWorkerGroup(
       phases []*Phase,
       graph *Graph,
       runner PhaseRunner,
       beads beads.Client,
       ui UI,
       state *State,
       workDir string,
       opts ...Option,
   ) *WorkerGroup {
       wg := &WorkerGroup{
           Phases:          phases,
           Graph:           graph,
           Runner:          runner,
           Beads:           beads,
           UI:              ui,
           State:           state,
           WorkDir:         workDir,
           MaxWorkers:      1,    // sensible default
           MaxReviewCycles: 5,    // sensible default
           Gate:            "trust",
       }
       for _, opt := range opts {
           opt(wg)
       }
       return wg
   }
   ```

3. Make optional fields unexported where possible (fields only set via options and read internally).

4. Update `Apply()` in `apply.go` to use `NewWorkerGroup(...)` with options.

## Files

- `internal/nebula/worker.go` — add `Option` type, option functions, `NewWorkerGroup` constructor; make optional fields unexported
- `internal/nebula/apply.go` — update `WorkerGroup` construction to use `NewWorkerGroup`

## Acceptance Criteria

- [ ] `NewWorkerGroup` constructor exists with required params + variadic options
- [ ] All optional configuration uses `With*` option functions
- [ ] Sensible defaults are set in the constructor (not reliant on zero values)
- [ ] `Apply()` uses the new constructor
- [ ] `go test ./...` passes
- [ ] `go vet ./...` passes
