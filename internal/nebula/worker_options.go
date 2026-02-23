package nebula

import (
	"context"
	"io"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/board"
)

// PhaseRunnerResult holds the outcome of a single phase execution.
type PhaseRunnerResult struct {
	TotalCostUSD   float64
	CyclesUsed     int
	Report         *agent.ReviewReport
	BaseCommitSHA  string // HEAD at start of the phase
	FinalCommitSHA string // last cycle's sealed SHA (or current HEAD as fallback)
}

// PhaseRunner is the interface for executing a phase (satisfied by loop.Loop).
type PhaseRunner interface {
	RunExistingPhase(ctx context.Context, phaseID, beadID, phaseTitle, phaseDescription string, exec ResolvedExecution) (*PhaseRunnerResult, error)
	GenerateCheckpoint(ctx context.Context, beadID, phaseDescription string) (string, error)
}

// ProgressFunc is called after each phase status change to report progress.
// Parameters: completed, total, openBeads, closedBeads, totalCostUSD.
type ProgressFunc func(completed, total, openBeads, closedBeads int, totalCostUSD float64)

// gateSignal communicates a gate decision from a worker goroutine back to the dispatch loop.
type gateSignal struct {
	phaseID string
	action  GateAction
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

// WithBoard sets the contract board. When non-nil, the dispatch loop polls
// phases against the board before launching worker goroutines and publishes
// contracts on completion. Nil preserves legacy (no-board) behavior.
func WithBoard(b board.Board) Option {
	return func(wg *WorkerGroup) { wg.Board = b }
}

// WithPoller sets the board poller used to check if a phase has enough
// context to proceed. Only used when Board is also set.
func WithPoller(p board.Poller) Option {
	return func(wg *WorkerGroup) { wg.Poller = p }
}

// WithPublisher sets the contract publisher used to extract and publish
// interface contracts after a phase completes. Only used when Board is
// also set.
func WithPublisher(p *board.Publisher) Option {
	return func(wg *WorkerGroup) { wg.Publisher = p }
}
