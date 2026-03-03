+++
id = "engine-extract"
title = "Extract nebula lifecycle from runNebulaApply into Engine"
type = "task"
priority = 1
depends_on = ["engine-types", "producer-migration"]
scope = ["internal/nebula/engine.go", "internal/nebula/engine_test.go"]
+++

## Problem

The `runNebulaApply` function in `cmd/nebula_apply.go` is ~567 lines that owns the entire nebula lifecycle: config resolution, nebula loading, validation, state management, branch creation, plan building, plan application, fabric initialization, snapshot scanning, worker group construction, TUI/stderr branching, worker execution, signal handling, post-completion git workflow, and nebula chaining. This makes it impossible to reuse the execution lifecycle from other entry points (web dashboard, constellation scheduler, canvas "apply" action).

With the bus infrastructure (phases 1-5) and engine types (phase 6) in place, the actual extraction can happen. The `Engine` struct owns the lifecycle; consumers (CLI, web, constellation) create an `Engine`, subscribe to its bus, and call `Engine.Run(ctx)`.

## Solution

Implement the `Engine` methods in `internal/nebula/engine.go`. The engine encapsulates the sequential lifecycle that `runNebulaApply` currently performs inline.

### Engine.Run

The main entry point. It orchestrates the full lifecycle:

```go
// Run executes the full nebula lifecycle: load, validate, plan, apply,
// and (if Auto is set) execute workers. It publishes lifecycle events
// to the bus and returns an EngineResult.
//
// The context controls cancellation — cancelling ctx will stop workers
// gracefully after their current cycle.
func (e *Engine) Run(ctx context.Context) *EngineResult {
    result := &EngineResult{}

    // Phase 1: Load
    e.transition(EngineLoading)
    e.publishLifecycle(bus.KindEngineLoading)
    n, err := e.load(ctx)
    if err != nil {
        result.Err = err
        e.transition(EngineDone)
        return result
    }
    e.nebula = n

    // Phase 2: Branch (optional)
    if err := e.createBranch(ctx); err != nil {
        result.Err = err
        e.transition(EngineDone)
        return result
    }

    // Phase 3: Plan
    e.transition(EnginePlanning)
    e.publishLifecycle(bus.KindEnginePlanning)
    plan, err := e.plan(ctx)
    if err != nil {
        result.Err = err
        e.transition(EngineDone)
        return result
    }
    result.Plan = plan
    e.plan = plan

    // Phase 4: Apply plan to beads
    if err := e.applyPlan(ctx, plan); err != nil {
        result.Err = err
        e.transition(EngineDone)
        return result
    }

    // Phase 5: Execute workers (if Auto)
    if !e.cfg.Auto {
        e.transition(EngineDone)
        return result
    }

    e.transition(EngineExecuting)
    e.publishLifecycle(bus.KindEngineExecuting)
    results, err := e.execute(ctx)
    result.WorkerResults = results
    if err != nil {
        result.Err = err
    }

    // Phase 6: Post-completion
    e.transition(EngineCompleting)
    e.publishLifecycle(bus.KindEngineCompleting)
    gitResult, err := e.postComplete(ctx, results)
    result.GitResult = gitResult
    if err != nil && result.Err == nil {
        result.Err = err
    }

    e.transition(EngineDone)
    e.publishLifecycle(bus.KindEngineDone)
    return result
}
```

### Internal lifecycle methods

Each method corresponds to a block of logic currently in `runNebulaApply`:

```go
// load reads and validates the nebula from disk.
func (e *Engine) load(ctx context.Context) (*Nebula, error) {
    n, err := Load(e.cfg.NebulaDir)
    if err != nil {
        return nil, fmt.Errorf("load nebula: %w", err)
    }
    if errs := Validate(n); len(errs) > 0 {
        return nil, fmt.Errorf("validate nebula: %w", errors.Join(toErrors(errs)...))
    }
    e.state, err = LoadState(e.cfg.NebulaDir)
    if err != nil {
        return nil, fmt.Errorf("load state: %w", err)
    }
    return n, nil
}

// createBranch creates or checks out the nebula git branch.
func (e *Engine) createBranch(ctx context.Context) error {
    e.branchMgr = NewBranchManager(ctx, e.cfg.WorkDir, e.nebula.Manifest.Nebula.Name)
    name, err := e.branchMgr.CreateOrCheckout(ctx)
    if err != nil {
        return fmt.Errorf("branch: %w", err)
    }
    e.branchName = name
    return nil
}

// plan builds and optionally gates the execution plan.
func (e *Engine) plan(ctx context.Context) (*Plan, error) {
    plan, err := BuildPlan(ctx, e.nebula, e.state, e.beadsClient)
    if err != nil {
        return nil, fmt.Errorf("build plan: %w", err)
    }
    // Publish plan for consumers (TUI plan view, web dashboard).
    e.bus.Publish(ctx, bus.Event{Kind: bus.KindPlanReady, Plan: plan})
    return plan, nil
}

// applyPlan executes the plan actions (create/update/close beads).
func (e *Engine) applyPlan(ctx context.Context, plan *Plan) error {
    if err := Apply(ctx, plan, e.nebula, e.state, e.beadsClient); err != nil {
        return fmt.Errorf("apply plan: %w", err)
    }
    return nil
}

// execute creates and runs the WorkerGroup.
func (e *Engine) execute(ctx context.Context) ([]WorkerResult, error) {
    opts := e.buildWorkerOptions()
    e.wg = NewWorkerGroup(e.nebula, e.state, opts...)
    return e.wg.Run(ctx)
}

// postComplete handles git branching, commit, and push.
func (e *Engine) postComplete(ctx context.Context, results []WorkerResult) (*PostCompletionResult, error) {
    allSucceeded := true
    for _, r := range results {
        if r.Err != nil {
            allSucceeded = false
            break
        }
    }
    return PostCompletion(ctx, e.cfg.WorkDir, e.branchName, allSucceeded)
}
```

### buildWorkerOptions

This method constructs the `[]Option` list currently scattered through `runNebulaApply`:

```go
func (e *Engine) buildWorkerOptions() []Option {
    opts := []Option{
        WithMaxWorkers(e.cfg.MaxWorkers),
        WithBeadsClient(e.beadsClient),
        WithGlobalCycles(e.cfg.MaxReviewCycles),
        WithGlobalBudget(e.cfg.MaxBudgetUSD),
        WithGlobalModel(e.cfg.Model),
        WithBus(e.bus),
    }
    if e.cfg.Resume {
        opts = append(opts, WithResumeEnabled(true), WithCheckpointDir(e.cfg.NebulaDir))
    }
    if e.fabric != nil {
        opts = append(opts, e.fabric.WorkerGroupOptions()...)
    }
    return opts
}
```

### State transitions

```go
func (e *Engine) transition(phase EnginePhase) {
    e.mu.Lock()
    e.phase = phase
    e.mu.Unlock()
}

func (e *Engine) publishLifecycle(kind string) {
    if e.bus != nil {
        _ = e.bus.Publish(context.Background(), bus.Event{Kind: kind})
    }
}

// Phase returns the current lifecycle phase (safe for concurrent reads).
func (e *Engine) Phase() EnginePhase {
    e.mu.Lock()
    defer e.mu.Unlock()
    return e.phase
}
```

### Fabric initialization

The engine initializes fabric when in auto mode, matching the current `initFabric` call in `runNebulaApply`:

```go
// initFabric creates the fabric instance for cross-phase coordination.
func (e *Engine) initFabric(ctx context.Context) error {
    fc, err := initFabricForNebula(ctx, e.nebula, e.cfg.NebulaDir, e.cfg.WorkDir, e.invoker)
    if err != nil {
        return fmt.Errorf("init fabric: %w", err)
    }
    e.fabric = fc
    return nil
}
```

## Files

- `internal/nebula/engine.go` — `Engine.Run`, `load`, `createBranch`, `plan`, `applyPlan`, `execute`, `postComplete`, `buildWorkerOptions`, `initFabric`, `transition`, `publishLifecycle`, `Phase`
- `internal/nebula/engine_test.go` — tests for: lifecycle state transitions (idle→loading→planning→executing→completing→done), error in load short-circuits to done, plan-only mode (Auto=false) stops after planning, bus receives lifecycle events in order, nil bus does not panic

## Acceptance Criteria

- [ ] `Engine.Run(ctx)` executes the full lifecycle: load→validate→branch→plan→apply→execute→postComplete
- [ ] `Engine.Run(ctx)` returns `EngineResult` with populated `WorkerResults`, `GitResult`, `Plan`, and `Err`
- [ ] If `Auto` is false, `Engine.Run` stops after plan application and returns the plan
- [ ] Lifecycle state transitions are published to the bus as events
- [ ] `Engine.Phase()` reflects the current lifecycle phase at any point
- [ ] Context cancellation propagates to all sub-operations
- [ ] Errors at any phase short-circuit to `EngineDone` with the error in `EngineResult.Err`
- [ ] `buildWorkerOptions()` produces the same option set as the current `runNebulaApply`
- [ ] Fabric initialization is called when `Auto` is true
- [ ] No dependency on Cobra, Viper, or `tea.Program` from `Engine`
- [ ] `go build ./internal/nebula/...` compiles
- [ ] `go vet ./internal/nebula/...` passes
- [ ] `go test ./internal/nebula/...` passes
