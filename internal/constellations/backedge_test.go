package constellations

import (
	"context"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// mustTemplate compiles a node-input template (${...} interpolation), failing
// the test on a parse error.
func mustTemplate(t *testing.T, src string) artifacts.Expression {
	t.Helper()
	e, err := artifacts.ParseTemplate(src)
	if err != nil {
		t.Fatalf("parse template %q: %v", src, err)
	}
	return e
}

func TestIsBackEdge(t *testing.T) {
	t.Parallel()
	con := &artifacts.Constellation{
		Nodes: []artifacts.ConstellationNode{
			{ID: "review"}, {ID: "decide"}, {ID: "give-up"},
		},
	}
	cases := []struct {
		name     string
		from, to string
		want     bool
	}{
		{"forward edge is not a back-edge", "review", "decide", false},
		{"return to earlier node is a back-edge", "decide", "review", true},
		{"self loop is a back-edge", "decide", "decide", true},
		{"terminal target is never a back-edge", "decide", artifacts.TermFailed, false},
		{"done target is never a back-edge", "decide", artifacts.TermDone, false},
		{"unknown source is not a back-edge", "ghost", "review", false},
		{"unknown target is not a back-edge", "review", "ghost", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isBackEdge(con, tc.from, tc.to); got != tc.want {
				t.Errorf("isBackEdge(%q, %q) = %v, want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

// requestChangesJSON is a master-review-decision-v1 payload whose verdict maps
// to the "fix" routing decision, so a star returning it keeps the master-review
// loop cycling until the cap is exhausted.
const requestChangesJSON = `{"verdict":"request_changes","score":40,` +
	`"reasons":[{"category":"correctness","detail":"still broken"}],` +
	`"suggestions":["try harder"]}`

// loopingMasterReview builds an in-memory constellation shaped like the
// master-review fix loop with a real back-edge (decide -> review) so the runtime
// can exercise cycle counting end-to-end. The embedded master-review.toml routes
// a within-cap fix to _done as a placeholder until the inner coder-reviewer
// constellation node lands; this fixture stands in for that wired loop.
func loopingMasterReview(t *testing.T, maxCycles int) *artifacts.Constellation {
	t.Helper()
	return &artifacts.Constellation{
		Name: "looping-master-review",
		Meta: map[string]any{"max_cycles": maxCycles},
		Nodes: []artifacts.ConstellationNode{
			{ID: "review", Type: artifacts.NodeStar, Star: "reviewer"},
			{ID: "decide", Type: artifacts.NodeBuiltin, Op: "master_review_decision",
				Inputs: map[string]artifacts.Expression{"output": mustTemplate(t, "${nodes.review.result}")}},
			{ID: "give-up", Type: artifacts.NodeBuiltin, Op: "fail_run",
				Inputs: map[string]artifacts.Expression{
					"reason": mustTemplate(t, "max master-review cycles exhausted"),
					"detail": mustTemplate(t, "${nodes.decide.feedback}"),
				}},
		},
		Edges: []artifacts.ConstellationEdge{
			{From: "review", To: "decide"},
			{From: "decide", To: artifacts.TermDone, When: mustExpr(t, "nodes.decide.decision == 'ship'")},
			{From: "decide", To: "review", When: mustExpr(t, "nodes.decide.decision == 'fix' && cycle < meta.max_cycles")},
			{From: "decide", To: "give-up", When: mustExpr(t, "nodes.decide.decision == 'fix' && cycle >= meta.max_cycles")},
			{From: "give-up", To: artifacts.TermFailed},
		},
	}
}

// TestBackEdgeIncrementsCycleUntilGiveUp drives a looping master-review-shaped
// constellation whose reviewer always requests changes. Each decide->review
// back-edge must bump the run's cycle; once the cap is hit the run must route to
// give-up and terminate failed with the structured reason, never opening a PR.
func TestBackEdgeIncrementsCycleUntilGiveUp(t *testing.T) {
	ctx := context.Background()
	con := loopingMasterReview(t, 2)
	loader := &fakeLoader{
		cons:  map[string]*artifacts.Constellation{"looping-master-review": con},
		stars: map[string]*artifacts.Star{"reviewer": {Name: "reviewer", Model: "sonnet", Prompt: "review"}},
	}
	inv := &fakeInvoker{result: agent.InvocationResult{ResultText: requestChangesJSON}}
	rt, nebID := newTestRuntime(t, loader, inv)

	runID, err := rt.Fire(ctx, "looping-master-review", nebID, "")
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	driveToTerminal(ctx, t, rt, runID)

	run, err := rt.runStore.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != StateFailed {
		t.Fatalf("final state = %q, want failed", run.State)
	}
	if run.Cycle != 2 {
		t.Errorf("run cycle = %d, want 2 (one per back-edge, capped)", run.Cycle)
	}
	st, err := UnmarshalState(run.DAGStateTOML)
	if err != nil {
		t.Fatalf("UnmarshalState: %v", err)
	}
	if st.Cycle != 2 {
		t.Errorf("state cycle = %d, want 2", st.Cycle)
	}
	if got := st.Nodes["give-up"]["reason"]; got != "max master-review cycles exhausted" {
		t.Errorf("give-up reason = %v, want structured cycle-exhaustion reason", got)
	}
}

// TestPerRunOverrideChangesCycleCap proves the nebula's
// [execution].max_review_cycles overrides the constellation's embedded
// [meta].max_cycles at Fire time: with an override of 1, the loop gives up after
// a single back-edge rather than the constellation's default of 2.
func TestPerRunOverrideChangesCycleCap(t *testing.T) {
	ctx := context.Background()
	con := loopingMasterReview(t, 2)
	loader := &fakeLoader{
		cons:  map[string]*artifacts.Constellation{"looping-master-review": con},
		stars: map[string]*artifacts.Star{"reviewer": {Name: "reviewer", Model: "sonnet", Prompt: "review"}},
	}
	inv := &fakeInvoker{result: agent.InvocationResult{ResultText: requestChangesJSON}}
	rt, nebID := newTestRuntimeWithExecution(t, loader, inv, "max_review_cycles = 1\n")

	runID, err := rt.Fire(ctx, "looping-master-review", nebID, "")
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	st, err := UnmarshalState(mustRun(t, rt, runID).DAGStateTOML)
	if err != nil {
		t.Fatalf("UnmarshalState after Fire: %v", err)
	}
	if st.Meta.MaxCycles != 1 {
		t.Fatalf("resolved max cycles = %d, want 1 (per-run override)", st.Meta.MaxCycles)
	}

	driveToTerminal(ctx, t, rt, runID)
	run := mustRun(t, rt, runID)
	if run.State != StateFailed {
		t.Fatalf("final state = %q, want failed", run.State)
	}
	if run.Cycle != 1 {
		t.Errorf("run cycle = %d, want 1 (override cap)", run.Cycle)
	}
}

func mustRun(t *testing.T, rt *Runtime, runID string) *fabric.RunRow {
	t.Helper()
	run, err := rt.runStore.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	return run
}
