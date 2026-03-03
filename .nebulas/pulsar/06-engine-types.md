+++
id = "engine-types"
title = "Define Engine types, config, and lifecycle states"
type = "task"
priority = 2
depends_on = ["producer-migration"]
scope = ["internal/nebula/engine_types.go"]
+++

## Problem

The `runNebulaApply` function in `cmd/nebula_apply.go` is ~567 lines that mixes CLI flag parsing, config resolution, nebula loading, validation, plan building, plan application, worker group construction, TUI/stderr branching, worker execution, signal handling, post-completion git workflow, and nebula chaining. Before extracting this logic into an `Engine`, we need clean type definitions for the engine's configuration, state, and lifecycle.

The engine needs to encapsulate:
- What nebula to run and with what parameters (resolved from CLI flags + config + manifest).
- Which bus to publish events to.
- The lifecycle state (idle, loading, planning, executing, completing, done).
- The result of execution.

## Solution

Create `internal/nebula/engine_types.go` with the Engine's configuration and lifecycle types.

### EngineConfig

`EngineConfig` is the fully-resolved configuration for an engine run. It contains everything needed to execute a nebula, with no Cobra/Viper dependencies:

```go
// EngineConfig holds the fully-resolved configuration for a single nebula
// execution. All fields are resolved from CLI flags, environment, config
// file, and nebula manifest before the Engine is created.
type EngineConfig struct {
    // NebulaDir is the path to the nebula directory (e.g. ".nebulas/my-nebula").
    NebulaDir string

    // WorkDir is the resolved working directory for git operations and
    // subprocess execution.
    WorkDir string

    // MaxWorkers is the resolved maximum concurrent worker count.
    MaxWorkers int

    // MaxReviewCycles is the global cycle limit (can be overridden per-phase).
    MaxReviewCycles int

    // MaxBudgetUSD is the global budget cap (can be overridden per-phase).
    MaxBudgetUSD float64

    // MaxContextTokens is the resolved max context token count for prompts.
    MaxContextTokens int

    // Model is the resolved model name (empty = default).
    Model string

    // CoderPrompt is the resolved system prompt for the coder agent.
    CoderPrompt string

    // ReviewerPrompt is the resolved system prompt for the reviewer agent.
    ReviewerPrompt string

    // Verbose enables verbose output.
    Verbose bool

    // Auto enables automatic worker execution (vs plan-only mode).
    Auto bool

    // Resume enables checkpoint resume.
    Resume bool

    // UseTUI enables TUI mode (vs stderr printer).
    UseTUI bool

    // NoSplash disables the TUI splash animation.
    NoSplash bool

    // Watch enables watching for phase file changes.
    Watch bool

    // LintCommands are the linter commands to run in the filter phase.
    LintCommands []string

    // ClaudePath is the path to the Claude CLI binary.
    ClaudePath string

    // BeadsPath is the path to the Beads CLI binary.
    BeadsPath string

    // CacheOptimization enables prompt cache optimization.
    CacheOptimization bool

    // CacheVerbose enables cache debug logging.
    CacheVerbose bool
}
```

### EngineState — lifecycle states

```go
// EnginePhase represents the current lifecycle phase of the engine.
type EnginePhase int

const (
    // EngineIdle is the initial state before Run is called.
    EngineIdle EnginePhase = iota

    // EngineLoading indicates the nebula is being loaded and validated.
    EngineLoading

    // EnginePlanning indicates the plan is being built and applied.
    EnginePlanning

    // EngineExecuting indicates workers are running phases.
    EngineExecuting

    // EngineCompleting indicates post-execution cleanup (git, checkpoints).
    EngineCompleting

    // EngineDone indicates the engine has finished (success or error).
    EngineDone
)

// String returns the human-readable name of the engine phase.
func (p EnginePhase) String() string {
    switch p {
    case EngineIdle:
        return "idle"
    case EngineLoading:
        return "loading"
    case EnginePlanning:
        return "planning"
    case EngineExecuting:
        return "executing"
    case EngineCompleting:
        return "completing"
    case EngineDone:
        return "done"
    default:
        return "unknown"
    }
}
```

### EngineResult — execution outcome

```go
// EngineResult holds the outcome of an Engine.Run invocation.
type EngineResult struct {
    // WorkerResults contains per-phase execution results. Nil if the
    // engine did not reach the execution phase (e.g. plan-only mode).
    WorkerResults []WorkerResult

    // GitResult contains post-completion git workflow results (branch
    // push, checkout). Nil if git branching was not used.
    GitResult *PostCompletionResult

    // Plan is the resolved execution plan. Always populated after
    // the planning phase.
    Plan *Plan

    // Err is the first fatal error encountered, if any.
    Err error
}
```

### Engine struct (skeleton)

Define the `Engine` struct with its dependencies. The full implementation comes in phase `engine-extract`, but the struct and constructor are defined here so types are stable:

```go
// Engine encapsulates the full nebula lifecycle: load, validate, plan,
// apply, execute, and post-completion. It publishes events to a Bus and
// owns the WorkerGroup, state, and branch manager.
type Engine struct {
    cfg    EngineConfig
    bus    bus.Bus
    phase  EnginePhase

    // Dependencies injected at construction time.
    invoker     agent.Invoker
    beadsClient *beads.CLI

    // Internal state populated during Run.
    nebula     *Nebula
    state      *State
    plan       *Plan
    branchMgr  *BranchManager
    branchName string
    wg         *WorkerGroup
    fabric     fabricCloser

    mu sync.Mutex // protects phase transitions
}

// fabricCloser is a minimal interface for the fabric lifecycle.
type fabricCloser interface {
    Close()
    WorkerGroupOptions() []Option
}

// NewEngine creates an Engine with the given configuration and bus.
// The bus may be nil for plan-only (non-auto) mode.
func NewEngine(cfg EngineConfig, b bus.Bus, invoker agent.Invoker, beadsClient *beads.CLI) *Engine {
    return &Engine{
        cfg:         cfg,
        bus:         b,
        phase:       EngineIdle,
        invoker:     invoker,
        beadsClient: beadsClient,
    }
}
```

## Files

- `internal/nebula/engine_types.go` — `EngineConfig`, `EnginePhase`, `EngineResult`, `Engine` struct skeleton, `NewEngine` constructor, `fabricCloser` interface

## Acceptance Criteria

- [ ] `go build ./internal/nebula/...` compiles
- [ ] `go vet ./internal/nebula/...` passes
- [ ] `EngineConfig` has fields for every configuration value currently parsed in `runNebulaApply`
- [ ] `EnginePhase` has all lifecycle states with `String()` method
- [ ] `EngineResult` captures worker results, git results, plan, and error
- [ ] `Engine` struct has `bus.Bus`, `EngineConfig`, injected dependencies, and internal state
- [ ] `NewEngine` constructor initializes engine in `EngineIdle` phase
- [ ] No dependency on Cobra, Viper, or `tea.Program` from `Engine` types
- [ ] All existing tests continue to pass
