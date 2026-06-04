package nebula

import (
	"context"
	"fmt"
	"os"

	"github.com/papapumpkin/quasar/internal/bus"
)

// Run executes the full nebula lifecycle: load, validate, branch, plan, apply,
// and (if Auto is set) execute workers. It publishes lifecycle events to the
// bus and returns an EngineResult. Additional options are forwarded to the
// WorkerGroup for display-path-specific configuration (runner, bus, etc.).
//
// The context controls cancellation — canceling ctx will stop workers
// gracefully after their current cycle.
func (e *Engine) Run(ctx context.Context, opts ...Option) *EngineResult {
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
	results, err := e.execute(ctx, opts...)
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

// load reads and validates the nebula from disk, resolves manifest-dependent
// defaults (workDir, maxWorkers, maxContextTokens), and loads state.
func (e *Engine) load(_ context.Context) (*Nebula, error) {
	n, err := Load(e.cfg.NebulaDir)
	if err != nil {
		return nil, fmt.Errorf("load nebula: %w", err)
	}
	if valErrs := Validate(n); len(valErrs) > 0 {
		return nil, &ValidationFailedError{
			Name:       n.Manifest.Nebula.Name,
			PhaseCount: len(n.Phases),
			Errors:     valErrs,
		}
	}

	// Resolve workDir: manifest overrides config; cwd is the fallback.
	if n.Manifest.Context.WorkingDir != "" {
		e.cfg.WorkDir = n.Manifest.Context.WorkingDir
	}
	if e.cfg.WorkDir == "" || e.cfg.WorkDir == "." {
		wd, wdErr := os.Getwd()
		if wdErr != nil {
			return nil, fmt.Errorf("get working directory: %w", wdErr)
		}
		e.cfg.WorkDir = wd
	}

	// Resolve manifest-dependent defaults when the CLI didn't set them.
	if !e.cfg.MaxWorkersExplicit && n.Manifest.Execution.MaxWorkers > 0 {
		e.cfg.MaxWorkers = n.Manifest.Execution.MaxWorkers
	}
	if !e.cfg.MaxContextTokensExplicit && n.Manifest.Execution.MaxContextTokens > 0 {
		e.cfg.MaxContextTokens = n.Manifest.Execution.MaxContextTokens
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
	plan, err := BuildPlan(ctx, e.nebula, e.state)
	if err != nil {
		return nil, fmt.Errorf("build plan: %w", err)
	}
	// Publish plan for consumers (TUI plan view, web dashboard).
	ev := bus.New(bus.KindPlanReady)
	ev.PlanReady = &bus.PlanReadyPayload{
		Plan:      plan,
		NebulaDir: e.cfg.NebulaDir,
	}
	e.publishEvent(ctx, ev)
	return plan, nil
}

// applyPlan executes the plan actions (record phase tracking state).
func (e *Engine) applyPlan(ctx context.Context, plan *Plan) error {
	if err := Apply(ctx, plan, e.nebula, e.state); err != nil {
		return fmt.Errorf("apply plan: %w", err)
	}
	return nil
}

// execute creates and runs the WorkerGroup. Additional options are appended
// after the base options, allowing the CLI to inject runner, bus subscribers,
// dashboard, and other display-path-specific configuration.
func (e *Engine) execute(ctx context.Context, extraOpts ...Option) ([]WorkerResult, error) {
	opts := e.buildWorkerOptions()
	opts = append(opts, extraOpts...)
	e.wg = NewWorkerGroup(e.nebula, e.state, opts...)
	return e.wg.Run(ctx)
}

// postComplete handles the post-execution git workflow: commit remaining
// changes and push the branch.
func (e *Engine) postComplete(ctx context.Context, _ []WorkerResult) *PostCompletionResult {
	return PostCompletion(ctx, e.cfg.WorkDir, e.branchName)
}

// buildWorkerOptions constructs the []Option list for WorkerGroup creation.
// These are the base options shared by all display paths (TUI and stderr).
// The caller appends display-path-specific options via execute's extraOpts.
func (e *Engine) buildWorkerOptions() []Option {
	committer := NewGitCommitterWithBranch(context.Background(), e.cfg.WorkDir, e.branchName)
	opts := []Option{
		WithMaxWorkers(e.cfg.MaxWorkers),
		WithGlobalCycles(e.cfg.MaxReviewCycles),
		WithGlobalBudget(e.cfg.MaxBudgetUSD),
		WithGlobalModel(e.cfg.Model),
		WithBus(e.bus),
		WithInvoker(e.invoker),
		WithCommitter(committer),
		WithCheckpointDir(e.cfg.NebulaDir),
	}
	if e.cfg.Resume {
		opts = append(opts, WithResumeEnabled(true))
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

// GetPlan returns the current plan, or nil if buildPlan has not been called.
func (e *Engine) GetPlan() *Plan { return e.plan }

// GetNebula returns the loaded nebula, or nil if load has not been called.
func (e *Engine) GetNebula() *Nebula { return e.nebula }

// GetState returns the loaded state, or nil if load has not been called.
func (e *Engine) GetState() *State { return e.state }

// WorkDir returns the resolved working directory. Only valid after Run
// has progressed past the loading phase.
func (e *Engine) WorkDir() string { return e.cfg.WorkDir }

// BranchName returns the nebula branch name. Empty if branch management
// is not active. Only valid after Run has progressed past branch creation.
func (e *Engine) BranchName() string { return e.branchName }

// Config returns the engine configuration (by value). The returned config
// includes any manifest-derived overrides applied during loading.
func (e *Engine) Config() EngineConfig { return e.cfg }

// Load loads, validates, and resolves the nebula: reads the manifest, checks
// phases, resolves workDir from the manifest, creates or checks out the git
// branch, and loads persisted state. After Load returns successfully, GetPlan,
// GetNebula, GetState, WorkDir, and BranchName are available.
func (e *Engine) Load(ctx context.Context) error {
	e.transition(EngineLoading)
	e.publishLifecycle(ctx, bus.KindEngineLoading)
	n, err := e.load(ctx)
	if err != nil {
		e.transition(EngineDone)
		e.publishLifecycle(ctx, bus.KindEngineDone)
		return err
	}
	e.nebula = n
	if err := e.createBranch(ctx); err != nil {
		e.transition(EngineDone)
		e.publishLifecycle(ctx, bus.KindEngineDone)
		return err
	}
	return nil
}

// Plan builds the execution plan by comparing nebula phases against current
// bead state. Does NOT apply bead changes — call ApplyPlan separately so the
// caller can display the plan in between. The result is available via GetPlan.
func (e *Engine) Plan(ctx context.Context) error {
	e.transition(EnginePlanning)
	e.publishLifecycle(ctx, bus.KindEnginePlanning)
	plan, err := e.buildPlan(ctx)
	if err != nil {
		e.transition(EngineDone)
		e.publishLifecycle(ctx, bus.KindEngineDone)
		return err
	}
	e.plan = plan
	return nil
}

// ApplyPlan applies the execution plan to beads (create/update/close).
// No-op if the plan has no changes. Must be called after Plan.
func (e *Engine) ApplyPlan(ctx context.Context) error {
	if e.plan == nil {
		return fmt.Errorf("no plan built; call Plan first")
	}
	return e.applyPlan(ctx, e.plan)
}

// Execute creates the WorkerGroup with the given options and runs all ready
// phases. The caller provides display-path-specific options (runner, bus,
// dashboard, etc.) via extraOpts. Call SetFabric before Execute if fabric
// coordination is needed. Must be called after ApplyPlan.
//
// The caller owns the fabric lifecycle — Execute does not close it.
func (e *Engine) Execute(ctx context.Context, extraOpts ...Option) ([]WorkerResult, error) {
	e.transition(EngineExecuting)
	e.publishLifecycle(ctx, bus.KindEngineExecuting)
	return e.execute(ctx, extraOpts...)
}

// PostComplete runs the post-execution git workflow: commit remaining changes,
// push the branch. Returns nil if branch management is not active.
func (e *Engine) PostComplete(ctx context.Context) *PostCompletionResult {
	e.transition(EngineCompleting)
	e.publishLifecycle(ctx, bus.KindEngineCompleting)
	if e.branchName == "" {
		e.transition(EngineDone)
		e.publishLifecycle(ctx, bus.KindEngineDone)
		return nil
	}
	result := PostCompletion(ctx, e.cfg.WorkDir, e.branchName)
	e.transition(EngineDone)
	e.publishLifecycle(ctx, bus.KindEngineDone)
	return result
}

// toErrors converts a slice of ValidationError to a slice of error.
func toErrors(valErrs []ValidationError) []error {
	errs := make([]error, len(valErrs))
	for i := range valErrs {
		errs[i] = &valErrs[i]
	}
	return errs
}
