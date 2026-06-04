package nebula

import (
	"fmt"
	"sync"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/bus"
)

// EngineConfig holds the fully-resolved configuration for a single nebula
// execution. All fields are resolved from CLI flags, environment, config
// file, and nebula manifest before the Engine is created. It has no
// dependency on Cobra, Viper, or BubbleTea.
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

	// MaxWorkersExplicit indicates whether MaxWorkers was explicitly set
	// by the user (e.g. via --max-workers flag). When false, the manifest
	// value is used if available.
	MaxWorkersExplicit bool

	// MaxContextTokensExplicit indicates whether MaxContextTokens was
	// explicitly set by the user. When false, the manifest value is used
	// if available.
	MaxContextTokensExplicit bool

	// FixEffort is the effort level for lint/filter fix invocations.
	FixEffort string

	// FallbackModel is the automatic fallback model when primary is overloaded.
	FallbackModel string
}

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

// ValidationFailedError is returned by Engine.Load when the nebula fails
// validation. It carries the validation context so the caller can display
// a structured error message.
type ValidationFailedError struct {
	Name       string
	PhaseCount int
	Errors     []ValidationError
}

// Error implements the error interface.
func (e *ValidationFailedError) Error() string {
	return fmt.Sprintf("nebula %q validation failed with %d error(s)", e.Name, len(e.Errors))
}

// fabricCloser is a minimal interface for the fabric lifecycle.
// It is satisfied by the cmd package's fabricComponents type.
type fabricCloser interface {
	Close() error
	WorkerGroupOptions() []Option
}

// Engine encapsulates the full nebula lifecycle: load, validate, plan,
// apply, execute, and post-completion. It publishes events to a Bus and
// owns the WorkerGroup, state, and branch manager.
type Engine struct {
	cfg   EngineConfig
	bus   bus.Bus
	phase EnginePhase

	// Dependencies injected at construction time.
	invoker agent.Invoker

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

// NewEngine creates an Engine with the given configuration and bus.
// The bus may be nil for plan-only (non-auto) mode.
func NewEngine(cfg EngineConfig, b bus.Bus, invoker agent.Invoker) *Engine {
	return &Engine{
		cfg:     cfg,
		bus:     b,
		phase:   EngineIdle,
		invoker: invoker,
	}
}
