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

// Loader resolves constellations and stars by name. artifacts.Loader satisfies
// it; tests inject a fake. Defined here (where consumed) per project convention.
type Loader interface {
	LoadConstellation(name string) (*artifacts.Constellation, error)
	LoadStar(name string) (*artifacts.Star, error)
}

// Committer is the git write seam. gitops.Client satisfies it. The runtime
// holds a repo-bound committer and always passes the repo's [pre_commit]
// config, so stars never see pre-commit and the runtime never decides per call.
type Committer interface {
	Commit(ctx context.Context, message string, opts gitops.CommitOpts) (string, error)
}

// Runtime executes constellation runs for a single repo. The supervisor owns
// one Runtime per registered repo and drives Step in its dispatch loop.
type Runtime struct {
	runStore  *fabric.ConstellationRunStore
	nebStore  *fabric.NebulaStore
	loader    Loader
	invoker   agent.Invoker
	committer Committer
	repoPath  string
	preCommit gitops.PreCommitConfig
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
}

// New constructs a Runtime. It panics on a nil required dependency, surfacing a
// wiring bug at boot rather than as a nil dereference mid-run.
func New(opts RuntimeOpts) *Runtime {
	if opts.RunStore == nil || opts.NebStore == nil || opts.Loader == nil {
		panic("constellations: RunStore, NebStore, and Loader are required")
	}
	return &Runtime{
		runStore:  opts.RunStore,
		nebStore:  opts.NebStore,
		loader:    opts.Loader,
		invoker:   opts.Invoker,
		committer: opts.Committer,
		repoPath:  opts.RepoPath,
		preCommit: opts.PreCommit,
	}
}

// Fire instantiates a constellation run against a nebula. It snapshots the
// constellation source for versioning, builds the initial State from the
// nebula, and inserts a run row positioned at the entry node. Execution is
// asynchronous: the supervisor drives Step until the run is terminal.
func (r *Runtime) Fire(ctx context.Context, constellationName, nebulaID, parentRunID string) (string, error) {
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
	st := NewState(SnapshotNebula(neb), now.Unix(), now.Unix())
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

	output, err := r.dispatch(ctx, run, st, node)
	if err != nil {
		return r.fail(ctx, run, st, err)
	}
	st.RecordNode(node.ID, output)

	next, err := nextTarget(con, node.ID, st.ExprState())
	if err != nil {
		return r.fail(ctx, run, st, err)
	}

	if artifacts.IsTerminal(next) {
		return r.terminate(ctx, run, st, next)
	}
	return r.persistTransition(ctx, run, st, next)
}

// Resume restores a run interrupted mid-flight. The DAG state and current node
// already live in the row, so resume is a heartbeat refresh that re-asserts the
// run as live; the supervisor then drives Step from the persisted node.
func (r *Runtime) Resume(ctx context.Context, runID string) error {
	run, err := r.runStore.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if isTerminalState(run.State) {
		return ErrTerminal
	}
	if _, err := UnmarshalState(run.DAGStateTOML); err != nil {
		return fmt.Errorf("constellations: resume %q: corrupt dag state: %w", runID, err)
	}
	return r.runStore.Heartbeat(ctx, runID)
}

// dispatch executes a single node by type and returns its outputs.
func (r *Runtime) dispatch(ctx context.Context, run *fabric.RunRow, st *State, node *artifacts.ConstellationNode) (map[string]any, error) {
	switch node.Type {
	case artifacts.NodeBuiltin:
		return r.dispatchBuiltin(ctx, st, node)
	case artifacts.NodeStar:
		return r.dispatchStar(ctx, run, st, node)
	case artifacts.NodeConstellation, artifacts.NodePhaseIterator:
		return nil, fmt.Errorf("%w: %q (node %q)", ErrNodeTypeUnsupported, node.Type, node.ID)
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

// dispatchStar resolves the star, invokes the LLM with the node's rendered
// inputs, accumulates cost, and records a star_invocation row.
func (r *Runtime) dispatchStar(ctx context.Context, run *fabric.RunRow, st *State, node *artifacts.ConstellationNode) (map[string]any, error) {
	if r.invoker == nil {
		return nil, fmt.Errorf("constellations: star node %q requires an Invoker", node.ID)
	}
	star, err := r.loader.LoadStar(node.Star)
	if err != nil {
		return nil, fmt.Errorf("constellations: load star %q: %w", node.Star, err)
	}
	args, err := evalInputs(node, st.ExprState())
	if err != nil {
		return nil, err
	}

	started := time.Now()
	res, err := r.invoker.Invoke(ctx, agent.Agent{
		Role:          agent.RoleCoder,
		SystemPrompt:  star.Prompt,
		Model:         star.Model,
		FallbackModel: star.FallbackModel,
		MaxBudgetUSD:  star.Defaults.MaxBudgetUSD,
		Effort:        star.Defaults.Effort,
		AllowedTools:  star.Tools.Allowed,
	}, userPrompt(args, st), r.repoPath)
	if err != nil {
		r.recordInvocation(ctx, run, node, star.Name, "failed", 0, started)
		return nil, fmt.Errorf("constellations: invoke star %q: %w", star.Name, err)
	}

	st.Meta.TotalCostUSD += res.CostUSD
	r.recordInvocation(ctx, run, node, star.Name, "done", res.CostUSD, started)
	return map[string]any{
		"result":     res.ResultText,
		"cost_usd":   res.CostUSD,
		"session_id": res.SessionID,
	}, nil
}

// recordInvocation persists a star_invocation row. Failure to record is logged,
// not fatal: the run's correctness does not depend on the trace.
func (r *Runtime) recordInvocation(ctx context.Context, run *fabric.RunRow, node *artifacts.ConstellationNode, starName, state string, cost float64, started time.Time) {
	_, err := r.runStore.InsertStarInvocation(ctx, fabric.StarInvocationRow{
		RunID:      run.ID,
		Seq:        run.StepIndex,
		Node:       node.ID,
		StarName:   starName,
		State:      state,
		Cycle:      run.Cycle,
		CostUSD:    cost,
		DurationMs: time.Since(started).Milliseconds(),
		StartedAt:  started.Unix(),
		EndedAt:    time.Now().Unix(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "constellations: record star invocation (run %s): %v\n", run.ID, err)
	}
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
	return StateRunning, nil
}

// terminate maps a reserved terminal target onto a run state and persists it.
func (r *Runtime) terminate(ctx context.Context, run *fabric.RunRow, st *State, target string) (string, error) {
	state := terminalState(target)
	if dag, err := MarshalState(st); err == nil {
		run.DAGStateTOML = dag
		run.CurrentNode = target
		_ = r.runStore.SaveProgress(ctx, run)
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
	return state, nil
}

// fail records the failure cause into State and marks the run failed.
func (r *Runtime) fail(ctx context.Context, run *fabric.RunRow, st *State, cause error) (string, error) {
	st.RecordNode("_error", map[string]any{"message": cause.Error(), "node": run.CurrentNode})
	if dag, err := MarshalState(st); err == nil {
		run.DAGStateTOML = dag
		_ = r.runStore.SaveProgress(ctx, run)
	}
	if err := r.runStore.Complete(ctx, run.ID, StateFailed); err != nil {
		return "", err
	}
	return StateFailed, cause
}
