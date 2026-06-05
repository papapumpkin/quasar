package loop

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/filter"
	"github.com/papapumpkin/quasar/internal/ui"
)

// CheckpointCleanupFunc is called after a successful task completion when
// resuming from a checkpoint. Callers supply a function that removes the
// checkpoint file, avoiding a circular dependency on the checkpoint package.
type CheckpointCleanupFunc func() error

// Loop orchestrates the coder-reviewer cycle for a single task.
type Loop struct {
	Invoker           agent.Invoker
	UI                ui.UI
	Git               CycleCommitter // Optional; nil disables per-cycle commits.
	Hooks             []Hook         // Lifecycle hooks.
	Linter            Linter         // Optional; nil disables lint checks between coder and reviewer.
	Filter            filter.Filter  // Optional; nil skips pre-reviewer filtering and goes straight to reviewer.
	MaxCycles         int
	MaxLintRetries    int // Max times coder is asked to fix lint issues per cycle. 0 uses DefaultMaxLintRetries.
	MaxFilterFixes    int // Max inner fix attempts per filter failure. 0 uses DefaultMaxFilterFixes.
	MaxBudgetUSD      float64
	Model             string
	CoderPrompt       string
	ReviewPrompt      string
	WorkDir           string
	MCP               *agent.MCPConfig // Optional MCP server config passed to agents.
	CommitSummary     string           // Short label for cycle commit messages. If empty, derived from task title.
	PhaseType         string           // Phase type for conventional commit prefixes (bug→fix, feature→feat, default→ref).
	Fabric            fabric.Fabric    // Optional; when set and FabricEnabled, auto-inject fabric state into prompts.
	FabricEnabled     bool             // When true, inject fabric protocol into agent system prompts.
	TaskID            string           // Task ID for fabric context (QUASAR_TASK_ID).
	ProjectContext    string           // Injected into agent system prompts for prompt caching.
	MaxContextTokens  int              // Token budget for context injection. 0 = use default.
	CacheOptimization bool             // When true, system prompts use a stable prefix for cache hits.
	CacheVerbose      bool             // When true, log cache-related diagnostics to stderr.
	StruggleConfig    StruggleConfig   // Optional; zero value disables struggle detection.
	CheckpointDir     string           // Directory for checkpoint files. Empty disables checkpointing.
	FixEffort         string           // Effort level for lint/filter fix invocations (e.g. "low"). Empty = Claude's default.
	FallbackModel     string           // Automatic fallback model passed to all agents.
	// NewCheckpointHook, when non-nil, is called by RunTask and RunExistingTask
	// to create a checkpoint hook that is prepended to Hooks. The function
	// receives a state accessor (returning the current *CycleState) and must
	// return a Hook. This avoids a circular import between loop and checkpoint.
	NewCheckpointHook func(stateFunc func() *CycleState) Hook

	// cachedCoderSystemPrompt is the pre-computed system prompt for the coder
	// agent, built once at phase start via cacheSystemPrompts. It contains only
	// stable content (ProjectContext + CoderPrompt + FabricProtocol) and must
	// remain byte-identical across all cycles for prompt cache hits.
	cachedCoderSystemPrompt string

	// cachedReviewerSystemPrompt is the pre-computed system prompt for the
	// reviewer agent, built once at phase start via cacheSystemPrompts.
	cachedReviewerSystemPrompt string
}

// TaskResult holds the outcome of a completed task loop.
type TaskResult struct {
	TotalCostUSD     float64
	CyclesUsed       int
	Report           *agent.ReviewReport // From final reviewer cycle (may be nil)
	BaseCommitSHA    string              // HEAD captured at task start
	FinalCommitSHA   string              // last cycle's sealed SHA (or current HEAD as fallback)
	CycleCommits     []string            // per-cycle sealed commit SHAs (index = cycle-1)
	Decompose        bool                // true if the loop exited due to a struggle signal
	StruggleReason   string              // human-readable reason from StruggleSignal.Reason
	AllFindings      []ReviewFinding     // accumulated findings at time of decomposition
	CacheHitCount    int                 // Number of invocations where system prompt hash matched previous.
	CacheMissCount   int                 // Number of invocations where system prompt hash changed.
	TotalCachedBytes int64               // Sum of SystemPromptLen for cache-hit invocations.
}

// RunTask runs the coder-reviewer loop for the given task description,
// generating a synthetic task ID.
func (l *Loop) RunTask(ctx context.Context, taskDescription string) (*TaskResult, error) {
	return l.runLoop(ctx, generateTaskID(), taskDescription)
}

// RunExistingTask runs the coder-reviewer loop for a caller-supplied task ID
// (used when the caller already tracks the task externally, e.g. a nebula
// phase ID).
func (l *Loop) RunExistingTask(ctx context.Context, taskID, taskDescription string) (*TaskResult, error) {
	return l.runLoop(ctx, taskID, taskDescription)
}

// generateTaskID returns a synthetic task ID for ad-hoc loop runs that did not
// supply one. Uses nanosecond timestamp; collisions are astronomically unlikely
// within a single process.
func generateTaskID() string {
	return fmt.Sprintf("task-%d", time.Now().UnixNano())
}

// RunFromCheckpoint resumes a coder-reviewer loop from a previously saved
// checkpoint. The caller is responsible for loading, validating, and converting
// the checkpoint to a CycleState (via checkpoint.ToCycleState) before calling
// this method. The checkpointPhase determines where to resume:
//   - PhaseReviewComplete or PhaseApproved: the completed cycle's results are
//     already captured, so the loop advances to cycle N+1.
//   - PhaseCoding, PhaseReviewing, or any other in-progress phase: the current
//     cycle is restarted from the beginning (agent output may be incomplete).
//
// On successful task completion, cleanup is called to remove the checkpoint
// file. If cleanup is nil it is skipped.
func (l *Loop) RunFromCheckpoint(ctx context.Context, cs *CycleState, checkpointPhase Phase, cleanup CheckpointCleanupFunc) (*TaskResult, error) {
	// Determine the resume cycle based on the checkpoint's phase.
	resumeCycle := cs.Cycle
	switch checkpointPhase {
	case PhaseReviewComplete, PhaseApproved:
		// Completed cycle — advance to the next one.
		resumeCycle = cs.Cycle + 1
	default:
		// In-progress phase — restart the same cycle. Clear potentially
		// incomplete agent output so the coder starts fresh.
		cs.CoderOutput = ""
		cs.ReviewOutput = ""
		cs.LintOutput = ""
		cs.Findings = nil
		cs.Verifications = nil
	}
	cs.Cycle = resumeCycle

	// Emit a resume event so hooks/TUI can indicate we're resuming.
	l.emit(ctx, Event{
		Kind:    EventResumed,
		TaskID:  cs.TaskID,
		Cycle:   resumeCycle,
		Message: "resumed from checkpoint",
	})

	result, err := l.resumeLoop(ctx, cs)
	if err != nil {
		return result, err
	}

	// Successful completion — remove the checkpoint.
	if cleanup != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			l.UI.Error(fmt.Sprintf("failed to remove checkpoint: %v", cleanupErr))
		}
	}

	return result, nil
}

// resumeLoop enters the coder-reviewer loop with a pre-populated CycleState,
// starting from cs.Cycle. It shares the core logic with runLoop but skips
// the initial state creation (already done by the caller).
//
// NOTE: The cycle body is intentionally duplicated from runLoop rather than
// extracted into a shared helper. Extracting would require threading many
// local variables (perAgentBudget, etc.) through parameters, adding complexity
// with limited benefit. If the cycle body evolves significantly, consider
// extracting a shared cycleBody method.
func (l *Loop) resumeLoop(ctx context.Context, cs *CycleState) (*TaskResult, error) {
	if l.CacheOptimization {
		l.cacheSystemPrompts()
	}

	// Auto-wire checkpoint hook for resumed loops too.
	l.wireCheckpointHook(func() *CycleState { return cs })

	perAgentBudget := l.perAgentBudget()
	l.UI.TaskStarted(cs.TaskID, cs.TaskTitle)

	for cycle := cs.Cycle; cycle <= l.MaxCycles; cycle++ {
		cs.Cycle = cycle
		cs.FilterFixedThisCycle = false
		cs.CycleFilterFixAttempts = 0
		cs.CycleFilterFixCostUSD = 0
		l.UI.CycleStart(cycle, l.MaxCycles)

		if err := l.runCoderPhase(ctx, cs, perAgentBudget); err != nil {
			return nil, err
		}
		if err := l.checkBudget(ctx, cs); err != nil {
			return nil, err
		}

		if err := l.runLintFixLoop(ctx, cs, perAgentBudget); err != nil {
			return nil, err
		}

		if l.Filter != nil {
			failed, err := l.runFilterChecks(ctx, cs)
			if err != nil {
				return nil, err
			}
			if failed {
				fixed, err := l.runFilterFixLoop(ctx, cs, cs.FilterCheckName, cs.FilterOutput)
				if err != nil {
					return nil, err
				}
				if !fixed {
					cs.FilterHistory = append(cs.FilterHistory, cs.FilterCheckName)
					l.sealCycleSHA(cs)
					l.emit(ctx, Event{Kind: EventCycleStart, TaskID: cs.TaskID, Cycle: cycle})
					continue
				}
				cs.FilterFixedThisCycle = true
				var revalResult *filter.Result
				var revalErr error
				if chain, ok := l.Filter.(*filter.Chain); ok {
					revalResult, revalErr = chain.RunFrom(ctx, l.WorkDir, cs.FilterCheckName)
				} else {
					revalResult, revalErr = l.Filter.Run(ctx, l.WorkDir)
				}
				if revalErr != nil {
					return nil, fmt.Errorf("filter re-validation failed: %w", revalErr)
				}
				if !revalResult.Passed {
					revalFail := revalResult.FirstFailure()
					cs.FilterOutput = revalFail.Output
					cs.FilterCheckName = revalFail.Name
					cs.FilterHistory = append(cs.FilterHistory, cs.FilterCheckName)
					l.sealCycleSHA(cs)
					l.emit(ctx, Event{Kind: EventCycleStart, TaskID: cs.TaskID, Cycle: cycle})
					continue
				}
				if len(cs.AllFindings) > 0 {
					cs.AllFindings = cs.AllFindings[:len(cs.AllFindings)-1]
				}
				cs.Findings = nil
				cs.FilterOutput = ""
				cs.FilterCheckName = ""
				cs.FilterFixAttempts = 0
			}
		}

		if err := l.runReviewerPhase(ctx, cs, perAgentBudget); err != nil {
			return nil, err
		}
		if err := l.checkBudget(ctx, cs); err != nil {
			return nil, err
		}

		if len(cs.Verifications) > 0 {
			summary := ApplyVerifications(cs.AllFindings, cs.Verifications)
			l.UI.FindingLifecycle(cs.Cycle, ui.FindingLifecycleData{
				Fixed:        summary.Fixed,
				StillPresent: summary.StillPresent,
				Regressed:    summary.Regressed,
			})
		}

		if isApproved(cs.ReviewOutput) {
			return l.handleApproval(ctx, cs)
		}

		cs.FilterHistory = append(cs.FilterHistory, cs.FilterCheckName)
		l.sealCycleSHA(cs)

		l.UI.IssuesFound(len(cs.Findings))
		cs.Phase = PhaseResolvingIssues
		for i := range cs.Findings {
			cs.Findings[i].Cycle = cs.Cycle
		}
		cs.AllFindings = append(cs.AllFindings, cs.Findings...)

		if l.StruggleConfig.Enabled {
			signal := EvaluateStruggle(cs, l.StruggleConfig)
			if signal.Triggered {
				l.emit(ctx, Event{
					Kind:    EventStruggleDetected,
					TaskID:  cs.TaskID,
					Cycle:   cycle,
					Message: signal.Reason,
				})
				return &TaskResult{
					TotalCostUSD:     cs.TotalCostUSD,
					CyclesUsed:       cs.Cycle,
					BaseCommitSHA:    cs.BaseCommitSHA,
					FinalCommitSHA:   l.finalCommitSHA(ctx, cs),
					CycleCommits:     cs.CycleCommits,
					Decompose:        true,
					StruggleReason:   signal.Reason,
					AllFindings:      cs.AllFindings,
					CacheHitCount:    cs.cacheHitCount,
					CacheMissCount:   cs.cacheMissCount,
					TotalCachedBytes: cs.totalCachedBytes,
				}, nil
			}
		}

		l.emit(ctx, Event{Kind: EventCycleStart, TaskID: cs.TaskID, Cycle: cycle})
	}

	l.UI.MaxCyclesReached(l.MaxCycles)
	l.emit(ctx, Event{
		Kind:    EventTaskFailed,
		TaskID:  cs.TaskID,
		Message: fmt.Sprintf("Max cycles reached (%d). Manual review recommended.", l.MaxCycles),
	})
	return &TaskResult{
		TotalCostUSD:     cs.TotalCostUSD,
		CyclesUsed:       cs.Cycle,
		BaseCommitSHA:    cs.BaseCommitSHA,
		FinalCommitSHA:   l.finalCommitSHA(ctx, cs),
		CycleCommits:     cs.CycleCommits,
		CacheHitCount:    cs.cacheHitCount,
		CacheMissCount:   cs.cacheMissCount,
		TotalCachedBytes: cs.totalCachedBytes,
	}, ErrMaxCycles
}

// wireCheckpointHook prepends a checkpoint hook to l.Hooks when both
// CheckpointDir is set and NewCheckpointHook is provided. This is a no-op
// when either condition is false, preserving existing behavior.
func (l *Loop) wireCheckpointHook(stateFunc func() *CycleState) {
	if l.CheckpointDir == "" || l.NewCheckpointHook == nil {
		return
	}
	hook := l.NewCheckpointHook(stateFunc)
	l.Hooks = append([]Hook{hook}, l.Hooks...)
}

// GenerateCheckpoint asks the coder to summarize its current progress for resumption.
func (l *Loop) GenerateCheckpoint(ctx context.Context, taskID, taskDescription string) (string, error) {
	a := agent.Agent{
		Role:         agent.RoleCoder,
		SystemPrompt: l.CoderPrompt,
		Model:        l.Model,
		MaxBudgetUSD: 0.50,
		AllowedTools: []string{"Read", "Glob", "Grep"},
	}
	prompt := fmt.Sprintf(
		"You were working on task (bead %s): %s\n\n"+
			"Summarize your current progress concisely:\n"+
			"- What you have completed\n"+
			"- What files you changed\n"+
			"- What remains to be done\n"+
			"- Any important context for continuing",
		taskID, taskDescription,
	)
	result, err := l.Invoker.Invoke(ctx, a, prompt, l.WorkDir)
	if err != nil {
		return "", err
	}
	return result.ResultText, nil
}

// emit fans out a lifecycle event to all registered hooks.
func (l *Loop) emit(ctx context.Context, event Event) {
	for _, h := range l.Hooks {
		h.OnEvent(ctx, event)
	}
}

// runLoop is the core coder-reviewer loop extracted from RunTask.
func (l *Loop) runLoop(ctx context.Context, taskID, taskDescription string) (*TaskResult, error) {
	// Pre-compute system prompts once for the entire phase. These contain
	// only stable content (ProjectContext + basePrompt + FabricProtocol)
	// and must remain byte-identical across all cycles for prompt cache hits.
	// When CacheOptimization is disabled, skip pre-computation so prompts
	// are rebuilt per cycle (legacy behavior).
	if l.CacheOptimization {
		l.cacheSystemPrompts()
	}

	perAgentBudget := l.perAgentBudget()
	state := l.initCycleState(ctx, taskID, taskDescription)

	// Auto-wire checkpoint hook when CheckpointDir is set and a factory
	// is provided. The hook is prepended so it runs before other hooks.
	l.wireCheckpointHook(func() *CycleState { return state })

	for cycle := 1; cycle <= l.MaxCycles; cycle++ {
		state.Cycle = cycle
		// Reset per-cycle filter fix tracking at cycle start.
		state.FilterFixedThisCycle = false
		state.CycleFilterFixAttempts = 0
		state.CycleFilterFixCostUSD = 0
		l.UI.CycleStart(cycle, l.MaxCycles)

		if err := l.runCoderPhase(ctx, state, perAgentBudget); err != nil {
			return nil, err
		}
		if err := l.checkBudget(ctx, state); err != nil {
			return nil, err
		}

		// Run lint checks and let the coder fix issues before reviewer handoff.
		if err := l.runLintFixLoop(ctx, state, perAgentBudget); err != nil {
			return nil, err
		}

		// Run pre-reviewer filter checks. If the filter fails, attempt
		// a fast inner fix loop before burning a full outer cycle.
		if l.Filter != nil {
			failed, err := l.runFilterChecks(ctx, state)
			if err != nil {
				return nil, err
			}
			if failed {
				// Attempt fast inner fix loop before burning a full outer cycle.
				fixed, err := l.runFilterFixLoop(ctx, state, state.FilterCheckName, state.FilterOutput)
				if err != nil {
					return nil, err
				}
				if !fixed {
					// Inner loop exhausted — fall through to outer cycle bounce.
					state.FilterHistory = append(state.FilterHistory, state.FilterCheckName)
					l.sealCycleSHA(state)
					l.emit(ctx, Event{Kind: EventCycleStart, TaskID: taskID, Cycle: cycle})
					continue
				}
				state.FilterFixedThisCycle = true
				// Fixed! Re-validate the chain from the failure point onward
				// to catch regressions the fix may have introduced.
				var revalResult *filter.Result
				var revalErr error
				if chain, ok := l.Filter.(*filter.Chain); ok {
					revalResult, revalErr = chain.RunFrom(ctx, l.WorkDir, state.FilterCheckName)
				} else {
					// Non-Chain filter — full re-run.
					revalResult, revalErr = l.Filter.Run(ctx, l.WorkDir)
				}
				if revalErr != nil {
					return nil, fmt.Errorf("filter re-validation failed: %w", revalErr)
				}
				if !revalResult.Passed {
					// Re-validation found a new failure — bounce to outer cycle.
					revalFail := revalResult.FirstFailure()
					state.FilterOutput = revalFail.Output
					state.FilterCheckName = revalFail.Name
					state.FilterHistory = append(state.FilterHistory, state.FilterCheckName)
					l.sealCycleSHA(state)
					l.emit(ctx, Event{Kind: EventCycleStart, TaskID: taskID, Cycle: cycle})
					continue
				}

				// Remove the synthetic finding that runFilterChecks
				// appended so the reviewer doesn't waste tokens verifying a
				// resolved issue and struggle detection doesn't see a phantom
				// critical finding.
				if len(state.AllFindings) > 0 {
					state.AllFindings = state.AllFindings[:len(state.AllFindings)-1]
				}
				state.Findings = nil
				// Clear filter state and proceed to reviewer.
				state.FilterOutput = ""
				state.FilterCheckName = ""
				state.FilterFixAttempts = 0
			}
		}

		if err := l.runReviewerPhase(ctx, state, perAgentBudget); err != nil {
			return nil, err
		}
		if err := l.checkBudget(ctx, state); err != nil {
			return nil, err
		}

		// Apply verification results to update finding lifecycle statuses.
		if len(state.Verifications) > 0 {
			summary := ApplyVerifications(state.AllFindings, state.Verifications)
			l.UI.FindingLifecycle(state.Cycle, ui.FindingLifecycleData{
				Fixed:        summary.Fixed,
				StillPresent: summary.StillPresent,
				Regressed:    summary.Regressed,
			})
		}

		if isApproved(state.ReviewOutput) {
			return l.handleApproval(ctx, state)
		}

		// Record this cycle's filter check name (empty if filter passed or was nil).
		state.FilterHistory = append(state.FilterHistory, state.FilterCheckName)

		// Seal the cycle's final SHA into CycleCommits before moving on.
		l.sealCycleSHA(state)

		// Check for a mid-run refactor signal before starting the next cycle.

		l.UI.IssuesFound(len(state.Findings))
		state.Phase = PhaseResolvingIssues
		// Tag findings with the current cycle number before accumulating
		// so the Cycle field is available downstream.
		for i := range state.Findings {
			state.Findings[i].Cycle = state.Cycle
		}
		state.AllFindings = append(state.AllFindings, state.Findings...)

		// Evaluate struggle detection after findings are accumulated.
		if l.StruggleConfig.Enabled {
			signal := EvaluateStruggle(state, l.StruggleConfig)
			if signal.Triggered {
				l.emit(ctx, Event{
					Kind:    EventStruggleDetected,
					TaskID:  taskID,
					Cycle:   cycle,
					Message: signal.Reason,
				})
				return &TaskResult{
					TotalCostUSD:     state.TotalCostUSD,
					CyclesUsed:       state.Cycle,
					BaseCommitSHA:    state.BaseCommitSHA,
					FinalCommitSHA:   l.finalCommitSHA(ctx, state),
					CycleCommits:     state.CycleCommits,
					Decompose:        true,
					StruggleReason:   signal.Reason,
					AllFindings:      state.AllFindings,
					CacheHitCount:    state.cacheHitCount,
					CacheMissCount:   state.cacheMissCount,
					TotalCachedBytes: state.totalCachedBytes,
				}, nil
			}
		}

		l.emit(ctx, Event{Kind: EventCycleStart, TaskID: taskID, Cycle: cycle})
	}

	l.UI.MaxCyclesReached(l.MaxCycles)
	l.emit(ctx, Event{
		Kind:    EventTaskFailed,
		TaskID:  taskID,
		Message: fmt.Sprintf("Max cycles reached (%d). Manual review recommended.", l.MaxCycles),
	})
	return &TaskResult{
		TotalCostUSD:     state.TotalCostUSD,
		CyclesUsed:       state.Cycle,
		BaseCommitSHA:    state.BaseCommitSHA,
		FinalCommitSHA:   l.finalCommitSHA(ctx, state),
		CycleCommits:     state.CycleCommits,
		CacheHitCount:    state.cacheHitCount,
		CacheMissCount:   state.cacheMissCount,
		TotalCachedBytes: state.totalCachedBytes,
	}, ErrMaxCycles
}

// maxLintRetries returns the effective maximum lint retry count.
func (l *Loop) maxLintRetries() int {
	if l.MaxLintRetries > 0 {
		return l.MaxLintRetries
	}
	return DefaultMaxLintRetries
}

// runLintFixLoop runs lint commands after the coder pass. If issues are found,
// it feeds them back to the coder for fixing, up to maxLintRetries times.
// After the retry limit, any remaining lint output is preserved in state so
// the reviewer can flag it. A nil Linter makes this a no-op.
func (l *Loop) runLintFixLoop(ctx context.Context, state *CycleState, perAgentBudget float64) error {
	if l.Linter == nil {
		return nil
	}

	maxRetries := l.maxLintRetries()
	for attempt := 0; attempt <= maxRetries; attempt++ {
		state.Phase = PhaseLinting
		l.UI.Info("running lint checks…")

		output, err := l.Linter.Run(ctx)
		if err != nil {
			// Lint execution error is non-fatal; log and continue to reviewer.
			l.UI.Error(fmt.Sprintf("lint execution error: %v", err))
			state.LintOutput = ""
			return nil
		}

		if output == "" {
			// Clean lint pass — proceed to reviewer.
			state.LintOutput = ""
			l.UI.Info("lint checks passed")
			return nil
		}

		state.LintOutput = output

		if attempt == maxRetries {
			// Max retries reached — let the reviewer see what's left.
			l.UI.Info(fmt.Sprintf("lint issues remain after %d retries, proceeding to reviewer", maxRetries))
			return nil
		}

		// Feed lint issues back to the coder.
		l.UI.Info(fmt.Sprintf("lint issues found (attempt %d/%d), sending back to coder", attempt+1, maxRetries))
		lintPrompt := l.buildLintFixPrompt(state)
		lintCoder := l.coderAgent(perAgentBudget)
		if l.FixEffort != "" {
			lintCoder.Effort = l.FixEffort
		}
		result, err := l.Invoker.Invoke(ctx, lintCoder, lintPrompt, l.WorkDir)
		if err != nil {
			return fmt.Errorf("coder lint-fix invocation failed: %w", err)
		}

		state.CoderOutput = result.ResultText
		state.TotalCostUSD += result.CostUSD
		l.UI.AgentDone("coder", result.CostUSD, result.DurationMs)

		if err := l.checkBudget(ctx, state); err != nil {
			return err
		}

		// Re-commit after lint fixes so the reviewer sees clean state.
		// Overwrites lastCycleSHA so only the final commit is sealed.
		if l.Git != nil {
			summary := l.CommitSummary
			if summary == "" {
				summary = firstLine(state.TaskTitle, 72)
			}
			sha, commitErr := l.Git.CommitCycle(ctx, state.TaskID, state.Cycle, summary+" (lint fix)", l.PhaseType)
			if commitErr != nil {
				l.UI.Error(fmt.Sprintf("failed to commit lint fix: %v", commitErr))
			} else {
				state.lastCycleSHA = sha
			}
		}
	}

	return nil
}

// runFilterChecks runs the pre-reviewer filter chain. If the filter fails, it
// records a synthetic finding from the failing check and returns true to signal
// the caller should skip the reviewer and bounce to the next coder cycle.
// Returns (false, nil) when the filter passes or is nil.
func (l *Loop) runFilterChecks(ctx context.Context, state *CycleState) (failed bool, err error) {
	state.Phase = PhaseFiltering
	l.UI.Info("running pre-reviewer filter checks…")

	result, err := l.Filter.Run(ctx, l.WorkDir)
	if err != nil {
		// Infrastructure error (e.g. context canceled) is fatal.
		return false, fmt.Errorf("filter execution failed: %w", err)
	}

	if result.Passed {
		state.FilterOutput = ""
		state.FilterCheckName = ""
		l.UI.Info("filter checks passed")
		return false, nil
	}

	// Filter failed — build a synthetic finding from the first failure.
	failure := result.FirstFailure()
	state.FilterOutput = failure.Output
	state.FilterCheckName = failure.Name
	l.UI.Info(fmt.Sprintf("filter check %q failed (%s), bouncing to coder", failure.Name, failure.Elapsed))

	// Surface the failure as a finding so the coder sees it next cycle.
	state.Findings = []ReviewFinding{{
		Severity:    "critical",
		Description: fmt.Sprintf("[filter:%s] %s", failure.Name, truncate(failure.Output, 3000)),
		Cycle:       state.Cycle,
	}}
	l.UI.IssuesFound(1)
	state.Phase = PhaseResolvingIssues
	state.AllFindings = append(state.AllFindings, state.Findings...)

	return true, nil
}

// DefaultMaxFilterFixes is the maximum number of inner fix attempts per
// filter failure before falling through to the outer coder-reviewer cycle.
const DefaultMaxFilterFixes = 3

// SingleCheckRunner can execute a single named check. Defined in the loop
// package where consumed, satisfied by *filter.Chain.
type SingleCheckRunner interface {
	RunCheck(ctx context.Context, workDir string, name string) (*filter.CheckResult, error)
}

// maxFilterFixes returns the effective maximum filter fix attempt count.
func (l *Loop) maxFilterFixes() int {
	if l.MaxFilterFixes > 0 {
		return l.MaxFilterFixes
	}
	return DefaultMaxFilterFixes
}

// runFilterFixLoop attempts to fix a failing filter check via a fast inner loop.
// It parses the error output, sends a focused fix prompt to the coder, re-runs
// only the failing check, and repeats up to maxFilterFixes times. Returns true
// if the check ultimately passes, false if retries are exhausted.
//
// Claims check failures are never short-circuited — they require coordination,
// not code fixes, so they always fall through to the outer cycle.
func (l *Loop) runFilterFixLoop(ctx context.Context, state *CycleState, checkName string, checkOutput string) (bool, error) {
	// Claims failures need human/fabric coordination, not automated fixes.
	if checkName == "claims" {
		return false, nil
	}

	// The filter must support single-check re-runs.
	runner, ok := l.Filter.(SingleCheckRunner)
	if !ok {
		return false, nil
	}

	parsed := filter.ParseCheckOutput(filter.CheckResult{
		Name:   checkName,
		Output: checkOutput,
	})

	maxFixes := l.maxFilterFixes()
	fixBudget := l.filterFixBudget()

	for attempt := 0; attempt < maxFixes; attempt++ {
		l.UI.Info(fmt.Sprintf("filter check %q failed with %d error(s), attempting targeted fix (attempt %d/%d)",
			checkName, len(parsed.Errors), attempt+1, maxFixes))

		// Build a focused fix prompt and invoke the coder with restricted tools.
		// Use the pre-computed system prompt for cache hits. Fall back to
		// building on the fly when the cache is empty (e.g., unit tests
		// calling runFilterFixLoop directly without cacheSystemPrompts).
		prompt := l.buildFilterFixPrompt(state, parsed)
		sysPrompt := l.cachedCoderSystemPrompt
		if sysPrompt == "" {
			sysPrompt = agent.BuildSystemPrompt(l.CoderPrompt, agent.PromptOpts{
				FabricEnabled:  l.FabricEnabled,
				TaskID:         l.TaskID,
				ProjectContext: l.ProjectContext,
			})
		}
		a := agent.Agent{
			Role:          agent.RoleCoder,
			SystemPrompt:  sysPrompt,
			Model:         l.Model,
			MaxBudgetUSD:  fixBudget,
			AllowedTools:  []string{"Read", "Edit", "Write", "Glob"},
			MCP:           l.MCP,
			Effort:        l.FixEffort,
			FallbackModel: l.FallbackModel,
		}

		result, err := l.Invoker.Invoke(ctx, a, prompt, l.WorkDir)
		if err != nil {
			return false, fmt.Errorf("coder filter-fix invocation failed: %w", err)
		}

		state.TotalCostUSD += result.CostUSD
		state.FilterFixCostUSD += result.CostUSD
		state.FilterFixAttempts++
		state.CycleFilterFixCostUSD += result.CostUSD
		state.CycleFilterFixAttempts++
		l.UI.AgentDone("coder", result.CostUSD, result.DurationMs)

		if err := l.checkBudget(ctx, state); err != nil {
			return false, err
		}

		// Commit the fix if Git is available.
		if l.Git != nil {
			summary := l.CommitSummary
			if summary == "" {
				summary = firstLine(state.TaskTitle, 72)
			}
			sha, commitErr := l.Git.CommitCycle(ctx, state.TaskID, state.Cycle, summary+" (filter fix)", l.PhaseType)
			if commitErr != nil {
				l.UI.Error(fmt.Sprintf("failed to commit filter fix: %v", commitErr))
			} else {
				state.lastCycleSHA = sha
			}
		}

		// Re-run only the failing check.
		cr, err := runner.RunCheck(ctx, l.WorkDir, checkName)
		if err != nil {
			return false, fmt.Errorf("re-running check %q: %w", checkName, err)
		}

		checkPassed := cr.Passed

		// Emit per-attempt telemetry event.
		l.emit(ctx, Event{
			Kind:   EventFilterFixAttempt,
			TaskID: state.TaskID,
			Cycle:  state.Cycle,
			FilterFix: &FilterFixData{
				CheckName:   checkName,
				Attempt:     attempt + 1,
				MaxAttempts: maxFixes,
				Fixed:       checkPassed,
				CostUSD:     result.CostUSD,
				ErrorCount:  len(parsed.Errors),
				DurationMs:  result.DurationMs,
			},
		})

		if checkPassed {
			l.UI.Info(fmt.Sprintf("filter check %q fixed on attempt %d", checkName, attempt+1))
			l.emit(ctx, Event{
				Kind:   EventFilterFixResult,
				TaskID: state.TaskID,
				Cycle:  state.Cycle,
				FilterFix: &FilterFixData{
					CheckName:   checkName,
					Attempt:     state.FilterFixAttempts,
					MaxAttempts: maxFixes,
					Fixed:       true,
					CostUSD:     state.FilterFixCostUSD,
					ErrorCount:  len(parsed.Errors),
				},
			})
			return true, nil
		}

		// Still failing — update parsed errors for next iteration.
		checkOutput = cr.Output
		parsed = filter.ParseCheckOutput(filter.CheckResult{
			Name:   checkName,
			Output: checkOutput,
		})
	}

	l.UI.Info(fmt.Sprintf("filter check %q not fixed after %d attempts, falling back to outer cycle",
		checkName, maxFixes))
	l.emit(ctx, Event{
		Kind:   EventFilterFixResult,
		TaskID: state.TaskID,
		Cycle:  state.Cycle,
		FilterFix: &FilterFixData{
			CheckName:   checkName,
			Attempt:     state.FilterFixAttempts,
			MaxAttempts: maxFixes,
			Fixed:       false,
			CostUSD:     state.FilterFixCostUSD,
			ErrorCount:  len(parsed.Errors),
		},
	})
	return false, nil
}

// perAgentBudget computes the per-invocation budget by splitting the total
// evenly between coder and reviewer across all cycles.
func (l *Loop) perAgentBudget() float64 {
	if l.MaxBudgetUSD <= 0 {
		return 0
	}
	return l.MaxBudgetUSD / float64(2*l.MaxCycles)
}

// initCycleState creates the initial cycle state and emits task-started events.
func (l *Loop) initCycleState(ctx context.Context, taskID, taskDescription string) *CycleState {
	l.UI.TaskStarted(taskID, taskDescription)
	l.emit(ctx, Event{Kind: EventCycleStart, TaskID: taskID})

	// Capture HEAD before the first cycle for later diffing.
	var baseSHA string
	if l.Git != nil {
		sha, err := l.Git.HeadSHA(ctx)
		if err != nil {
			l.UI.Error(fmt.Sprintf("failed to capture base commit SHA: %v", err))
		} else {
			baseSHA = sha
		}
	}

	return &CycleState{
		TaskID:        taskID,
		TaskTitle:     taskDescription,
		Phase:         PhaseTaskCreated,
		MaxCycles:     l.MaxCycles,
		MaxBudgetUSD:  l.MaxBudgetUSD,
		BaseCommitSHA: baseSHA,
	}
}

// cacheSystemPrompts pre-computes and stores the system prompts for both
// coder and reviewer agents. This must be called once at phase start (in
// runLoop) before the cycle loop begins. By building the prompts once, we
// guarantee byte-identical system prompts across all cycles within a phase,
// which is critical for prompt cache hits in the Claude CLI.
func (l *Loop) cacheSystemPrompts() {
	opts := agent.PromptOpts{
		FabricEnabled:  l.FabricEnabled,
		TaskID:         l.TaskID,
		ProjectContext: l.ProjectContext,
	}
	l.cachedCoderSystemPrompt = agent.BuildSystemPrompt(l.CoderPrompt, opts)
	l.cachedReviewerSystemPrompt = agent.BuildSystemPrompt(l.ReviewPrompt, opts)
}

// coderAgent builds the agent configuration for the coder role.
// It uses the pre-computed system prompt cached at phase start by
// cacheSystemPrompts. If the cache is empty (e.g., in unit tests that call
// coderAgent directly), it falls back to building the prompt on the fly.
func (l *Loop) coderAgent(budget float64) agent.Agent {
	sysPrompt := l.cachedCoderSystemPrompt
	if sysPrompt == "" {
		sysPrompt = agent.BuildSystemPrompt(l.CoderPrompt, agent.PromptOpts{
			FabricEnabled:  l.FabricEnabled,
			TaskID:         l.TaskID,
			ProjectContext: l.ProjectContext,
		})
	}
	return agent.Agent{
		Role:         agent.RoleCoder,
		SystemPrompt: sysPrompt,
		Model:        l.Model,
		MaxBudgetUSD: budget,
		AllowedTools: []string{
			"Read", "Edit", "Write", "Glob", "Grep",
			"Bash(go *)", "Bash(git diff *)", "Bash(git status)", "Bash(git log *)",
		},
		MCP:           l.MCP,
		FallbackModel: l.FallbackModel,
	}
}

// reviewerAgent builds the agent configuration for the reviewer role.
// It uses the pre-computed system prompt cached at phase start by
// cacheSystemPrompts. If the cache is empty, it falls back to building
// the prompt on the fly for backward compatibility.
func (l *Loop) reviewerAgent(budget float64) agent.Agent {
	sysPrompt := l.cachedReviewerSystemPrompt
	if sysPrompt == "" {
		sysPrompt = agent.BuildSystemPrompt(l.ReviewPrompt, agent.PromptOpts{
			FabricEnabled:  l.FabricEnabled,
			TaskID:         l.TaskID,
			ProjectContext: l.ProjectContext,
		})
	}
	return agent.Agent{
		Role:         agent.RoleReviewer,
		SystemPrompt: sysPrompt,
		Model:        l.Model,
		MaxBudgetUSD: budget,
		AllowedTools: []string{
			"Read", "Glob", "Grep",
			"Bash(go vet *)", "Bash(git diff *)", "Bash(git log *)",
		},
		MCP:           l.MCP,
		FallbackModel: l.FallbackModel,
	}
}

// runCoderPhase invokes the coder agent, updates state and UI, and emits
// lifecycle events.
func (l *Loop) runCoderPhase(ctx context.Context, state *CycleState, perAgentBudget float64) error {
	state.Phase = PhaseCoding
	l.UI.AgentStart("coder")

	prompt := l.buildCoderPrompt(state)
	prompt = l.composeVolatilePrefix(ctx, prompt)

	coder := l.coderAgent(perAgentBudget)
	// Resume the coder's prior session on cycle 2+ to avoid re-reading
	// the entire codebase each iteration.
	if state.Cycle > 1 && state.coderSessionID != "" {
		coder.ResumeSessionID = state.coderSessionID
	}

	result, err := l.Invoker.Invoke(ctx, coder, prompt, l.WorkDir)
	if err != nil {
		state.Phase = PhaseError
		return fmt.Errorf("coder invocation failed: %w", err)
	}

	// Store the session ID for potential resume in the next cycle.
	if result.SessionID != "" {
		state.coderSessionID = result.SessionID
	}

	state.CoderOutput = result.ResultText
	state.TotalCostUSD += result.CostUSD
	state.Phase = PhaseCodeComplete
	l.UI.AgentOutput("coder", state.Cycle, result.ResultText)
	l.UI.AgentDone("coder", result.CostUSD, result.DurationMs)
	l.emitCycleSummary(state, PhaseCodeComplete, result)

	// Commit the coder's changes for this cycle.
	// The SHA is stored in lastCycleSHA and sealed into CycleCommits at cycle end.
	if l.Git != nil {
		summary := l.CommitSummary
		if summary == "" {
			summary = firstLine(state.TaskTitle, 72)
		}
		sha, err := l.Git.CommitCycle(ctx, state.TaskID, state.Cycle, summary, l.PhaseType)
		if err != nil {
			l.UI.Error(fmt.Sprintf("failed to commit cycle %d: %v", state.Cycle, err))
		} else {
			state.lastCycleSHA = sha
		}
	}

	l.emit(ctx, Event{
		Kind:    EventAgentDone,
		TaskID:  state.TaskID,
		Cycle:   state.Cycle,
		Agent:   "coder",
		Result:  &result,
		Message: fmt.Sprintf("[coder cycle %d]\n%s", state.Cycle, truncate(result.ResultText, 2000)),
	})
	l.trackCacheMetrics(ctx, state, "coder", &result)
	return nil
}

// runReviewerPhase invokes the reviewer agent, updates state and UI, parses
// findings, and emits lifecycle events.
func (l *Loop) runReviewerPhase(ctx context.Context, state *CycleState, perAgentBudget float64) error {
	state.Phase = PhaseReviewing
	l.UI.AgentStart("reviewer")

	prompt := l.buildReviewerPrompt(state)
	prompt = l.composeVolatilePrefix(ctx, prompt)

	result, err := l.Invoker.Invoke(ctx, l.reviewerAgent(perAgentBudget), prompt, l.WorkDir)
	if err != nil {
		state.Phase = PhaseError
		return fmt.Errorf("reviewer invocation failed: %w", err)
	}

	state.ReviewOutput = result.ResultText
	state.TotalCostUSD += result.CostUSD
	state.Phase = PhaseReviewComplete
	l.UI.AgentOutput("reviewer", state.Cycle, result.ResultText)
	l.UI.AgentDone("reviewer", result.CostUSD, result.DurationMs)
	state.Findings = ParseReviewFindings(result.ResultText)
	state.Verifications = ParseVerifications(result.ResultText)
	l.emit(ctx, Event{
		Kind:    EventAgentDone,
		TaskID:  state.TaskID,
		Cycle:   state.Cycle,
		Agent:   "reviewer",
		Result:  &result,
		Message: fmt.Sprintf("[reviewer cycle %d]\n%s", state.Cycle, truncate(result.ResultText, 2000)),
	})
	l.trackCacheMetrics(ctx, state, "reviewer", &result)
	l.emitCycleSummary(state, PhaseReviewComplete, result)
	return nil
}

// emitCycleSummary sends a cycle summary to the UI for the given phase.
func (l *Loop) emitCycleSummary(state *CycleState, phase Phase, result agent.InvocationResult) {
	l.UI.CycleSummary(ui.CycleSummaryData{
		Cycle:             state.Cycle,
		MaxCycles:         l.MaxCycles,
		Phase:             phase.String(),
		CostUSD:           result.CostUSD,
		TotalCostUSD:      state.TotalCostUSD,
		MaxBudgetUSD:      l.MaxBudgetUSD,
		DurationMs:        result.DurationMs,
		Approved:          isApproved(state.ReviewOutput),
		IssueCount:        len(state.Findings),
		FilterFixAttempts: state.CycleFilterFixAttempts,
		FilterFixCostUSD:  state.CycleFilterFixCostUSD,
		FilterFixSuccess:  state.FilterFixedThisCycle,
	})
}

// trackCacheMetrics emits a cache metrics event and updates the cycle state's
// cache hit/miss counters. It also logs cache stability diagnostics to stderr
// when CacheVerbose is enabled.
func (l *Loop) trackCacheMetrics(ctx context.Context, state *CycleState, agentRole string, result *agent.InvocationResult) {
	hash := result.SystemPromptHash
	if state.prevSystemPromptHash != "" && hash == state.prevSystemPromptHash {
		state.cacheHitCount++
		state.totalCachedBytes += int64(result.SystemPromptLen)
		if l.CacheVerbose {
			fmt.Fprintf(os.Stderr, "[cache] %s cycle %d: system prompt STABLE (hash match, %d bytes cached)\n",
				agentRole, state.Cycle, result.SystemPromptLen)
		}
	} else {
		state.cacheMissCount++
		if l.CacheVerbose && state.prevSystemPromptHash != "" {
			fmt.Fprintf(os.Stderr, "[cache] %s cycle %d: system prompt CHANGED (cache miss, prev=%.8s curr=%.8s)\n",
				agentRole, state.Cycle, state.prevSystemPromptHash, hash)
		}
	}
	state.prevSystemPromptHash = hash

	l.emit(ctx, Event{
		Kind:   EventCacheMetrics,
		TaskID: state.TaskID,
		Cycle:  state.Cycle,
		Agent:  agentRole,
		Result: result,
		Message: fmt.Sprintf("sys_prompt_hash=%s sys_prompt_len=%d user_prompt_len=%d cost=%.4f",
			hash, result.SystemPromptLen, result.UserPromptLen, result.CostUSD),
	})
}

// checkBudget returns ErrBudgetExceeded if the total cost has reached the limit.
func (l *Loop) checkBudget(ctx context.Context, state *CycleState) error {
	if l.MaxBudgetUSD <= 0 || state.TotalCostUSD < l.MaxBudgetUSD {
		return nil
	}
	l.UI.BudgetExceeded(state.TotalCostUSD, l.MaxBudgetUSD)
	l.emit(ctx, Event{
		Kind:    EventTaskFailed,
		TaskID:  state.TaskID,
		Message: fmt.Sprintf("Budget exceeded: $%.4f / $%.2f", state.TotalCostUSD, l.MaxBudgetUSD),
	})
	return ErrBudgetExceeded
}

// handleApproval seals the final cycle's commit SHA, emits success events,
// records the review report, and returns the final result.
func (l *Loop) handleApproval(ctx context.Context, state *CycleState) (*TaskResult, error) {
	l.sealCycleSHA(state)
	state.Phase = PhaseApproved
	l.UI.Approved()

	report := ParseReviewReport(state.ReviewOutput)

	l.emit(ctx, Event{
		Kind:   EventTaskSuccess,
		TaskID: state.TaskID,
		Cycle:  state.Cycle,
		Report: report,
	})

	l.UI.TaskComplete(state.TaskID, state.TotalCostUSD)
	return &TaskResult{
		TotalCostUSD:     state.TotalCostUSD,
		CyclesUsed:       state.Cycle,
		Report:           report,
		BaseCommitSHA:    state.BaseCommitSHA,
		FinalCommitSHA:   l.finalCommitSHA(ctx, state),
		CycleCommits:     state.CycleCommits,
		CacheHitCount:    state.cacheHitCount,
		CacheMissCount:   state.cacheMissCount,
		TotalCachedBytes: state.totalCachedBytes,
	}, nil
}

// sealCycleSHA appends the current cycle's last commit SHA to CycleCommits
// and resets the transient field. This guarantees CycleCommits[i] is the
// final SHA for cycle i+1. A no-op when no commit was recorded.
func (l *Loop) sealCycleSHA(state *CycleState) {
	if state.lastCycleSHA != "" {
		state.CycleCommits = append(state.CycleCommits, state.lastCycleSHA)
		state.lastCycleSHA = ""
	}
}

// finalCommitSHA returns the last sealed cycle SHA, falling back to a fresh
// HeadSHA call if CycleCommits is empty (e.g. no commits were made).
func (l *Loop) finalCommitSHA(ctx context.Context, state *CycleState) string {
	if n := len(state.CycleCommits); n > 0 {
		return state.CycleCommits[n-1]
	}
	if l.Git != nil {
		sha, err := l.Git.HeadSHA(ctx)
		if err != nil {
			l.UI.Error(fmt.Sprintf("failed to capture final commit SHA: %v", err))
			return ""
		}
		return sha
	}
	return ""
}
