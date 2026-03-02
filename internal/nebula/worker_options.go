package nebula

import (
	"context"
	"io"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// PhaseRunnerResult holds the outcome of a single phase execution.
type PhaseRunnerResult struct {
	TotalCostUSD   float64
	CyclesUsed     int
	Report         *agent.ReviewReport
	BaseCommitSHA  string             // HEAD at start of the phase
	FinalCommitSHA string             // last cycle's sealed SHA (or current HEAD as fallback)
	CycleCommits   []string           // per-cycle sealed commit SHAs (index = cycle-1)
	Decompose      bool               // true if the loop exited due to a struggle signal
	StruggleReason string             // human-readable reason from StruggleSignal.Reason
	AllFindings    []DecomposeFinding // accumulated findings at time of decomposition
}

// PhaseRunner is the interface for executing a phase (satisfied by loop.Loop).
type PhaseRunner interface {
	RunExistingPhase(ctx context.Context, phaseID, beadID, phaseTitle, phaseDescription string, exec ResolvedExecution) (*PhaseRunnerResult, error)
	GenerateCheckpoint(ctx context.Context, beadID, phaseDescription string) (string, error)
	// RunFromCheckpoint resumes a phase from a previously saved checkpoint.
	// The checkpointData parameter is an opaque *checkpoint.Checkpoint passed
	// as any to avoid an import cycle (nebula → checkpoint → loop → ui → nebula).
	// Implementations must type-assert to *checkpoint.Checkpoint.
	RunFromCheckpoint(ctx context.Context, checkpointData any, phaseID, beadID, phaseTitle, phaseDescription string, exec ResolvedExecution) (*PhaseRunnerResult, error)
}

// ProgressFunc is called after each phase status change to report progress.
// Parameters: completed, total, openBeads, closedBeads, totalCostUSD.
type ProgressFunc func(completed, total, openBeads, closedBeads int, totalCostUSD float64)

// gateSignal communicates a gate decision from a worker goroutine back to the dispatch loop.
type gateSignal struct {
	phaseID string
	action  GateAction
	reason  string // optional context for error messages (e.g. "fabric escalation")
}

// phaseLoopHandle tracks a running phase's refactor channel so that mid-run
// edits can be signaled to the loop without interrupting the current cycle.
type phaseLoopHandle struct {
	RefactorCh chan<- string
}

// HotAddFunc is called after a new phase is dynamically inserted into the DAG.
// Parameters: phaseID, title, dependsOn.
type HotAddFunc func(phaseID, title string, dependsOn []string)

// Option configures a WorkerGroup.
type Option func(*WorkerGroup)

// WithRunner sets the phase runner. Required before calling Run, but may be
// set after construction when the runner depends on the WorkerGroup itself.
func WithRunner(r PhaseRunner) Option {
	return func(wg *WorkerGroup) { wg.Runner = r }
}

// WithMaxWorkers sets the maximum number of concurrent phase workers.
func WithMaxWorkers(n int) Option {
	return func(wg *WorkerGroup) { wg.MaxWorkers = n }
}

// WithWatcher enables in-flight file watching for live edits.
func WithWatcher(w *Watcher) Option {
	return func(wg *WorkerGroup) { wg.Watcher = w }
}

// WithCommitter enables phase-boundary git commits.
func WithCommitter(c GitCommitter) Option {
	return func(wg *WorkerGroup) { wg.Committer = c }
}

// WithGater sets the gate strategy directly. Takes precedence over WithPrompter.
func WithGater(g Gater) Option {
	return func(wg *WorkerGroup) { wg.Gater = g }
}

// WithPrompter sets the gate prompter used for interactive modes (review, approve).
// The WorkerGroup builds the appropriate Gater strategy from this prompter and
// the manifest gate mode at run time.
func WithPrompter(p GatePrompter) Option {
	return func(wg *WorkerGroup) { wg.Prompter = p }
}

// WithDashboard enables dashboard output coordination in watch mode.
func WithDashboard(d *Dashboard) Option {
	return func(wg *WorkerGroup) { wg.Dashboard = d }
}

// WithBeadsClient sets the beads client for hot-added phase bead creation.
func WithBeadsClient(c beads.Client) Option {
	return func(wg *WorkerGroup) { wg.BeadsClient = c }
}

// WithGlobalCycles sets the default max review cycles for phases.
func WithGlobalCycles(n int) Option {
	return func(wg *WorkerGroup) { wg.GlobalCycles = n }
}

// WithGlobalBudget sets the default max budget (USD) for phases.
func WithGlobalBudget(b float64) Option {
	return func(wg *WorkerGroup) { wg.GlobalBudget = b }
}

// WithGlobalModel sets the default model override for phases.
func WithGlobalModel(m string) Option {
	return func(wg *WorkerGroup) { wg.GlobalModel = m }
}

// WithOnProgress sets a callback invoked after each phase status change.
func WithOnProgress(f ProgressFunc) Option {
	return func(wg *WorkerGroup) { wg.OnProgress = f }
}

// WithOnRefactor sets a callback invoked when a refactor is pending or dispatched.
func WithOnRefactor(f func(phaseID string, pending bool)) Option {
	return func(wg *WorkerGroup) { wg.OnRefactor = f }
}

// WithOnHotAdd sets a callback invoked after a phase is dynamically inserted.
func WithOnHotAdd(f HotAddFunc) Option {
	return func(wg *WorkerGroup) { wg.OnHotAdd = f }
}

// WithMetrics enables metrics collection.
func WithMetrics(m *Metrics) Option {
	return func(wg *WorkerGroup) { wg.Metrics = m }
}

// WithLogger sets the log output writer. Nil defaults to os.Stderr.
func WithLogger(w io.Writer) Option {
	return func(wg *WorkerGroup) { wg.Logger = w }
}

// WithFabric sets the entanglement fabric. When non-nil, the dispatch loop polls
// phases against the fabric before launching worker goroutines and publishes
// entanglements on completion. Nil preserves legacy (no-fabric) behavior.
func WithFabric(f fabric.Fabric) Option {
	return func(wg *WorkerGroup) { wg.Fabric = f }
}

// WithPoller sets the fabric poller used to check if a phase has enough
// context to proceed. Only used when Fabric is also set.
func WithPoller(p fabric.Poller) Option {
	return func(wg *WorkerGroup) { wg.Poller = p }
}

// WithPublisher sets the entanglement publisher used to extract and publish
// interface entanglements after a phase completes. Only used when Fabric is
// also set.
func WithPublisher(p *fabric.Publisher) Option {
	return func(wg *WorkerGroup) { wg.Publisher = p }
}

// WithInvoker sets the agent invoker used for architect invocations during
// auto-decomposition. Required when Execution.AutoDecompose is enabled.
func WithInvoker(inv agent.Invoker) Option {
	return func(wg *WorkerGroup) { wg.Invoker = inv }
}

// WithHealingPolicy overrides the healing policy derived from the manifest.
// This is primarily useful for testing.
func WithHealingPolicy(p HealingPolicy) Option {
	return func(wg *WorkerGroup) { wg.healingPolicy = p }
}

// WithResumeEnabled activates checkpoint-based resume, loading existing
// checkpoints to skip completed phases on startup.
func WithResumeEnabled(enabled bool) Option {
	return func(wg *WorkerGroup) { wg.ResumeEnabled = enabled }
}

// WithCheckpointDir sets the directory for reading and cleaning up checkpoint
// files. An empty value disables checkpoint load/cleanup.
func WithCheckpointDir(dir string) Option {
	return func(wg *WorkerGroup) { wg.CheckpointDir = dir }
}

// WithCheckpointLoader sets the function used to load checkpoint files.
// The function should return (nil, nil) when no checkpoint exists for a phase.
// This indirection avoids an import cycle between nebula and checkpoint.
func WithCheckpointLoader(fn func(dir, phaseID string) (any, error)) Option {
	return func(wg *WorkerGroup) { wg.CheckpointLoader = fn }
}

// WithCheckpointValidator sets the function used to validate a loaded checkpoint.
// It receives the opaque checkpoint and the current git SHA.
func WithCheckpointValidator(fn func(cp any, gitSHA string) error) Option {
	return func(wg *WorkerGroup) { wg.CheckpointValidator = fn }
}

// WithCheckpointRemover sets the function used to remove stale checkpoint files.
func WithCheckpointRemover(fn func(dir, phaseID string) error) Option {
	return func(wg *WorkerGroup) { wg.CheckpointRemover = fn }
}

// WithGitSHAFunc sets the function used to retrieve the current git SHA for
// checkpoint validation. When set alongside CheckpointValidator, loaded
// checkpoints are checked against HEAD before resume.
func WithGitSHAFunc(fn func(ctx context.Context) (string, error)) Option {
	return func(wg *WorkerGroup) { wg.GitSHAFunc = fn }
}
