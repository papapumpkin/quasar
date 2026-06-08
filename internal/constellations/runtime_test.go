package constellations

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/gitops"
)

// fakeLoader serves in-memory constellations and stars.
type fakeLoader struct {
	cons  map[string]*artifacts.Constellation
	stars map[string]*artifacts.Star
}

func (f *fakeLoader) LoadConstellation(name string) (*artifacts.Constellation, error) {
	c, ok := f.cons[name]
	if !ok {
		return nil, errNotFound(name)
	}
	return c, nil
}

func (f *fakeLoader) LoadStar(name string) (*artifacts.Star, error) {
	s, ok := f.stars[name]
	if !ok {
		return nil, errNotFound(name)
	}
	return s, nil
}

type errNotFound string

func (e errNotFound) Error() string { return "not found: " + string(e) }

// fakeInvoker returns a canned result and records the agent and user prompt it saw.
type fakeInvoker struct {
	result    agent.InvocationResult
	gotAgent  agent.Agent
	gotPrompt string
}

func (f *fakeInvoker) Invoke(_ context.Context, a agent.Agent, prompt string, _ string) (agent.InvocationResult, error) {
	f.gotAgent = a
	f.gotPrompt = prompt
	return f.result, nil
}
func (f *fakeInvoker) Validate() error { return nil }

func mustExpr(t *testing.T, src string) artifacts.Expression {
	t.Helper()
	e, err := artifacts.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return e
}

// newTestRuntime builds a runtime backed by a real SQLite fabric with a seeded
// nebula, plus the provided loader and invoker.
func newTestRuntime(t *testing.T, loader Loader, inv agent.Invoker) (*Runtime, string) {
	t.Helper()
	return newTestRuntimeWithExecution(t, loader, inv, "")
}

// newTestRuntimeWithExecution is newTestRuntime with control over the seeded
// nebula's [execution] config blob, so a test can exercise per-run overrides
// such as max_review_cycles.
func newTestRuntimeWithExecution(t *testing.T, loader Loader, inv agent.Invoker, executionTOML string) (*Runtime, string) {
	t.Helper()
	dir := t.TempDir()
	fab, err := fabric.NewSQLiteFabric(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("fabric: %v", err)
	}
	t.Cleanup(func() { _ = fab.DB().Close() })
	blobs, err := blobstore.New(filepath.Join(dir, "blobs"), fab.DB())
	if err != nil {
		t.Fatalf("blobs: %v", err)
	}
	nebStore := fabric.NewNebulaStore(fab.DB(), blobs)
	nebID, err := nebStore.Insert(context.Background(), fabric.NebulaRow{
		Name: "demo", Status: "running", ContextTOML: "do the thing", ExecutionTOML: executionTOML,
	})
	if err != nil {
		t.Fatalf("seed nebula: %v", err)
	}
	rt := New(RuntimeOpts{
		RunStore: fabric.NewConstellationRunStore(fab.DB()),
		NebStore: nebStore,
		Loader:   loader,
		Invoker:  inv,
		RepoPath: dir,
	})
	return rt, nebID
}

// builtinNode is a small constructor for a builtin node with no inputs.
func builtinNode(id, op string) artifacts.ConstellationNode {
	return artifacts.ConstellationNode{ID: id, Type: artifacts.NodeBuiltin, Op: op}
}

func TestRuntimeFireAndStepBuiltins(t *testing.T) {
	ctx := context.Background()
	con := &artifacts.Constellation{
		Name:  "seed-flow",
		Nodes: []artifacts.ConstellationNode{builtinNode("render", "render_seed_prompt"), builtinNode("notify", "notify_human")},
		Edges: []artifacts.ConstellationEdge{
			{From: "render", To: "notify"},
			{From: "notify", To: artifacts.TermAwaitingHuman},
		},
	}
	loader := &fakeLoader{cons: map[string]*artifacts.Constellation{"seed-flow": con}}
	rt, nebID := newTestRuntime(t, loader, nil)

	runID, err := rt.Fire(ctx, "seed-flow", nebID, "", 0)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}

	// Step 1: render -> notify (still running).
	state, err := rt.Step(ctx, runID)
	if err != nil {
		t.Fatalf("Step 1: %v", err)
	}
	if state != StateRunning {
		t.Fatalf("after step 1 state = %q, want running", state)
	}
	run, _ := rt.runStore.GetRun(ctx, runID)
	if run.CurrentNode != "notify" {
		t.Fatalf("current node = %q, want notify", run.CurrentNode)
	}
	st, _ := UnmarshalState(run.DAGStateTOML)
	if _, ok := st.Nodes["render"]["prompt"]; !ok {
		t.Errorf("render node output not persisted: %+v", st.Nodes)
	}

	// Step 2: notify -> _awaiting_human (terminal).
	state, err = rt.Step(ctx, runID)
	if err != nil {
		t.Fatalf("Step 2: %v", err)
	}
	if state != StateAwaitingHuman {
		t.Fatalf("after step 2 state = %q, want awaiting_human", state)
	}

	// Stepping a terminal run errors.
	if _, err := rt.Step(ctx, runID); err != ErrTerminal {
		t.Fatalf("step on terminal run: err = %v, want ErrTerminal", err)
	}
}

func TestRuntimeStarNode(t *testing.T) {
	ctx := context.Background()
	con := &artifacts.Constellation{
		Name:  "code",
		Nodes: []artifacts.ConstellationNode{{ID: "coder", Type: artifacts.NodeStar, Star: "coder"}},
		Edges: []artifacts.ConstellationEdge{{From: "coder", To: artifacts.TermDone}},
	}
	loader := &fakeLoader{
		cons:  map[string]*artifacts.Constellation{"code": con},
		stars: map[string]*artifacts.Star{"coder": {Name: "coder", Model: "sonnet", Prompt: "be a coder"}},
	}
	inv := &fakeInvoker{result: agent.InvocationResult{ResultText: "did it", CostUSD: 0.75}}
	rt, nebID := newTestRuntime(t, loader, inv)

	runID, err := rt.Fire(ctx, "code", nebID, "", 0)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	state, err := rt.Step(ctx, runID)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if state != StateDone {
		t.Fatalf("state = %q, want done", state)
	}
	if inv.gotAgent.SystemPrompt != "be a coder" || inv.gotAgent.Model != "sonnet" {
		t.Errorf("star defaults not passed to invoker: %+v", inv.gotAgent)
	}
	run, _ := rt.runStore.GetRun(ctx, runID)
	st, _ := UnmarshalState(run.DAGStateTOML)
	if st.Nodes["coder"]["result"] != "did it" {
		t.Errorf("star result not recorded: %+v", st.Nodes["coder"])
	}
	if st.Meta.TotalCostUSD != 0.75 {
		t.Errorf("cost not accumulated: %v", st.Meta.TotalCostUSD)
	}
}

func TestRuntimeResume(t *testing.T) {
	ctx := context.Background()
	con := &artifacts.Constellation{
		Name:  "flow",
		Nodes: []artifacts.ConstellationNode{builtinNode("render", "render_seed_prompt"), builtinNode("notify", "notify_human")},
		Edges: []artifacts.ConstellationEdge{{From: "render", To: "notify"}, {From: "notify", To: artifacts.TermDone}},
	}
	loader := &fakeLoader{cons: map[string]*artifacts.Constellation{"flow": con}}
	rt, nebID := newTestRuntime(t, loader, nil)

	runID, _ := rt.Fire(ctx, "flow", nebID, "", 0)
	if _, err := rt.Step(ctx, runID); err != nil { // render -> notify
		t.Fatalf("Step: %v", err)
	}
	// Simulate a fresh process picking the run back up.
	if err := rt.Resume(ctx, runID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	run, _ := rt.runStore.GetRun(ctx, runID)
	if run.CurrentNode != "notify" {
		t.Fatalf("resumed at %q, want notify", run.CurrentNode)
	}
	state, err := rt.Step(ctx, runID)
	if err != nil {
		t.Fatalf("Step after resume: %v", err)
	}
	if state != StateDone {
		t.Fatalf("state = %q, want done", state)
	}
}

func TestNextTargetConditional(t *testing.T) {
	con := &artifacts.Constellation{
		Edges: []artifacts.ConstellationEdge{
			{From: "review", To: "_done", When: mustExpr(t, "nodes.review.approved")},
			{From: "review", To: "coder", When: mustExpr(t, "!nodes.review.approved")},
		},
	}
	t.Run("approved follows first edge", func(t *testing.T) {
		st := artifacts.State{"nodes": map[string]any{"review": map[string]any{"approved": true}}}
		got, err := nextTarget(con, "review", st)
		if err != nil || got != "_done" {
			t.Fatalf("got %q, %v; want _done", got, err)
		}
	})
	t.Run("rejected loops back", func(t *testing.T) {
		st := artifacts.State{"nodes": map[string]any{"review": map[string]any{"approved": false}}}
		got, err := nextTarget(con, "review", st)
		if err != nil || got != "coder" {
			t.Fatalf("got %q, %v; want coder", got, err)
		}
	})
	t.Run("no outgoing edge terminates done", func(t *testing.T) {
		got, err := nextTarget(con, "orphan", artifacts.State{})
		if err != nil || got != artifacts.TermDone {
			t.Fatalf("got %q, %v; want _done", got, err)
		}
	})
}

func TestUnsupportedNodeTypeFailsRun(t *testing.T) {
	ctx := context.Background()
	con := &artifacts.Constellation{
		Name:  "sub",
		Nodes: []artifacts.ConstellationNode{{ID: "child", Type: artifacts.NodeConstellation, Ref: "other"}},
		Edges: []artifacts.ConstellationEdge{{From: "child", To: artifacts.TermDone}},
	}
	loader := &fakeLoader{cons: map[string]*artifacts.Constellation{"sub": con}}
	rt, nebID := newTestRuntime(t, loader, nil)
	runID, _ := rt.Fire(ctx, "sub", nebID, "", 0)
	state, err := rt.Step(ctx, runID)
	if state != StateFailed {
		t.Fatalf("state = %q, want failed", state)
	}
	if err == nil {
		t.Fatalf("expected an error describing the unsupported node type")
	}
}

// fakeCommitter records the CommitOpts it was handed so a test can assert the
// runtime threaded the repo's [pre_commit] config, and can be made to fail to
// simulate a pre-commit failure blocking the commit.
type fakeCommitter struct {
	gotOpts gitops.CommitOpts
	sha     string
	err     error
	calls   int
	diff    string // returned by Diff to drive in-flight marking
	diffErr error
}

func (f *fakeCommitter) Commit(_ context.Context, _ string, opts gitops.CommitOpts) (string, error) {
	f.calls++
	f.gotOpts = opts
	return f.sha, f.err
}

func (f *fakeCommitter) Diff(_ context.Context, _, _ string) (string, error) {
	return f.diff, f.diffErr
}

// newRuntimeWithCommitter builds a runtime wired to a committer and pre-commit
// config, with a single-node "commit" constellation.
func newRuntimeWithCommitter(t *testing.T, c Committer, pc gitops.PreCommitConfig) (*Runtime, string) {
	t.Helper()
	con := &artifacts.Constellation{
		Name:  "ship",
		Nodes: []artifacts.ConstellationNode{builtinNode("commit", "commit")},
		Edges: []artifacts.ConstellationEdge{{From: "commit", To: artifacts.TermDone}},
	}
	loader := &fakeLoader{cons: map[string]*artifacts.Constellation{"ship": con}}
	rt, nebID := newTestRuntime(t, loader, nil)
	rt.committer = c
	rt.preCommit = pc
	return rt, nebID
}

func TestCommitOperatorThreadsPreCommit(t *testing.T) {
	ctx := context.Background()
	pc := gitops.PreCommitConfig{Commands: []string{"gofmt -l ."}, FailOnError: true}
	committer := &fakeCommitter{sha: "abc123"}
	rt, nebID := newRuntimeWithCommitter(t, committer, pc)

	runID, err := rt.Fire(ctx, "ship", nebID, "", 0)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	state, err := rt.Step(ctx, runID)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if state != StateDone {
		t.Fatalf("state = %q, want done", state)
	}
	if committer.calls != 1 {
		t.Fatalf("committer called %d times, want 1", committer.calls)
	}
	// The runtime must thread the repo's pre-commit config into the commit;
	// the operator never sets it.
	if len(committer.gotOpts.PreCommit.Commands) != 1 || !committer.gotOpts.PreCommit.FailOnError {
		t.Errorf("pre-commit not threaded into commit: %+v", committer.gotOpts.PreCommit)
	}
	run, _ := rt.runStore.GetRun(ctx, runID)
	st, _ := UnmarshalState(run.DAGStateTOML)
	if st.Nodes["commit"]["sha"] != "abc123" {
		t.Errorf("commit sha not recorded: %+v", st.Nodes["commit"])
	}
}

func TestCommitFailureBlocksRun(t *testing.T) {
	ctx := context.Background()
	pc := gitops.PreCommitConfig{Commands: []string{"false"}, FailOnError: true}
	committer := &fakeCommitter{err: errors.New("pre-commit hook \"false\" failed")}
	rt, nebID := newRuntimeWithCommitter(t, committer, pc)

	runID, _ := rt.Fire(ctx, "ship", nebID, "", 0)
	state, err := rt.Step(ctx, runID)
	if state != StateFailed {
		t.Fatalf("state = %q, want failed", state)
	}
	if err == nil {
		t.Fatal("expected the pre-commit failure to surface as a step error")
	}
}

func TestCommitNothingToCommitIsNotAFailure(t *testing.T) {
	ctx := context.Background()
	committer := &fakeCommitter{err: gitops.ErrNothingToCommit}
	rt, nebID := newRuntimeWithCommitter(t, committer, gitops.PreCommitConfig{})

	runID, _ := rt.Fire(ctx, "ship", nebID, "", 0)
	state, err := rt.Step(ctx, runID)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if state != StateDone {
		t.Fatalf("state = %q, want done", state)
	}
	run, _ := rt.runStore.GetRun(ctx, runID)
	st, _ := UnmarshalState(run.DAGStateTOML)
	if st.Nodes["commit"]["committed"] != false {
		t.Errorf("expected committed=false for empty index, got %+v", st.Nodes["commit"])
	}
}

// verify gitops.CommitOpts is the shape commitWork uses (compile-time guard
// that the Committer seam stays aligned with gitops).
var _ Committer = (*gitops.Client)(nil)
