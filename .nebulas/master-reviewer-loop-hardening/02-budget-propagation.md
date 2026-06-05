+++
id = "budget-propagation"
title = "First-class budget on constellation_runs: starts at the nebula's max_budget_usd, decrements per star_invocation cost, over-budget short-circuits the run"
type = "task"
priority = 2
depends_on = ["extract-master-reviewer-star", "cycle-limit-in-constellation"]
scope = [
    "internal/fabric/migrations/**",
    "internal/runtime/budget.go",
    "internal/runtime/budget_test.go",
    "internal/runtime/engine.go",
    "internal/runtime/operators/fail_run.go",
]
+++

## Problem

Budgets exist on the nebula manifest (`[execution].max_budget_usd`) but the runtime ignores them once a run starts. Each `star_invocation` already records `input_tokens`, `output_tokens`, and `cost_usd` (Phase 5), so the bookkeeping is there — what's missing is enforcement.

Enforcement needs three things:
1. A `budget_remaining_usd` column on `constellation_runs`, initialized at Fire time
2. A pre-step check: refuse to start the next star invocation if `budget_remaining_usd <= 0`
3. A post-step decrement: subtract the actual cost after each invocation completes

When budget hits zero mid-run, the engine routes to the same `fail_run` operator as the cycle-limit case, with `reason = "budget exhausted"`. No PR is opened.

## Solution

### Schema

`internal/fabric/migrations/NNN_budget.sql`:

```sql
ALTER TABLE constellation_runs ADD COLUMN budget_usd_initial   REAL;
ALTER TABLE constellation_runs ADD COLUMN budget_usd_remaining REAL;
ALTER TABLE constellation_runs ADD COLUMN budget_exhausted_at  INTEGER;

CREATE INDEX constellation_runs_budget_low
    ON constellation_runs (budget_usd_remaining)
    WHERE state = 'running' AND budget_usd_remaining < 1.0;
```

The index supports the TUI "running low on budget" alert (Phase 8 already accommodates this with a status field on `RunCard`).

### Budget tracker

`internal/runtime/budget.go`:

```go
type Budget struct {
    db *sql.DB
}

// Initialize sets the starting budget for a run.
// Precedence: per-run override > nebula manifest > global config default.
func (b *Budget) Initialize(ctx context.Context, runID string, amount float64) error

// CheckBefore returns ErrBudgetExhausted if a step cannot start.
// Called by the engine before each star_invocation.
func (b *Budget) CheckBefore(ctx context.Context, runID string) error

// RecordCost is called after a star_invocation completes. It atomically
// decrements budget_usd_remaining by the actual cost.
// If the result crosses zero, budget_exhausted_at is set to now.
func (b *Budget) RecordCost(ctx context.Context, runID string, cost float64) error

var ErrBudgetExhausted = errors.New("constellation run budget exhausted")
```

The decrement happens inside the same transaction that writes the `star_invocation` row, so a crash mid-write doesn't double-charge or skip.

### Engine integration

`internal/runtime/engine.go` `Step`:

```go
func (e *Engine) Step(ctx context.Context, runID string) error {
    if err := e.budget.CheckBefore(ctx, runID); err != nil {
        if errors.Is(err, ErrBudgetExhausted) {
            return e.failRun(ctx, runID, "budget exhausted", e.budgetDetail(runID))
        }
        return err
    }

    inv, err := e.startInvocation(ctx, runID) // writes star_invocation row
    if err != nil { return err }

    result, err := e.executeInvocation(ctx, inv)
    if err != nil { return err }

    if err := e.budget.RecordCost(ctx, runID, result.CostUSD); err != nil {
        return err
    }

    return e.advance(ctx, runID, inv, result)
}
```

### Precedence (Fire time)

Final initial budget is computed at `Fire`:

1. **Explicit override** passed to `runtime.Fire(ctx, name, nebulaID, opts.Budget)` (CLI flag, API caller)
2. **Nebula manifest** `[execution].max_budget_usd` (the standard path)
3. **Global default** from `.quasar.yaml` `defaults.max_budget_usd` (operator-level cap)
4. If none are set, the run starts with no budget cap (`budget_usd_initial = NULL`) and `CheckBefore` is a no-op

This honors the user's "no default nebula budget, but limit to 3 master review cycles before a PR is opened" instruction: leaving budget unset is valid; cycle limit still enforces.

### Failure detail

When budget exhausts, `failRun` writes a structured failure with breakdown:

```json
{
  "reason": "budget exhausted",
  "initial_usd": 30.0,
  "spent_usd": 30.07,
  "exhausted_at_node": "coder-reviewer.coder",
  "top_costs": [
    {"node": "coder-reviewer.coder", "invocations": 6, "cost_usd": 18.40},
    {"node": "master-review", "invocations": 3, "cost_usd": 9.20}
  ]
}
```

The TUI fleet view (Phase 8) shows this on the Recent lane so the operator can see where the money went.

### Tests

- `internal/runtime/budget_test.go` — table tests for: initialize sets columns; CheckBefore returns nil when remaining > 0; CheckBefore returns ErrBudgetExhausted when remaining ≤ 0; RecordCost decrements; concurrent RecordCost serializes via SQL constraints
- Engine integration test: a run with budget=1.0 and per-step cost=0.5 completes 2 steps then fails on the 3rd CheckBefore
- Precedence test: explicit > manifest > global > none
- "no cap" test: budget_usd_initial=NULL → CheckBefore always nil, RecordCost still records spend for telemetry

## Files

- `internal/runtime/budget.go` (new)
- `internal/runtime/budget_test.go` (new)
- `internal/fabric/migrations/NNN_budget.sql` (new)
- `internal/runtime/engine.go` (modify) — wire CheckBefore/RecordCost into Step
- `internal/runtime/operators/fail_run.go` (modify) — accept structured detail map
- `cmd/run.go` (modify) — add `--budget-usd` flag passthrough

## Acceptance Criteria

- [ ] `Budget.Initialize` writes both `budget_usd_initial` and `budget_usd_remaining`
- [ ] `Budget.CheckBefore` returns `ErrBudgetExhausted` when remaining ≤ 0 AND initial is non-NULL
- [ ] `Budget.CheckBefore` returns nil when `budget_usd_initial IS NULL` (no-cap mode)
- [ ] `Budget.RecordCost` decrements `budget_usd_remaining` in the same SQL transaction that inserts the star_invocation row
- [ ] When budget exhausts mid-run, the engine routes through `fail_run` with `reason="budget exhausted"` and a top-costs breakdown
- [ ] `runtime.Fire` resolves budget precedence: explicit override > nebula manifest > global default > none
- [ ] `--budget-usd` CLI flag overrides everything
- [ ] TUI Recent lane displays the structured failure detail on budget-exhausted runs
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
