+++
id = "oracle-agent"
title = "Oracle agent for monitoring outcomes and dynamic replanning"
type = "feature"
priority = 2
depends_on = ["dag-scheduler", "cross-nebula-fabric"]
scope = ["internal/constellation/oracle.go", "internal/constellation/oracle_test.go"]
+++

## Problem

When a nebula fails in a constellation, the current behavior is binary: abort everything or skip dependents. Neither is satisfactory. A failed nebula might have partially succeeded (some phases done, others failed). An intelligent agent could analyze the failure, determine whether the constellation can still achieve its goals, and propose recovery strategies: retry with adjusted parameters, decompose the failed nebula into smaller pieces, insert a remediation nebula, or skip if the failure is non-critical.

This is analogous to the existing healing system (`internal/nebula/worker.go` healing logic) but at the constellation level: instead of healing individual phases, the oracle heals nebulas within the constellation DAG.

## Solution

### Oracle agent

```go
// internal/constellation/oracle.go

package constellation

import (
    "context"
    "fmt"

    "github.com/aaronsalm/quasar/internal/agent"
)

// OracleDecision represents a recovery strategy proposed by the oracle.
type OracleDecision struct {
    // NebulaName is the failed nebula this decision applies to.
    NebulaName string

    // Strategy is the proposed recovery action.
    Strategy OracleStrategy

    // Reason explains why this strategy was chosen.
    Reason string

    // Params carries strategy-specific data.
    Params OracleParams
}

// OracleStrategy enumerates the possible recovery actions.
type OracleStrategy string

const (
    // StrategyRetry re-runs the nebula with the same or adjusted config.
    StrategyRetry OracleStrategy = "retry"

    // StrategySkip marks the nebula as skipped and continues.
    StrategySkip OracleStrategy = "skip"

    // StrategyDecompose breaks the failed nebula into smaller nebulas
    // and inserts them into the DAG.
    StrategyDecompose OracleStrategy = "decompose"

    // StrategyRemediate inserts a new remediation nebula before retrying.
    StrategyRemediate OracleStrategy = "remediate"

    // StrategyAbort stops the entire constellation.
    StrategyAbort OracleStrategy = "abort"
)

// OracleParams carries strategy-specific configuration.
type OracleParams struct {
    // For retry: adjusted max_workers, budget, cycles.
    MaxWorkers      int     `json:"max_workers,omitempty"`
    MaxBudgetUSD    float64 `json:"max_budget_usd,omitempty"`
    MaxReviewCycles int     `json:"max_review_cycles,omitempty"`

    // For decompose: proposed sub-nebula specs.
    SubNebulas []NebulaRef `json:"sub_nebulas,omitempty"`

    // For remediate: the remediation nebula to insert.
    Remediation *NebulaRef `json:"remediation,omitempty"`
}
```

### Oracle struct

```go
// Oracle monitors constellation execution and proposes recovery
// strategies for failed nebulas. It uses an AI agent (via agent.Invoker)
// to analyze failures and determine the best course of action.
type Oracle struct {
    invoker agent.Invoker
    model   string

    // MaxRetries limits how many times the oracle will retry a single
    // nebula before giving up.
    MaxRetries int

    // retries tracks per-nebula retry counts.
    retries map[string]int
}

// NewOracle creates an oracle with the given agent invoker.
func NewOracle(invoker agent.Invoker, model string, maxRetries int) *Oracle {
    return &Oracle{
        invoker:    invoker,
        model:      model,
        MaxRetries: maxRetries,
        retries:    make(map[string]int),
    }
}
```

### Analyze failure

```go
// Analyze examines a failed nebula outcome and proposes a recovery
// strategy. It considers: the failure details, how many phases
// succeeded vs failed, the nebula's position in the DAG, remaining
// budget, and previous retry attempts.
func (o *Oracle) Analyze(ctx context.Context, c *Constellation, outcome NebulaOutcome) (*OracleDecision, error) {
    // Check retry budget.
    if o.retries[outcome.Name] >= o.MaxRetries {
        return &OracleDecision{
            NebulaName: outcome.Name,
            Strategy:   StrategySkip,
            Reason:     fmt.Sprintf("max retries (%d) exhausted", o.MaxRetries),
        }, nil
    }

    // Build analysis prompt with constellation context.
    prompt := o.buildAnalysisPrompt(c, outcome)

    // Invoke the AI agent for analysis.
    resp, err := o.invoker.Invoke(ctx, prompt, agent.InvokeOptions{
        Model: o.model,
    })
    if err != nil {
        // If the oracle itself fails, default to skip.
        return &OracleDecision{
            NebulaName: outcome.Name,
            Strategy:   StrategySkip,
            Reason:     fmt.Sprintf("oracle analysis failed: %v", err),
        }, nil
    }

    decision := o.parseDecision(outcome.Name, resp)
    return decision, nil
}
```

### Prompt construction

```go
func (o *Oracle) buildAnalysisPrompt(c *Constellation, outcome NebulaOutcome) string {
    var b strings.Builder
    b.WriteString("You are the Oracle agent for a Quasar constellation.\n\n")
    b.WriteString("A nebula has failed. Analyze the failure and recommend a recovery strategy.\n\n")

    b.WriteString(fmt.Sprintf("## Constellation: %s\n", c.Name))
    b.WriteString(fmt.Sprintf("## Failed nebula: %s\n", outcome.Name))
    b.WriteString(fmt.Sprintf("## Attempt: %d / %d\n\n", o.retries[outcome.Name]+1, o.MaxRetries))

    if outcome.Result != nil {
        b.WriteString("## Execution result\n")
        if outcome.Result.Err != nil {
            b.WriteString(fmt.Sprintf("Error: %v\n", outcome.Result.Err))
        }
        for _, wr := range outcome.Result.WorkerResults {
            status := "ok"
            if wr.Err != nil {
                status = fmt.Sprintf("FAILED: %v", wr.Err)
            }
            b.WriteString(fmt.Sprintf("- Phase %s: %s\n", wr.PhaseID, status))
        }
    }

    b.WriteString("\n## Strategies available\n")
    b.WriteString("- retry: Re-run with adjusted parameters\n")
    b.WriteString("- skip: Skip this nebula and continue\n")
    b.WriteString("- decompose: Break into smaller nebulas\n")
    b.WriteString("- remediate: Insert a fix-up nebula first\n")
    b.WriteString("- abort: Stop the entire constellation\n")
    b.WriteString("\nRespond with a JSON object: {\"strategy\": \"...\", \"reason\": \"...\", \"params\": {...}}\n")

    return b.String()
}
```

### Integration with scheduler

The scheduler calls the oracle when a nebula fails (if `FailureStrategy` is `oracle`):

```go
// In scheduler.go, within executeNebula or the wave runner:
if outcome.Status == NebulaStatusFailed && s.oracle != nil {
    decision, err := s.oracle.Analyze(ctx, s.constellation, *outcome)
    if err == nil {
        outcome = s.applyDecision(ctx, ref, outcome, decision)
    }
}
```

The `applyDecision` method handles each strategy:
- `retry`: increment retry counter, re-run `executeNebula`
- `skip`: mark as skipped, continue
- `decompose`: create sub-nebulas, insert into DAG, re-compute waves
- `remediate`: insert remediation nebula before retrying
- `abort`: return error to halt constellation

## Files

- `internal/constellation/oracle.go` — `Oracle`, `OracleDecision`, `OracleStrategy`, `OracleParams`, `NewOracle`, `Analyze`, `buildAnalysisPrompt`, `parseDecision`
- `internal/constellation/oracle_test.go` — tests for: retry exhaustion returns skip, mock invoker returns valid JSON decision, parse handles each strategy, invalid JSON falls back to skip
- `internal/constellation/scheduler.go` — add `oracle` field to `Scheduler`, call `Analyze` on failure, `applyDecision` handler

## Acceptance Criteria

- [ ] `Oracle.Analyze` produces a decision for a failed nebula
- [ ] Retry strategy increments counter and re-runs the nebula
- [ ] Skip strategy marks the nebula as skipped without retrying
- [ ] Max retries exhaustion automatically produces a skip decision
- [ ] Oracle failure (invoker error) safely defaults to skip (not crash)
- [ ] Analysis prompt includes constellation context, failure details, and phase-level results
- [ ] Decision JSON is parsed into typed `OracleDecision`
- [ ] Invalid JSON response from AI gracefully degrades to skip
- [ ] `go test ./internal/constellation/...` passes
- [ ] `go vet ./internal/constellation/...` passes
