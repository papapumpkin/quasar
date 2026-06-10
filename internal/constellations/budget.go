package constellations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// ErrBudgetExhausted is returned by Budget.CheckBefore when a capped run has no
// remaining budget, so the engine must not start the next star invocation. It is
// the budget analog of the cycle-cap give-up path: a defined terminal failure
// mode, not an operational error.
var ErrBudgetExhausted = errors.New("constellations: constellation run budget exhausted")

// budgetStore is the slice of fabric.ConstellationRunStore the Budget needs,
// defined here (where consumed) per project convention. The concrete store
// satisfies it.
type budgetStore interface {
	InitializeBudget(ctx context.Context, runID string, amount float64) error
	RunBudget(ctx context.Context, runID string) (capSet bool, initial, remaining float64, err error)
	RecordInvocationCost(ctx context.Context, inv fabric.StarInvocationRow) (int64, error)
}

// Budget enforces a per-run USD cap on star invocations. It reuses the
// star_invocation cost columns rather than introducing a separate ledger:
// Initialize seeds the cap at Fire time, CheckBefore refuses to start a step
// once the cap is spent, and RecordCost decrements the remaining budget in the
// same transaction that records each invocation.
type Budget struct {
	store budgetStore
}

// NewBudget constructs a Budget over a run store.
func NewBudget(store budgetStore) *Budget {
	return &Budget{store: store}
}

// Initialize seeds a run's budget cap. A non-positive amount selects no-cap mode
// (the run's budget columns stay NULL), under which CheckBefore never trips.
func (b *Budget) Initialize(ctx context.Context, runID string, amount float64) error {
	if amount <= 0 {
		return nil
	}
	return b.store.InitializeBudget(ctx, runID, amount)
}

// CheckBefore returns ErrBudgetExhausted when a capped run has no budget left,
// and nil for an uncapped run or one still in budget. The engine calls it before
// each star invocation.
func (b *Budget) CheckBefore(ctx context.Context, runID string) error {
	capSet, _, remaining, err := b.store.RunBudget(ctx, runID)
	if err != nil {
		return err
	}
	if capSet && remaining <= 0 {
		return ErrBudgetExhausted
	}
	return nil
}

// RecordCost records a completed star invocation and, for a capped run,
// decrements the remaining budget by inv.CostUSD in the same SQL transaction.
// The invocation row is passed (rather than a bare cost) so the decrement and
// the trace insert commit atomically. Returns the invocation row ID.
func (b *Budget) RecordCost(ctx context.Context, inv fabric.StarInvocationRow) (int64, error) {
	return b.store.RecordInvocationCost(ctx, inv)
}

// invocationRow builds a star_invocation trace row for one node firing. Cycle is
// taken from the run row (authoritative for the column), and the duration is
// measured from started to now so both success- and failure-path traces stamp a
// consistent end time.
func (r *Runtime) invocationRow(run *fabric.RunRow, node *artifacts.ConstellationNode, starName, state string, costUSD float64, started time.Time) fabric.StarInvocationRow {
	ended := time.Now()
	return fabric.StarInvocationRow{
		RunID:      run.ID,
		Node:       node.ID,
		StarName:   starName,
		State:      state,
		Cycle:      run.Cycle,
		CostUSD:    costUSD,
		DurationMs: ended.Sub(started).Milliseconds(),
		StartedAt:  started.Unix(),
		EndedAt:    ended.Unix(),
	}
}

// recordInvocation persists a failure-path trace row for telemetry only — it
// does not charge the budget (a failed invocation has no billable cost). Unlike
// the success path's RecordCost, a write error here is logged and swallowed: the
// trace is best-effort and must not mask the underlying invocation failure the
// caller is already returning.
func (r *Runtime) recordInvocation(ctx context.Context, run *fabric.RunRow, node *artifacts.ConstellationNode, starName, state string, costUSD float64, started time.Time) {
	if _, err := r.runStore.InsertStarInvocation(ctx, r.invocationRow(run, node, starName, state, costUSD, started)); err != nil {
		fmt.Fprintf(os.Stderr, "constellations: record %s invocation (run %s, node %s): %v\n", state, run.ID, node.ID, err)
	}
}

// budgetDetail builds the structured breakdown attached to a budget-exhaustion
// failure: the cap, the total spent, the node the cap tripped on, and the
// per-node cost ranking (most expensive first). Store-read errors are logged and
// the partial detail is returned rather than failing the already-terminating
// run.
func (r *Runtime) budgetDetail(ctx context.Context, runID, node string) map[string]any {
	_, initial, remaining, err := r.runStore.RunBudget(ctx, runID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "constellations: read budget for detail (run %s): %v\n", runID, err)
	}
	breakdown, err := r.runStore.CostBreakdown(ctx, runID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "constellations: cost breakdown for detail (run %s): %v\n", runID, err)
	}
	top := make([]map[string]any, len(breakdown))
	for i, nc := range breakdown {
		top[i] = map[string]any{
			"node":        nc.Node,
			"invocations": nc.Invocations,
			"cost_usd":    nc.CostUSD,
		}
	}
	return map[string]any{
		"reason":            "budget exhausted",
		"exhausted_at_node": node,
		"initial_usd":       initial,
		"spent_usd":         initial - remaining,
		"top_costs":         top,
	}
}

// failBudget terminates a run that hit its budget cap. It mirrors fail, but
// records a structured budget breakdown into the _error node and returns
// ErrBudgetExhausted so callers (and tests) can distinguish a budget stop — a
// defined terminal failure mode — from an operational error.
func (r *Runtime) failBudget(ctx context.Context, run *fabric.RunRow, st *State, node *artifacts.ConstellationNode) (string, error) {
	st.RecordNode("_error", map[string]any{
		"reason": "budget exhausted",
		"detail": r.budgetDetail(ctx, run.ID, node.ID),
		"node":   node.ID,
	})
	dag, err := MarshalState(st)
	if err != nil {
		fmt.Fprintf(os.Stderr, "constellations: marshal budget-failure state (run %s): %v\n", run.ID, err)
	} else {
		run.DAGStateTOML = dag
		if err := r.runStore.SaveProgress(ctx, run); err != nil {
			fmt.Fprintf(os.Stderr, "constellations: save budget-failure state (run %s): %v\n", run.ID, err)
		}
	}
	if err := r.runStore.Complete(ctx, run.ID, StateFailed); err != nil {
		return "", err
	}
	return StateFailed, ErrBudgetExhausted
}

// resolveBudget computes a run's initial budget cap at Fire time. Precedence:
// explicit override (CLI/API) > nebula [execution].max_budget_usd > global
// default. A non-positive result means the run has no cap.
func resolveBudget(override float64, neb *fabric.Nebula, globalDefault float64) float64 {
	if override > 0 {
		return override
	}
	if neb != nil {
		if v := executionMaxBudgetUSD(neb.ExecutionTOML); v > 0 {
			return v
		}
	}
	if globalDefault > 0 {
		return globalDefault
	}
	return 0
}

// executionMaxBudgetUSD extracts max_budget_usd from a nebula's serialized
// [execution] config. A blank or unparseable blob yields 0 (no override),
// mirroring executionMaxReviewCycles.
func executionMaxBudgetUSD(executionTOML string) float64 {
	if strings.TrimSpace(executionTOML) == "" {
		return 0
	}
	var exec struct {
		MaxBudgetUSD float64 `toml:"max_budget_usd"`
	}
	if err := toml.Unmarshal([]byte(executionTOML), &exec); err != nil {
		// Non-fatal: a malformed blob falls back to the global default rather than
		// failing the fire. Surface it for diagnosis.
		fmt.Fprintf(os.Stderr, "constellations: parse nebula execution budget: %v\n", err)
		return 0
	}
	return exec.MaxBudgetUSD
}
