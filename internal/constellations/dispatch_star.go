package constellations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/claude"
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
	// Live update: the cockpit marks this run's current node active.
	r.emit("runs", "step_started", map[string]any{"run_id": run.ID, "node": node.ID})
	star, err := r.loader.LoadStar(node.Star)
	if err != nil {
		return nil, fmt.Errorf("constellations: load star %q: %w", node.Star, err)
	}
	// Resolve the star's declared structured-output schema (if any). The invoker
	// enforces it against the backend; an unknown name is an authoring error, not
	// a silent unstructured run.
	var outputSchema []byte
	if star.OutputSchema != "" {
		s, ok := SchemaByName(star.OutputSchema)
		if !ok {
			return nil, fmt.Errorf("constellations: star %q declares unknown output_schema %q", star.Name, star.OutputSchema)
		}
		outputSchema = s
	}
	args, err := evalInputs(node, st.ExprState())
	if err != nil {
		return nil, err
	}

	// Pre-flight coordination check: query active entanglements that intersect
	// this phase's scope and inject sibling-aware notes into the prompt. Advisory
	// only — a failed read (or a skipped check) never fails the run; the merge
	// gate still catches any conflict the coder misses. Gated on the star opting
	// in via coordination_aware (default true for coder-class stars).
	prompt := r.coordinationPrompt(ctx, run, node, star, userPrompt(args, st))

	// Best-effort live stdout tail: when a tail dir is configured, open the
	// per-run .log and tee the star's subprocess stdout into it so the cockpit
	// can stream it to the browser. Every step is best-effort — on any failure
	// we proceed WITHOUT teeing, never altering the dispatch (see rule #1).
	ctx, closeTail := r.attachStdoutTail(ctx, run, node, st)
	defer closeTail()

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
		// Honor the star's [health] block: the invoker monitors this invocation
		// against these thresholds and kills a stalled/thrashing subprocess, so
		// wall_clock_cap etc. in a star file have a real runtime effect.
		Health: contextHealth(star.Health),
		// Schema-enforced structured output when the star declares one: the
		// invoker returns a validated JSON object the downstream operator consumes
		// via result_json, so no stage ever re-parses fenced/prose-wrapped text.
		OutputSchema: outputSchema,
		// Write in the run's workDir — its isolated worktree when worktree
		// isolation is enabled, else the repo root.
	}, prompt, run.RepoPath)
	if err != nil {
		// A dead-coder termination is distinct from a generic failure: the
		// partial work persists in the worktree, so record terminated_health so
		// downstream can route it for reviewer judgement rather than discard.
		status := "failed"
		var dead *claude.DeadCoderError
		if errors.As(err, &dead) {
			status = "terminated_health"
			// The coder was killed mid-cycle but its partial work persists in the
			// worktree. Surface the latest per-dispatch snapshot (from a prior
			// successful cycle) alongside it so the reviewer can choose a known-good
			// fallback. Best-effort: a restore failure — including no prior snapshot
			// to fall back to — must not mask the original termination.
			r.restoreForReview(ctx, run.ID)
		}
		r.recordInvocation(ctx, run, node, star.Name, status, 0, started)
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
	r.maybeCheckpoint(ctx, run.ID, st.Cycle, star)
	return map[string]any{
		"result": res.ResultText,
		// result_json is the schema-validated JSON object when the star declared an
		// output_schema (empty otherwise). Downstream builtin operators consume it
		// instead of re-parsing result text.
		"result_json": string(res.StructuredOutput),
		"cost_usd":    res.CostUSD,
		"session_id":  res.SessionID,
	}, nil
}

// attachStdoutTail opens the per-run tail log and returns a context carrying a
// best-effort stdout tee plus a closer to defer. When r.tailDir is empty (the
// common, no-cockpit case) it is a no-op: it returns ctx unchanged and a no-op
// closer, so the happy path is byte-for-byte unchanged.
//
// SAFETY (rule #1) — teeing must NEVER affect the run. Every step here is
// best-effort: a MkdirAll, OpenFile, or header-write failure is logged to
// stderr and we return ctx WITHOUT a tee. We never return an error and never
// block the dispatch. The invoker's in-memory buffer remains the single source
// of truth for parsing the result regardless of what happens to the tail file.
func (r *Runtime) attachStdoutTail(ctx context.Context, run *fabric.RunRow, node *artifacts.ConstellationNode, st *State) (context.Context, func()) {
	noop := func() {}
	if r.tailDir == "" {
		return ctx, noop
	}
	if err := os.MkdirAll(r.tailDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "tail: mkdir %s (run %s): %v\n", r.tailDir, run.ID, err)
		return ctx, noop
	}
	path := filepath.Join(r.tailDir, run.ID+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tail: open %s (run %s): %v\n", path, run.ID, err)
		return ctx, noop
	}
	// A header delimits each dispatch in the appended log; a write failure here
	// is non-fatal (the tee below still streams the subprocess output).
	if _, err := fmt.Fprintf(f, "\n--- %s (cycle %d) ---\n", node.ID, st.Cycle); err != nil {
		fmt.Fprintf(os.Stderr, "tail: write header %s (run %s): %v\n", path, run.ID, err)
	}
	closer := func() {
		if err := f.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "tail: close %s (run %s): %v\n", path, run.ID, err)
		}
	}
	return agent.WithStdoutTee(ctx, f), closer
}

// coordinationPrompt runs the pre-flight coordination check for a dispatching
// coder and returns the user prompt with a `## Coordination notes` block
// appended when sibling intents intersect the phase. It is a no-op — returning
// the prompt unchanged — when no Check is wired or the star opts out via
// coordination_aware = false. A read failure is logged and swallowed: the check
// is advisory, so a missed note never blocks the dispatch.
//
// KNOWN LIMITATION — this wiring produces NO notes in the runtime today, by
// design for this phase. The PhaseContext is built from run/node identity only:
// Scope and Files are empty, so packageOverlapsScope ranges over an empty slice
// and the symbol-name content match has no files to scan — Notes always returns
// zero. What IS exercised end-to-end is the coordination_aware gate and exactly
// one zero-count telemetry row per coordination-aware dispatch. The override
// allowlists (PhaseContext.Ignore{Deprecations,Signatures}) are likewise inert
// here: they are not populated, and the `[coordination]` frontmatter that would
// source them is not yet parsed in internal/artifacts/loader.go. Package
// matching, symbol matching, and override suppression are validated by unit
// tests (coordination_test.go) but are NOT reachable through this call until a
// follow-up threads the phase spec's scope/files and the [coordination]
// frontmatter into the run State. Do not assume coordination notes back the
// merge gate until that wiring lands.
func (r *Runtime) coordinationPrompt(ctx context.Context, run *fabric.RunRow, node *artifacts.ConstellationNode, star *artifacts.Star, prompt string) string {
	if r.coordination == nil || !star.CoordinationAware {
		return prompt
	}
	notes, err := r.coordination.Notes(ctx, PhaseContext{
		RunID:   run.ID,
		PhaseID: node.ID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "coordination check (run %s node %s): %v\n", run.ID, node.ID, err)
		return prompt
	}
	return agent.AppendCoordinationNotes(prompt, notes)
}

// maybeCheckpoint snapshots the worktree after a successful coder dispatch when a
// checkpointer is wired and the star opts in via [checkpoint] enabled.
//
// Granularity is PER-DISPATCH, not per-build: the capture happens once, here on
// the success path, because the coder's individual tool calls run inside an
// opaque subprocess the runtime cannot observe mid-cycle. A coder that returned
// without a DeadCoderError left a usable tree, so the next cycle's dead coder can
// fall back to it. The headline "three green builds in one cycle, forfeit only
// post-build work" guarantee requires per-build tool events from the invoker and
// is a tracked follow-up — see the internal/checkpoint package comment.
//
// Best-effort — a snapshot failure is logged, never fatal (correctness over
// throughput: a missed checkpoint only forfeits the fallback, it never breaks the
// run). The trigger label is honest about what fired the capture: dispatch
// completion, not a build command.
func (r *Runtime) maybeCheckpoint(ctx context.Context, runID string, cycle int, star *artifacts.Star) {
	if r.checkpointer == nil || !star.Checkpoint.Enabled {
		return
	}
	trigger := "post-dispatch:" + star.Name
	if err := r.checkpointer.Checkpoint(ctx, runID, cycle, trigger); err != nil {
		fmt.Fprintf(os.Stderr, "checkpoint: run %s cycle %d: %v\n", runID, cycle, err)
	}
}

// restoreForReview materializes the latest per-dispatch snapshot next to a copy of
// the dead coder's partial worktree, logging both paths so the reviewer can
// compare them. Best-effort: any failure (including no snapshot to fall back to)
// is logged and swallowed so it never masks the coder termination.
func (r *Runtime) restoreForReview(ctx context.Context, runID string) {
	if r.checkpointer == nil {
		return
	}
	base := filepath.Join(os.TempDir(), "quasar-restore-"+runID)
	partial, checkpoint, err := r.checkpointer.RestoreForReview(ctx, runID, base)
	if err != nil {
		fmt.Fprintf(os.Stderr, "checkpoint: restore-for-review run %s: %v\n", runID, err)
		return
	}
	fmt.Fprintf(os.Stderr, "checkpoint: dead coder run %s — partial=%s checkpoint=%s\n", runID, partial, checkpoint)
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
	b.ResultIsStructured = sb.ResultIsStructured
	return &b
}

// contextHealth converts a star's parsed [health] block into the agent health
// policy the invoker consumes. A zero field means "unset", filled from the
// conservative defaults by WithDefaults, so a star that omits [health] (or
// omits a single field) still gets full monitoring with the 25-minute cap. The
// returned pointer is always non-nil, so every dispatched star is monitored.
func contextHealth(sh artifacts.StarHealthPolicy) *agent.HealthPolicy {
	p := agent.HealthPolicy{
		WallClockCap:         sh.WallClockCap,
		FileWriteIdleCap:     sh.FileWriteIdleCap,
		TokenRateFloor:       sh.TokenRateFloor,
		TokenRateWindow:      sh.TokenRateWindow,
		ToolCallRatioCeiling: sh.ToolCallRatioCeiling,
		ToolCallWindow:       sh.ToolCallWindow,
		CPUIdleCap:           sh.CPUIdleCap,
	}.WithDefaults()
	return &p
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
