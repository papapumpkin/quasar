package constellations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/gitops"
	"github.com/papapumpkin/quasar/internal/telemetry"
)

// Terminal run states. Edge targets (_done, _failed, …) map onto these when the
// DAG walk reaches a reserved pseudo-node.
const (
	StateRunning       = "running"
	StateDone          = "done"
	StateFailed        = "failed"
	StateAwaitingHuman = "awaiting_human"
	StatePaused        = "paused"
)

var (
	// ErrUnknownNode is returned when current_node names no node in the
	// constellation (a corrupt run or an edited constellation).
	ErrUnknownNode = errors.New("constellations: current node not found in constellation")
	// ErrNoEdgeMatched is returned when a node has outgoing edges but none of
	// their `when` guards evaluated truthy — a dead end the author must fix.
	ErrNoEdgeMatched = errors.New("constellations: no outgoing edge matched")
	// ErrNodeTypeUnsupported is returned for node types not yet executable by
	// this runtime (constellation, phase_iterator). They land in a follow-up.
	ErrNodeTypeUnsupported = errors.New("constellations: node type not supported by runtime yet")
	// ErrTerminal is returned by Step when called on an already-terminal run.
	ErrTerminal = errors.New("constellations: run is already terminal")
)

// The interfaces below are the Runtime's collaborator seams, declared here per
// the project convention of defining interfaces at the point of use. Concrete
// implementations are injected from the cmd layer so the runtime never imports
// the blob/fabric/cockpit machinery directly, preserving the dependency layering.

// Loader resolves constellations and stars by name. artifacts.Loader satisfies
// it; tests inject a fake.
type Loader interface {
	LoadConstellation(name string) (*artifacts.Constellation, error)
	LoadStar(name string) (*artifacts.Star, error)
}

// Committer is the runtime's git seam (gitops.Client satisfies it). The runtime
// holds a repo-bound committer and always passes the repo's [pre_commit] config,
// so stars never see pre-commit. Commit is the only write; Diff is a read used to
// inspect a just-created commit (e.g. to mark touched entanglements in flight).
type Committer interface {
	Commit(ctx context.Context, message string, opts gitops.CommitOpts) (string, error)
	Diff(ctx context.Context, baseRef, headRef string) (string, error)
}

// EventSink receives live state-change events for a subscriber (the cockpit's
// SSE fan-out). Primitive argument types keep the runtime from importing the
// cockpit package; a nil sink disables emission, so a fleet without the cockpit
// pays nothing.
type EventSink interface {
	Emit(topic, typ string, data map[string]any)
}

// Checkpointer snapshots a run's worktree after a successful coder dispatch and
// restores the latest snapshot when a later cycle's coder dies, so the reviewer
// can fall back to a recoverable state. Granularity is per-dispatch (see the
// internal/checkpoint package comment); checkpoint.RuntimeCheckpointer is the
// injected implementation.
type Checkpointer interface {
	// Checkpoint captures the worktree for runID at the given cycle, labeled by
	// trigger. Implementations dedup an unchanged tree.
	Checkpoint(ctx context.Context, runID string, cycle int, trigger string) error
	// RestoreForReview materializes, under baseDir, partial/ (the live worktree)
	// and checkpoint/ (the latest snapshot) for runID, returning their paths.
	RestoreForReview(ctx context.Context, runID, baseDir string) (partialDir, checkpointDir string, err error)
}

// Runtime executes constellation runs for a single repo. The supervisor owns
// one Runtime per registered repo and drives Step in its dispatch loop.
type Runtime struct {
	runStore         *fabric.ConstellationRunStore
	nebStore         *fabric.NebulaStore
	loader           Loader
	invoker          agent.Invoker
	committer        Committer
	repoPath         string
	preCommit        gitops.PreCommitConfig
	budget           *Budget
	defaultBudgetUSD float64
	cacheMetrics     *telemetry.CacheMetricStore      // Optional; nil disables cache-token recording.
	checkpointer     Checkpointer                     // Optional; nil disables per-dispatch worktree checkpoints.
	entanglements    *fabric.EntanglementStore        // Optional; nil disables entanglement-lifecycle tracking.
	merger           merger                           // Optional test seam; nil builds a gitops-backed merger from repoPath.
	coordination     *Check                           // Optional; nil disables the pre-flight coordination check.
	conflictLog      *telemetry.ConflictResolutionLog // Optional; nil disables conflict-resolution telemetry (emit node no-ops).
	events           EventSink                        // Optional; nil disables live event emission (cockpit SSE).
}

// RuntimeOpts configures New. RunStore, NebStore, and Loader are required;
// Invoker is required for star nodes; Committer and PreCommit are required only
// if any star commits.
type RuntimeOpts struct {
	RunStore  *fabric.ConstellationRunStore
	NebStore  *fabric.NebulaStore
	Loader    Loader
	Invoker   agent.Invoker
	Committer Committer
	RepoPath  string
	PreCommit gitops.PreCommitConfig
	// DefaultBudgetUSD is the operator-level fallback budget cap (from the repo's
	// .quasar.yaml defaults.max_budget_usd) used at Fire time when neither an
	// explicit override nor the nebula manifest sets a budget. Zero means no
	// fallback cap.
	DefaultBudgetUSD float64
	// CacheMetrics, when non-nil, persists per-star prompt-cache token counts to
	// the JSONL log for `quasar cache report`. Nil disables recording.
	CacheMetrics *telemetry.CacheMetricStore
	// Checkpointer, when non-nil, snapshots the worktree after each successful
	// coder dispatch and restores the latest snapshot on a dead-coder
	// termination. Nil disables per-dispatch checkpointing.
	Checkpointer Checkpointer
	// Entanglements, when non-nil, drives the entanglement lifecycle: the
	// architect operator declares producer symbols and the runtime withdraws or
	// fulfills a run's entanglements as it terminates. Nil disables tracking, so
	// repos that do not coordinate cross-phase symbols pay nothing.
	Entanglements *fabric.EntanglementStore
	// Coordination, when non-nil, runs the pre-flight coordination check before
	// each coordination-aware coder dispatch and injects sibling-aware notes into
	// the prompt. Nil disables the check; the dispatch proceeds with no notes.
	Coordination *Check
	// ConflictLog, when non-nil, persists one conflict-resolution outcome row per
	// merge-conflict-resolve run (via the emit_conflict_telemetry node) for
	// `quasar conflicts report`. Nil disables recording; the emit node no-ops.
	ConflictLog *telemetry.ConflictResolutionLog
	// Events, when non-nil, receives live run/fleet state-change events for the
	// cockpit's SSE fan-out. Nil disables emission.
	Events EventSink
}

// New constructs a Runtime. It panics on a nil required dependency, surfacing a
// wiring bug at boot rather than as a nil dereference mid-run.
func New(opts RuntimeOpts) *Runtime {
	if opts.RunStore == nil || opts.NebStore == nil || opts.Loader == nil {
		panic("constellations: RunStore, NebStore, and Loader are required")
	}
	return &Runtime{
		runStore:         opts.RunStore,
		nebStore:         opts.NebStore,
		loader:           opts.Loader,
		invoker:          opts.Invoker,
		committer:        opts.Committer,
		repoPath:         opts.RepoPath,
		preCommit:        opts.PreCommit,
		budget:           NewBudget(opts.RunStore),
		defaultBudgetUSD: opts.DefaultBudgetUSD,
		cacheMetrics:     opts.CacheMetrics,
		checkpointer:     opts.Checkpointer,
		entanglements:    opts.Entanglements,
		coordination:     opts.Coordination,
		conflictLog:      opts.ConflictLog,
		events:           opts.Events,
	}
}

// emit publishes a live state-change event when an EventSink is configured.
// Nil-safe: a runtime without the cockpit emits nothing.
func (r *Runtime) emit(topic, typ string, data map[string]any) {
	if r.events != nil {
		r.events.Emit(topic, typ, data)
	}
}

// Fire instantiates a constellation run against a nebula. It snapshots the
// constellation source for versioning, builds the initial State from the
// nebula, and inserts a run row positioned at the entry node. Execution is
// asynchronous: the supervisor drives Step until the run is terminal.
// budgetOverride is the explicit per-run cap (CLI --budget-usd / API caller);
// a non-positive value defers to the nebula manifest then the global default.
func (r *Runtime) Fire(ctx context.Context, constellationName, nebulaID, parentRunID string, budgetOverride float64) (string, error) {
	con, err := r.loader.LoadConstellation(constellationName)
	if err != nil {
		return "", fmt.Errorf("constellations: load %q: %w", constellationName, err)
	}
	entry, err := entryNode(con)
	if err != nil {
		return "", err
	}

	neb, err := r.nebStore.Get(ctx, nebulaID)
	if err != nil {
		return "", fmt.Errorf("constellations: load nebula %q: %w", nebulaID, err)
	}

	now := time.Now()
	st := NewState(SnapshotNebula(neb), now.Unix())
	st.Meta.MaxCycles = resolveMaxCycles(con, neb)
	dagState, err := MarshalState(st)
	if err != nil {
		return "", err
	}

	runID, err := r.runStore.InsertRun(ctx, fabric.RunRow{
		RepoPath:          r.repoPath,
		NebulaID:          nebulaID,
		ConstellationName: constellationName,
		Snapshot:          snapshotSource(con),
		ParentRunID:       parentRunID,
		State:             StateRunning,
		CurrentNode:       entry.ID,
		DAGStateTOML:      dagState,
	})
	if err != nil {
		return "", err
	}

	// Resolve and seed the run's budget cap. An uncapped result (no override, no
	// manifest budget, no global default) leaves the budget columns NULL so the
	// CheckBefore gate is a no-op for this run.
	if err := r.budget.Initialize(ctx, runID, resolveBudget(budgetOverride, neb, r.defaultBudgetUSD)); err != nil {
		return "", err
	}
	return runID, nil
}

// Step advances a run by exactly one node: it dispatches the current node,
// records the node's outputs into State, evaluates outgoing edges to pick the
// next node, and persists the transition. It returns the run's new state.
// Calling Step on a terminal run returns ErrTerminal.
func (r *Runtime) Step(ctx context.Context, runID string) (string, error) {
	run, err := r.runStore.GetRun(ctx, runID)
	if err != nil {
		return "", err
	}
	if isTerminalState(run.State) {
		return run.State, ErrTerminal
	}

	con, err := r.loader.LoadConstellation(run.ConstellationName)
	if err != nil {
		return "", fmt.Errorf("constellations: load %q: %w", run.ConstellationName, err)
	}
	st, err := UnmarshalState(run.DAGStateTOML)
	if err != nil {
		return "", err
	}
	node := findNode(con, run.CurrentNode)
	if node == nil {
		return r.fail(ctx, run, st, fmt.Errorf("%w: %q", ErrUnknownNode, run.CurrentNode))
	}

	// Poison-node guard: bump the durable attempt counter before dispatching.
	// A node that crash-loops (panic-on-restart, OOM) is failed here rather
	// than re-dispatched forever, so it can't take the fleet down on every boot.
	// handled means the guard already failed the run (state+cause mirror the
	// normal fail path); a non-nil err with handled=false is a store error.
	if state, handled, err := r.guardAttempt(ctx, run, st, node); handled || err != nil {
		return state, err
	}

	// Keep the run's heartbeat fresh across a potentially minutes-long dispatch
	// (a star invocation, or an entire nested-constellation walk) so a crash
	// reaper can distinguish a live long step from a dead process. dispatchSafely
	// recovers a node panic into a normal failure so one bad node can't crash
	// the fleet process.
	stopHeartbeat := r.startHeartbeat(ctx, run.ID, heartbeatInterval)
	output, err := r.dispatchSafely(ctx, run, st, node)
	stopHeartbeat()
	if err != nil {
		if errors.Is(err, ErrBudgetExhausted) {
			return r.failBudget(ctx, run, st, node)
		}
		return r.fail(ctx, run, st, err)
	}
	st.RecordNode(node.ID, output)

	next, err := nextTarget(con, node.ID, st.ExprState())
	if err != nil {
		return r.fail(ctx, run, st, err)
	}

	// A back-edge (a transition to an earlier-declared node) is one loop
	// iteration. Incrementing here is what makes the declarative cycle cap bite:
	// the next time the loop's routing node evaluates its `when` guards, `cycle`
	// has advanced, so meta.max_cycles is eventually exceeded and the give-up
	// fallback edge wins. The new count persists with the transition below.
	if isBackEdge(con, node.ID, next) {
		st.Cycle++
	}

	if artifacts.IsTerminal(next) {
		return r.terminate(ctx, run, st, next)
	}
	return r.persistTransition(ctx, run, st, next)
}

// dispatch executes a single node by type and returns its outputs.
func (r *Runtime) dispatch(ctx context.Context, run *fabric.RunRow, st *State, node *artifacts.ConstellationNode) (map[string]any, error) {
	switch node.Type {
	case artifacts.NodeBuiltin:
		out, err := r.dispatchBuiltin(ctx, st, node)
		if err == nil && node.Op == opCommitName {
			// The commit node is the green-build gate: its [pre_commit] hooks ran
			// and produced a commit. Mark every symbol the commit touched in
			// flight so a sibling's pre-flight sees the freshest signature.
			r.markInFlightFromCommit(ctx, run, out)
		}
		return out, err
	case artifacts.NodeStar:
		return r.dispatchStar(ctx, run, st, node)
	case artifacts.NodeConstellation:
		return r.dispatchConstellation(ctx, run, st, node)
	case artifacts.NodePhaseIterator:
		return r.dispatchPhaseIterator(ctx, run, st, node)
	default:
		return nil, fmt.Errorf("constellations: unknown node type %q (node %q)", node.Type, node.ID)
	}
}

// dispatchBuiltin evaluates the node's inputs and invokes the named operator.
func (r *Runtime) dispatchBuiltin(ctx context.Context, st *State, node *artifacts.ConstellationNode) (map[string]any, error) {
	op, ok := lookupOperator(node.Op)
	if !ok {
		return nil, fmt.Errorf("constellations: no operator %q (node %q)", node.Op, node.ID)
	}
	args, err := evalInputs(node, st.ExprState())
	if err != nil {
		return nil, err
	}
	return op(ctx, r, st, args)
}

// commitWork is the single point where the runtime writes a commit. It always
// applies the repo's pre-commit config; callers never pass it. Returns the SHA,
// or "" when there was nothing to commit.
func (r *Runtime) commitWork(ctx context.Context, message string) (string, error) {
	if r.committer == nil {
		return "", fmt.Errorf("constellations: commit requested but no Committer configured")
	}
	sha, err := r.committer.Commit(ctx, message, gitops.CommitOpts{PreCommit: r.preCommit})
	if errors.Is(err, gitops.ErrNothingToCommit) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("constellations: commit: %w", err)
	}
	return sha, nil
}

// persistTransition advances current_node and saves DAG state for resume.
func (r *Runtime) persistTransition(ctx context.Context, run *fabric.RunRow, st *State, next string) (string, error) {
	dag, err := MarshalState(st)
	if err != nil {
		return "", err
	}
	run.State = StateRunning
	run.CurrentNode = next
	run.StepIndex++
	run.Cycle = st.Cycle
	run.DAGStateTOML = dag
	if err := r.runStore.SaveProgress(ctx, run); err != nil {
		return "", err
	}
	// Live update: the cockpit re-renders this run's card in place (it re-reads
	// the run by id, so only the run_id is needed in the payload).
	r.emit("runs", "step_completed", map[string]any{"run_id": run.ID, "node": next})
	return StateRunning, nil
}

// terminate maps a reserved terminal target onto a run state and persists it.
func (r *Runtime) terminate(ctx context.Context, run *fabric.RunRow, st *State, target string) (string, error) {
	state := terminalState(target)
	dag, err := MarshalState(st)
	if err != nil {
		fmt.Fprintf(os.Stderr, "constellations: marshal final state (run %s): %v\n", run.ID, err)
	} else {
		run.DAGStateTOML = dag
		run.CurrentNode = target
		// Keep the cycle column authoritative, symmetric with persistTransition.
		// Today a terminal target is never a back-edge so st.Cycle already matches
		// the column, but syncing here removes the footgun if that ever changes.
		run.Cycle = st.Cycle
		if err := r.runStore.SaveProgress(ctx, run); err != nil {
			fmt.Fprintf(os.Stderr, "constellations: save final state (run %s): %v\n", run.ID, err)
		}
	}
	if err := r.runStore.Complete(ctx, run.ID, state); err != nil {
		return "", err
	}
	if state == StateAwaitingHuman {
		// Surface the nebula in the TUI as awaiting a human decision.
		if err := r.nebStore.SetStatus(ctx, run.NebulaID, StateAwaitingHuman); err != nil {
			fmt.Fprintf(os.Stderr, "constellations: set nebula %s awaiting_human: %v\n", run.NebulaID, err)
		}
	}
	r.applyTerminalEntanglements(ctx, run.ID, state)
	// Live update: the run left the in-flight lane, so signal a board refresh
	// (the card moves to recent / awaiting-human on every operator's screen).
	r.emit("fleet", "nebula_status_changed", map[string]any{"nebula_id": run.NebulaID, "state": state})
	return state, nil
}

// fail records the failure cause into State and marks the run failed.
func (r *Runtime) fail(ctx context.Context, run *fabric.RunRow, st *State, cause error) (string, error) {
	st.RecordNode("_error", map[string]any{"message": cause.Error(), "node": run.CurrentNode})
	dag, err := MarshalState(st)
	if err != nil {
		fmt.Fprintf(os.Stderr, "constellations: marshal failure state (run %s): %v\n", run.ID, err)
	} else {
		run.DAGStateTOML = dag
		if err := r.runStore.SaveProgress(ctx, run); err != nil {
			fmt.Fprintf(os.Stderr, "constellations: save failure state (run %s): %v\n", run.ID, err)
		}
	}
	if err := r.runStore.Complete(ctx, run.ID, StateFailed); err != nil {
		return "", err
	}
	r.applyTerminalEntanglements(ctx, run.ID, StateFailed)
	return StateFailed, cause
}
