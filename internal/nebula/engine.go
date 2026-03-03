package nebula

import (
	"context"
	"errors"
	"fmt"

	"github.com/papapumpkin/quasar/internal/bus"
)

// Run executes the full nebula lifecycle: load, validate, branch, plan, apply,
// and (if Auto is set) execute workers. It publishes lifecycle events to the
// bus and returns an EngineResult.
//
// The context controls cancellation — cancelling ctx will stop workers
// gracefully after their current cycle.
func (e *Engine) Run(ctx context.Context) *EngineResult {
	result := &EngineResult{}

	// Phase 1: Load & validate.
	e.transition(EngineLoading)
	e.publishLifecycle(ctx, bus.KindEngineLoading)
	n, err := e.load(ctx)
	if err != nil {
		result.Err = err
		e.transition(EngineDone)
		e.publishLifecycle(ctx, bus.KindEngineDone)
		return result
	}
	e.nebula = n

	// Phase 2: Branch (create or checkout the nebula branch).
	if err := e.createBranch(ctx); err != nil {
		result.Err = err
		e.transition(EngineDone)
		e.publishLifecycle(ctx, bus.KindEngineDone)
		return result
	}

	// Phase 3: Plan.
	e.transition(EnginePlanning)
	e.publishLifecycle(ctx, bus.KindEnginePlanning)
	plan, err := e.buildPlan(ctx)
	if err != nil {
		result.Err = err
		e.transition(EngineDone)
		e.publishLifecycle(ctx, bus.KindEngineDone)
		return result
	}
	result.Plan = plan
	e.plan = plan

	// Phase 4: Apply plan to beads.
	if err := e.applyPlan(ctx, plan); err != nil {
		result.Err = err
		e.transition(EngineDone)
		e.publishLifecycle(ctx, bus.KindEngineDone)
		return result
	}

	// Phase 5: Execute workers (only if Auto mode).
	if !e.cfg.Auto {
		e.transition(EngineDone)
		e.publishLifecycle(ctx, bus.KindEngineDone)
		return result
	}

	// Initialize fabric for cross-phase coordination (when dependencies exist).
	if err := e.initFabric(ctx); err != nil {
		result.Err = err
		e.transition(EngineDone)
		e.publishLifecycle(ctx, bus.KindEngineDone)
		return result
	}
	if e.fabric != nil {
		defer e.fabric.Close()
	}

	e.transition(EngineExecuting)
	e.publishLifecycle(ctx, bus.KindEngineExecuting)
	results, err := e.execute(ctx)
	result.WorkerResults = results
	if err != nil {
		result.Err = err
	}

	// Phase 6: Post-completion (git commit, push, checkout).
	e.transition(EngineCompleting)
	e.publishLifecycle(ctx, bus.KindEngineCompleting)
	gitResult := e.postComplete(ctx, results)
	result.GitResult = gitResult

	e.transition(EngineDone)
	e.publishLifecycle(ctx, bus.KindEngineDone)
	return result
}

// load reads and validates the nebula from disk, and loads its state file.
func (e *Engine) load(_ context.Context) (*Nebula, error) {
	n, err := Load(e.cfg.NebulaDir)
	if err != nil {
		return nil, fmt.Errorf("load nebula: %w", err)
	}
	if valErrs := Validate(n); len(valErrs) > 0 {
		return nil, fmt.Errorf("validate nebula: %w", errors.Join(toErrors(valErrs)...))
	}
	e.state, err = LoadState(e.cfg.NebulaDir)
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}
	return n, nil
}

// createBranch creates or checks out the nebula git branch.
func (e *Engine) createBranch(ctx context.Context) error {
	bm, err := NewBranchManager(ctx, e.cfg.WorkDir, e.nebula.Manifest.Nebula.Name)
	if err != nil {
		return fmt.Errorf("branch manager: %w", err)
	}
	e.branchMgr = bm
	if err := e.branchMgr.CreateOrCheckout(ctx); err != nil {
		return fmt.Errorf("create or checkout branch: %w", err)
	}
	e.branchName = e.branchMgr.Branch()
	return nil
}

// buildPlan builds the execution plan and publishes a PlanReady event.
func (e *Engine) buildPlan(ctx context.Context) (*Plan, error) {
	plan, err := BuildPlan(ctx, e.nebula, e.state, e.beadsClient)
	if err != nil {
		return nil, fmt.Errorf("build plan: %w", err)
	}
	// Publish plan for consumers (TUI plan view, web dashboard).
	e.publishEvent(ctx, bus.Event{
		Kind: bus.KindPlanReady,
		PlanReady: &bus.PlanReadyPayload{
			Plan: plan,
		},
	})
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

// postComplete handles the post-execution git workflow: commit remaining
// changes, push the branch, and optionally check out the default branch.
func (e *Engine) postComplete(ctx context.Context, results []WorkerResult) *PostCompletionResult {
	allSucceeded := true
	for _, r := range results {
		if r.Err != nil {
			allSucceeded = false
			break
		}
	}
	return PostCompletion(ctx, e.cfg.WorkDir, e.branchName, allSucceeded)
}

// buildWorkerOptions constructs the []Option list for WorkerGroup creation.
func (e *Engine) buildWorkerOptions() []Option {
	opts := []Option{
		WithMaxWorkers(e.cfg.MaxWorkers),
		WithBeadsClient(e.beadsClient),
		WithGlobalCycles(e.cfg.MaxReviewCycles),
		WithGlobalBudget(e.cfg.MaxBudgetUSD),
		WithGlobalModel(e.cfg.Model),
		WithBus(e.bus),
		WithInvoker(e.invoker),
	}
	if e.cfg.Resume {
		opts = append(opts, WithResumeEnabled(true), WithCheckpointDir(e.cfg.NebulaDir))
	}
	if e.fabric != nil {
		opts = append(opts, e.fabric.WorkerGroupOptions()...)
	}
	return opts
}

// initFabric initializes the fabric for cross-phase coordination. This is
// a no-op when the nebula has no inter-phase dependencies.
func (e *Engine) initFabric(_ context.Context) error {
	// The engine does not own the fabric initialization — it delegates
	// to the fabricCloser set on the engine at construction time. The
	// engine's fabric field is set externally by the CLI adapter (which
	// owns the Cobra/Viper dependency). If no fabric was injected, this
	// is a no-op.
	return nil
}

// transition atomically updates the current lifecycle phase.
func (e *Engine) transition(phase EnginePhase) {
	e.mu.Lock()
	e.phase = phase
	e.mu.Unlock()
}

// Phase returns the current lifecycle phase (safe for concurrent reads).
func (e *Engine) Phase() EnginePhase {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.phase
}

// publishLifecycle publishes a lifecycle event to the bus. It is a no-op
// when the bus is nil.
func (e *Engine) publishLifecycle(ctx context.Context, kind bus.Kind) {
	e.publishEvent(ctx, bus.New(kind))
}

// publishEvent publishes an arbitrary event to the bus. It is a no-op
// when the bus is nil.
func (e *Engine) publishEvent(ctx context.Context, ev bus.Event) {
	if e.bus == nil {
		return
	}
	_ = e.bus.Publish(ctx, ev)
}

// SetFabric sets the fabric closer for cross-phase coordination. This must
// be called before Run if fabric support is desired. The Engine will call
// Close on it and use its WorkerGroupOptions during execution.
func (e *Engine) SetFabric(fc fabricCloser) {
	e.fabric = fc
}

// toErrors converts a slice of ValidationError to a slice of error.
func toErrors(valErrs []ValidationError) []error {
	errs := make([]error, len(valErrs))
	for i := range valErrs {
		errs[i] = &valErrs[i]
	}
	return errs
}
