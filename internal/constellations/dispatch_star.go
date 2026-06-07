package constellations

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/telemetry"
)

// dispatchStar resolves the star, invokes the LLM with the node's rendered
// inputs, accumulates cost, and records a star_invocation row.
//
// SAFETY INVARIANT — stars must never be granted git-write tools. A star edits
// the worktree; the *commit* happens only in the `commit` builtin node, which
// is the sole place the runtime threads the repo's [pre_commit] config into
// gitops.Commit. If a star's allowed-tools (star.Tools.Allowed, passed below)
// included direct git access, the LLM could commit inside the worktree itself,
// bypassing both the internal/gitops perimeter and the pre-commit gate. This
// runtime does not yet enforce the invariant (a loader-side rejection of
// git-write tools lands when the star tool model firms up); until then it is an
// authoring rule. See docs/safety.md ("Stars and git writes").
func (r *Runtime) dispatchStar(ctx context.Context, run *fabric.RunRow, st *State, node *artifacts.ConstellationNode) (map[string]any, error) {
	if r.invoker == nil {
		return nil, fmt.Errorf("constellations: star node %q requires an Invoker", node.ID)
	}
	// Pre-step budget gate: refuse to start this invocation if the run's cap is
	// spent. The sentinel propagates up to Step, which routes to failBudget.
	if err := r.budget.CheckBefore(ctx, run.ID); err != nil {
		return nil, err
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
		// A star's prompt is a fixed prefix reused across every firing of the
		// same node, so a byte-stable system prompt is always desirable here.
		CacheOptimization: true,
		// Honor the star's [context_budget] block: the invoker uses it to cap
		// the result and (when enabled) enforce per-tool budgets, so changing
		// tool_result_max_bytes in a star file has a real runtime effect.
		ContextBudget: contextBudget(star.ContextBudget, agent.RoleCoder),
	}, userPrompt(args, st), r.repoPath)
	if err != nil {
		r.recordInvocation(ctx, run, node, star.Name, "failed", 0, started)
		return nil, fmt.Errorf("constellations: invoke star %q: %w", star.Name, err)
	}

	st.Meta.TotalCostUSD += res.CostUSD
	// Charge the run's budget and persist the invocation trace atomically via
	// RecordCost: a crash between the two writes can neither double-charge nor
	// skip the cost. Unlike the failure-path trace, the charge is correctness-
	// relevant, so a failure to record it fails the step rather than being
	// logged and ignored.
	if _, err := r.budget.RecordCost(ctx, r.invocationRow(run, node, star.Name, "done", res.CostUSD, started)); err != nil {
		return nil, fmt.Errorf("constellations: record star invocation (run %s): %w", run.ID, err)
	}
	r.recordCacheMetric(ctx, run, node, st, &res)
	return map[string]any{
		"result":     res.ResultText,
		"cost_usd":   res.CostUSD,
		"session_id": res.SessionID,
	}, nil
}

// contextBudget converts a star's parsed [context_budget] block into the
// agent budget the invoker consumes. A zero value in the star config means
// "unset", so each unset numeric field falls back to the per-role default
// (BudgetForRole). Boolean fields are taken verbatim from the star config.
func contextBudget(sb artifacts.StarContextBudget, role agent.Role) *agent.ContextBudget {
	b := agent.BudgetForRole(role)
	if sb.MaxReadsBeforeEdit > 0 {
		b.MaxReadsBeforeEdit = sb.MaxReadsBeforeEdit
	}
	if sb.MaxGrepsBeforeEdit > 0 {
		b.MaxGrepsBeforeEdit = sb.MaxGrepsBeforeEdit
	}
	if sb.MaxTotalReads > 0 {
		b.MaxTotalReads = sb.MaxTotalReads
	}
	if sb.ToolResultMaxBytes > 0 {
		b.ToolResultMaxBytes = sb.ToolResultMaxBytes
	}
	b.IncludeSiblingPhases = sb.IncludeSiblingPhases
	b.EnableToolHook = sb.EnableToolHook
	return &b
}

// recordCacheMetric persists a star invocation's prompt-cache token counts to
// the JSONL log when a store is configured. Recording is best-effort: a write
// failure is logged but never fails the step, since telemetry is a read-only
// side channel that must not block the constellation walk.
func (r *Runtime) recordCacheMetric(ctx context.Context, run *fabric.RunRow, node *artifacts.ConstellationNode, st *State, res *agent.InvocationResult) {
	if r.cacheMetrics == nil {
		return
	}
	err := r.cacheMetrics.Record(ctx, telemetry.CacheMetric{
		NebulaID:    run.NebulaID,
		PhaseID:     node.ID,
		CycleN:      st.Cycle,
		InputTokens: res.InputTokens,
		CacheCreate: res.CacheCreationTokens,
		CacheRead:   res.CacheReadTokens,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[cache] failed to record star metric: %v\n", err)
	}
}
