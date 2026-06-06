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
// the budget analogue of the cycle-cap give-up path: a defined terminal failure
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
