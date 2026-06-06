package constellations

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// approxEq reports whether two USD amounts are equal within a cent, so float
// accumulation error in budget arithmetic does not make assertions brittle.
func approxEq(a, b float64) bool { return math.Abs(a-b) < 0.005 }

// loopingStarRuntime builds a runtime over a single star node that loops back to
// itself with no edge guard, so the only thing that can terminate the run is the
// budget gate. The invoker returns a fixed per-invocation cost.
func loopingStarRuntime(t *testing.T, costUSD float64) (*Runtime, string) {
	t.Helper()
	con := &artifacts.Constellation{
		Name:  "loop",
		Nodes: []artifacts.ConstellationNode{{ID: "coder", Type: artifacts.NodeStar, Star: "coder"}},
		Edges: []artifacts.ConstellationEdge{{From: "coder", To: "coder"}},
	}
	loader := &fakeLoader{
		cons:  map[string]*artifacts.Constellation{"loop": con},
		stars: map[string]*artifacts.Star{"coder": {Name: "coder", Model: "sonnet", Prompt: "code"}},
	}
	inv := &fakeInvoker{result: agent.InvocationResult{ResultText: "ok", CostUSD: costUSD}}
	rt, nebID := newTestRuntime(t, loader, inv)
	return rt, nebID
}

func TestResolveBudget(t *testing.T) {
	t.Parallel()
	nebWith := func(execTOML string) *fabric.Nebula { return &fabric.Nebula{ExecutionTOML: execTOML} }
	manifest := nebWith("max_budget_usd = 12.5")

	tests := []struct {
		name          string
		override      float64
		neb           *fabric.Nebula
		globalDefault float64
		want          float64
	}{
		{"explicit override wins over all", 30.0, manifest, 5.0, 30.0},
		{"manifest used when no override", 0, manifest, 5.0, 12.5},
		{"global default when no override or manifest", 0, nebWith(""), 5.0, 5.0},
		{"none set yields no cap", 0, nebWith(""), 0, 0},
		{"nil nebula falls through to global", 0, nil, 7.0, 7.0},
		{"zero manifest value is not a cap", 0, nebWith("max_budget_usd = 0"), 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveBudget(tc.override, tc.neb, tc.globalDefault); !approxEq(got, tc.want) {
				t.Fatalf("resolveBudget(%v, %+v, %v) = %v, want %v", tc.override, tc.neb, tc.globalDefault, got, tc.want)
			}
		})
	}
}

func TestBudgetInitializeSeedsBothColumns(t *testing.T) {
	ctx := context.Background()
	rt, nebID := loopingStarRuntime(t, 0.5)
	runID, err := rt.Fire(ctx, "loop", nebID, "", 20.0)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	capSet, initial, remaining, err := rt.runStore.RunBudget(ctx, runID)
	if err != nil {
		t.Fatalf("RunBudget: %v", err)
	}
	if !capSet || !approxEq(initial, 20.0) || !approxEq(remaining, 20.0) {
		t.Fatalf("after Initialize: capSet=%v initial=%v remaining=%v, want true/20/20", capSet, initial, remaining)
	}
}

func TestBudgetNoCapLeavesColumnsNull(t *testing.T) {
	ctx := context.Background()
	rt, nebID := loopingStarRuntime(t, 0.5)
	// Fire with a non-positive override and no manifest/global default selects
	// no-cap mode: the budget columns stay NULL.
	runID, err := rt.Fire(ctx, "loop", nebID, "", 0)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	capSet, _, _, err := rt.runStore.RunBudget(ctx, runID)
	if err != nil {
		t.Fatalf("RunBudget: %v", err)
	}
	if capSet {
		t.Fatalf("uncapped run reports capSet=true, want false")
	}
}

func TestBudgetCheckBefore(t *testing.T) {
	ctx := context.Background()

	t.Run("capped with budget remaining returns nil", func(t *testing.T) {
		rt, nebID := loopingStarRuntime(t, 0.5)
		runID, _ := rt.Fire(ctx, "loop", nebID, "", 10.0)
		if err := rt.budget.CheckBefore(ctx, runID); err != nil {
			t.Fatalf("CheckBefore with budget left = %v, want nil", err)
		}
	})

	t.Run("capped and exhausted returns ErrBudgetExhausted", func(t *testing.T) {
		rt, nebID := loopingStarRuntime(t, 0.5)
		runID, _ := rt.Fire(ctx, "loop", nebID, "", 1.0)
		// Spend the whole cap so remaining hits zero.
		if _, err := rt.budget.RecordCost(ctx, budgetInv(runID, "coder", 1.0)); err != nil {
			t.Fatalf("RecordCost: %v", err)
		}
		if err := rt.budget.CheckBefore(ctx, runID); !errors.Is(err, ErrBudgetExhausted) {
			t.Fatalf("CheckBefore when exhausted = %v, want ErrBudgetExhausted", err)
		}
	})

	t.Run("uncapped run never trips even after spend", func(t *testing.T) {
		rt, nebID := loopingStarRuntime(t, 0.5)
		runID, _ := rt.Fire(ctx, "loop", nebID, "", 0) // no cap
		if _, err := rt.budget.RecordCost(ctx, budgetInv(runID, "coder", 99.0)); err != nil {
			t.Fatalf("RecordCost: %v", err)
		}
		if err := rt.budget.CheckBefore(ctx, runID); err != nil {
			t.Fatalf("CheckBefore on uncapped run = %v, want nil", err)
		}
	})
}

func TestBudgetRecordCostDecrements(t *testing.T) {
	ctx := context.Background()
	rt, nebID := loopingStarRuntime(t, 0.5)
	runID, _ := rt.Fire(ctx, "loop", nebID, "", 5.0)

	if _, err := rt.budget.RecordCost(ctx, budgetInv(runID, "coder", 2.0)); err != nil {
		t.Fatalf("RecordCost 1: %v", err)
	}
	_, _, remaining, _ := rt.runStore.RunBudget(ctx, runID)
	if !approxEq(remaining, 3.0) {
		t.Fatalf("after first charge remaining = %v, want 3.0", remaining)
	}

	// The charge that crosses zero stamps budget_exhausted_at and records spend.
	if _, err := rt.budget.RecordCost(ctx, budgetInv(runID, "coder", 3.5)); err != nil {
		t.Fatalf("RecordCost 2: %v", err)
	}
	_, _, remaining, _ = rt.runStore.RunBudget(ctx, runID)
	if !approxEq(remaining, -0.5) {
		t.Fatalf("after overspend remaining = %v, want -0.5", remaining)
	}

	// Both invocations are persisted as trace rows for telemetry.
	breakdown, err := rt.runStore.CostBreakdown(ctx, runID)
	if err != nil {
		t.Fatalf("CostBreakdown: %v", err)
	}
	if len(breakdown) != 1 || breakdown[0].Invocations != 2 || !approxEq(breakdown[0].CostUSD, 5.5) {
		t.Fatalf("breakdown = %+v, want one node with 2 invocations totalling 5.5", breakdown)
	}
}

func TestBudgetRecordCostUncappedRecordsTelemetry(t *testing.T) {
	ctx := context.Background()
	rt, nebID := loopingStarRuntime(t, 0.5)
	runID, _ := rt.Fire(ctx, "loop", nebID, "", 0) // no cap

	if _, err := rt.budget.RecordCost(ctx, budgetInv(runID, "coder", 1.5)); err != nil {
		t.Fatalf("RecordCost: %v", err)
	}
	// No cap, so budget stays NULL...
	if capSet, _, _, _ := rt.runStore.RunBudget(ctx, runID); capSet {
		t.Fatalf("uncapped run gained a cap after RecordCost")
	}
	// ...but the spend is still recorded for telemetry.
	breakdown, _ := rt.runStore.CostBreakdown(ctx, runID)
	if len(breakdown) != 1 || !approxEq(breakdown[0].CostUSD, 1.5) {
		t.Fatalf("breakdown = %+v, want one node totalling 1.5", breakdown)
	}
}

func TestBudgetRecordCostConcurrentSerializes(t *testing.T) {
	ctx := context.Background()
	rt, nebID := loopingStarRuntime(t, 0.5)
	runID, _ := rt.Fire(ctx, "loop", nebID, "", 100.0)

	const n = 20
	const each = 1.0
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = rt.budget.RecordCost(ctx, budgetInv(runID, "coder", each))
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent RecordCost %d: %v", i, err)
		}
	}
	// Each decrement is atomic, so none are lost: remaining = 100 - 20*1.
	_, _, remaining, _ := rt.runStore.RunBudget(ctx, runID)
	if !approxEq(remaining, 100.0-n*each) {
		t.Fatalf("remaining after %d concurrent charges = %v, want %v", n, remaining, 100.0-n*each)
	}
}

func TestBudgetDetailBreakdown(t *testing.T) {
	ctx := context.Background()
	rt, nebID := loopingStarRuntime(t, 0.5)
	runID, _ := rt.Fire(ctx, "loop", nebID, "", 1.0)
	if _, err := rt.budget.RecordCost(ctx, budgetInv(runID, "coder", 0.7)); err != nil {
		t.Fatalf("RecordCost coder: %v", err)
	}
	if _, err := rt.budget.RecordCost(ctx, budgetInv(runID, "reviewer", 0.5)); err != nil {
		t.Fatalf("RecordCost reviewer: %v", err)
	}

	detail := rt.budgetDetail(ctx, runID, "coder")
	if detail["reason"] != "budget exhausted" {
		t.Errorf("reason = %v, want \"budget exhausted\"", detail["reason"])
	}
	if detail["exhausted_at_node"] != "coder" {
		t.Errorf("exhausted_at_node = %v, want coder", detail["exhausted_at_node"])
	}
	if got, _ := detail["initial_usd"].(float64); !approxEq(got, 1.0) {
		t.Errorf("initial_usd = %v, want 1.0", detail["initial_usd"])
	}
	if got, _ := detail["spent_usd"].(float64); !approxEq(got, 1.2) {
		t.Errorf("spent_usd = %v, want 1.2", detail["spent_usd"])
	}
	top, ok := detail["top_costs"].([]map[string]any)
	if !ok || len(top) != 2 {
		t.Fatalf("top_costs = %v, want 2 entries", detail["top_costs"])
	}
	// Ordered by cost descending: coder (0.7) before reviewer (0.5).
	if top[0]["node"] != "coder" || top[0]["invocations"] != 1 {
		t.Errorf("top_costs[0] = %v, want coder with 1 invocation", top[0])
	}
	if got, _ := top[0]["cost_usd"].(float64); !approxEq(got, 0.7) {
		t.Errorf("top_costs[0].cost_usd = %v, want 0.7", top[0]["cost_usd"])
	}
}

// TestRuntimeBudgetExhaustionFailsRun is the engine integration test: a run
// capped at $1.00 with a $0.50-per-step star completes two steps, then fails on
// the third when CheckBefore finds no budget left. The run terminates failed
// with a structured budget-exhaustion reason and no further work.
func TestRuntimeBudgetExhaustionFailsRun(t *testing.T) {
	ctx := context.Background()
	rt, nebID := loopingStarRuntime(t, 0.5)
	runID, err := rt.Fire(ctx, "loop", nebID, "", 1.0)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}

	// Step 1 and 2 run the star and stay running (remaining 1.0 -> 0.5 -> 0.0).
	for i := 1; i <= 2; i++ {
		state, err := rt.Step(ctx, runID)
		if err != nil {
			t.Fatalf("Step %d: %v", i, err)
		}
		if state != StateRunning {
			t.Fatalf("after step %d state = %q, want running", i, state)
		}
	}

	// Step 3: CheckBefore finds remaining <= 0 and routes through failBudget.
	state, err := rt.Step(ctx, runID)
	if state != StateFailed {
		t.Fatalf("step 3 state = %q, want failed", state)
	}
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("step 3 err = %v, want ErrBudgetExhausted", err)
	}

	run, _ := rt.runStore.GetRun(ctx, runID)
	if run.State != StateFailed {
		t.Fatalf("persisted run state = %q, want failed", run.State)
	}
	st, _ := UnmarshalState(run.DAGStateTOML)
	errNode := st.Nodes["_error"]
	if errNode["reason"] != "budget exhausted" {
		t.Fatalf("_error.reason = %v, want \"budget exhausted\"", errNode["reason"])
	}
	if errNode["detail"] == nil {
		t.Fatal("_error.detail missing structured budget breakdown")
	}

	// Exactly two invocations were charged; the third never started.
	breakdown, _ := rt.runStore.CostBreakdown(ctx, runID)
	if len(breakdown) != 1 || breakdown[0].Invocations != 2 {
		t.Fatalf("breakdown = %+v, want one node with 2 invocations", breakdown)
	}
}

// budgetInv builds a minimal completed star_invocation row for budget tests.
func budgetInv(runID, node string, cost float64) fabric.StarInvocationRow {
	return fabric.StarInvocationRow{RunID: runID, Node: node, StarName: node, State: "done", CostUSD: cost}
}
