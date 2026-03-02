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
	Hooks             []Hook         // Lifecycle hooks (e.g., BeadHook for tracking).
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
	RefactorCh        <-chan string    // Optional channel carrying updated task descriptions from phase edits.
	CommitSummary     string           // Short label for cycle commit messages. If empty, derived from task title.
	Fabric            fabric.Fabric    // Optional; when set and FabricEnabled, auto-inject fabric state into prompts.
	FabricEnabled     bool             // When true, inject fabric protocol into agent system prompts.
	TaskID            string           // Task ID for fabric context (QUASAR_TASK_ID).
	ProjectContext    string           // Injected into agent system prompts for prompt caching.
	MaxContextTokens  int              // Token budget for context injection. 0 = use default.
	CacheOptimization bool             // When true, system prompts use a stable prefix for cache hits.
	CacheVerbose      bool             // When true, log cache-related diagnostics to stderr.
	HailQueue         HailQueue        // Optional; when set, hails extracted during execution are posted here.
	HailTimeout       time.Duration    // Auto-resolve timeout for hails. 0 disables auto-resolution.
	StruggleConfig    StruggleConfig   // Optional; zero value disables struggle detection.
	CheckpointDir     string           // Directory for checkpoint files. Empty disables checkpointing.
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

// RunTask creates a new bead for the given task and runs the coder-reviewer loop.
// Bead creation is delegated to hooks that implement TaskCreator.
func (l *Loop) RunTask(ctx context.Context, taskDescription string) (*TaskResult, error) {
	beadID, err := l.createTask(ctx, taskDescription)
	if err != nil {
		return nil, fmt.Errorf("failed to create task bead: %w", err)
	}
	return l.runLoop(ctx, beadID, taskDescription)
}

// createTask delegates task bead creation to the first hook implementing TaskCreator.
// Returns an error if no hook provides the capability.
func (l *Loop) createTask(ctx context.Context, description string) (string, error) {
	for _, h := range l.Hooks {
		if tc, ok := h.(TaskCreator); ok {
			return tc.CreateTask(ctx, description)
		}
	}
	return "", fmt.Errorf("no TaskCreator hook registered")
}

// RunExistingTask runs the coder-reviewer loop for an already-created bead.
func (l *Loop) RunExistingTask(ctx context.Context, beadID, taskDescription string) (*TaskResult, error) {
	return l.runLoop(ctx, beadID, taskDescription)
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
		BeadID:  cs.TaskBeadID,
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
	l.UI.TaskStarted(cs.TaskBeadID, cs.TaskTitle)
	l.emitBeadUpdate(cs, "in_progress")

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
					l.drainRefactor(cs)
					l.emit(ctx, Event{Kind: EventCycleStart, BeadID: cs.TaskBeadID, Cycle: cycle})
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
					l.drainRefactor(cs)
					l.emit(ctx, Event{Kind: EventCycleStart, BeadID: cs.TaskBeadID, Cycle: cycle})
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

		l.extractAndPostHails(ctx, cs)

		if isApproved(cs.ReviewOutput) {
			return l.handleApproval(ctx, cs)
		}

		cs.FilterHistory = append(cs.FilterHistory, cs.FilterCheckName)
		l.sealCycleSHA(cs)
		l.drainRefactor(cs)

		l.UI.IssuesFound(len(cs.Findings))
		cs.Phase = PhaseResolvingIssues
		for i := range cs.Findings {
			cs.Findings[i].Cycle = cs.Cycle
		}
		newChildIDs := l.createFindingBeads(ctx, cs)
		cs.ChildBeadIDs = append(cs.ChildBeadIDs, newChildIDs...)
		cs.AllFindings = append(cs.AllFindings, cs.Findings...)
		l.emitBeadUpdate(cs, "in_progress")

		if l.StruggleConfig.Enabled {
			signal := EvaluateStruggle(cs, l.StruggleConfig)
			if signal.Triggered {
				l.emit(ctx, Event{
					Kind:    EventStruggleDetected,
					BeadID:  cs.TaskBeadID,
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

		l.emit(ctx, Event{Kind: EventCycleStart, BeadID: cs.TaskBeadID, Cycle: cycle})
	}

	l.UI.MaxCyclesReached(l.MaxCycles)
	l.postMaxCyclesHail(cs)
	l.emit(ctx, Event{
		Kind:    EventTaskFailed,
		BeadID:  cs.TaskBeadID,
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
func (l *Loop) GenerateCheckpoint(ctx context.Context, beadID, taskDescription string) (string, error) {
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
		beadID, taskDescription,
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
func (l *Loop) runLoop(ctx context.Context, beadID, taskDescription string) (*TaskResult, error) {
	// Pre-compute system prompts once for the entire phase. These contain
	// only stable content (ProjectContext + basePrompt + FabricProtocol)
	// and must remain byte-identical across all cycles for prompt cache hits.
	// When CacheOptimization is disabled, skip pre-computation so prompts
	// are rebuilt per cycle (legacy behavior).
	if l.CacheOptimization {
		l.cacheSystemPrompts()
	}

	perAgentBudget := l.perAgentBudget()
	state := l.initCycleState(ctx, beadID, taskDescription)

	// Auto-wire checkpoint hook when CheckpointDir is set and a factory
	// is provided. The hook is prepended so it runs before other hooks.
	l.wireCheckpointHook(func() *CycleState { return state })

	l.emitBeadUpdate(state, "in_progress")

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
					l.drainRefactor(state)
					l.emit(ctx, Event{Kind: EventCycleStart, BeadID: beadID, Cycle: cycle})
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
					l.drainRefactor(state)
					l.emit(ctx, Event{Kind: EventCycleStart, BeadID: beadID, Cycle: cycle})
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

		// Extract hails from the reviewer's report and any fabric discoveries.
		l.extractAndPostHails(ctx, state)

		if isApproved(state.ReviewOutput) {
			return l.handleApproval(ctx, state)
		}

		// Record this cycle's filter check name (empty if filter passed or was nil).
		state.FilterHistory = append(state.FilterHistory, state.FilterCheckName)

		// Seal the cycle's final SHA into CycleCommits before moving on.
		l.sealCycleSHA(state)

		// Check for a mid-run refactor signal before starting the next cycle.
		l.drainRefactor(state)

		l.UI.IssuesFound(len(state.Findings))
		state.Phase = PhaseResolvingIssues
		// Tag findings with the current cycle number before creating beads
		// or accumulating, so the Cycle field is available downstream.
		for i := range state.Findings {
			state.Findings[i].Cycle = state.Cycle
		}
		newChildIDs := l.createFindingBeads(ctx, state)
		state.ChildBeadIDs = append(state.ChildBeadIDs, newChildIDs...)
		state.AllFindings = append(state.AllFindings, state.Findings...)
		l.emitBeadUpdate(state, "in_progress")

		// Evaluate struggle detection after findings are accumulated.
		if l.StruggleConfig.Enabled {
			signal := EvaluateStruggle(state, l.StruggleConfig)
			if signal.Triggered {
				l.emit(ctx, Event{
					Kind:    EventStruggleDetected,
					BeadID:  beadID,
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

		l.emit(ctx, Event{Kind: EventCycleStart, BeadID: beadID, Cycle: cycle})
	}

	l.UI.MaxCyclesReached(l.MaxCycles)
	l.postMaxCyclesHail(state)
	l.emit(ctx, Event{
		Kind:    EventTaskFailed,
		BeadID:  beadID,
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

// perAgentBudget computes the per-invocation budget by splitting the total
// evenly between coder and reviewer across all cycles.
func (l *Loop) perAgentBudget() float64 {
	if l.MaxBudgetUSD <= 0 {
		return 0
	}
	return l.MaxBudgetUSD / float64(2*l.MaxCycles)
}

// initCycleState creates the initial cycle state and emits task-started events.
func (l *Loop) initCycleState(ctx context.Context, beadID, taskDescription string) *CycleState {
	l.UI.TaskStarted(beadID, taskDescription)
	l.emit(ctx, Event{Kind: EventCycleStart, BeadID: beadID})

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
		TaskBeadID:    beadID,
		TaskTitle:     taskDescription,
		Phase:         PhaseBeadCreated,
		MaxCycles:     l.MaxCycles,
		MaxBudgetUSD:  l.MaxBudgetUSD,
		BaseCommitSHA: baseSHA,
	}
}

// checkBudget returns ErrBudgetExceeded if the total cost has reached the limit.
func (l *Loop) checkBudget(ctx context.Context, state *CycleState) error {
	if l.MaxBudgetUSD <= 0 || state.TotalCostUSD < l.MaxBudgetUSD {
		return nil
	}
	l.UI.BudgetExceeded(state.TotalCostUSD, l.MaxBudgetUSD)
	l.emit(ctx, Event{
		Kind:    EventTaskFailed,
		BeadID:  state.TaskBeadID,
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
		BeadID: state.TaskBeadID,
		Cycle:  state.Cycle,
		Report: report,
	})
	l.emitBeadUpdate(state, "closed")

	l.UI.TaskComplete(state.TaskBeadID, state.TotalCostUSD)
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
		BeadID: state.TaskBeadID,
		Cycle:  state.Cycle,
		Agent:  agentRole,
		Result: result,
		Message: fmt.Sprintf("sys_prompt_hash=%s sys_prompt_len=%d user_prompt_len=%d cost=%.4f",
			hash, result.SystemPromptLen, result.UserPromptLen, result.CostUSD),
	})
}

// drainRefactor checks the RefactorCh for a pending phase edit and applies it
// to the cycle state. The current cycle always completes before the new
// description takes effect. Only the most recent value on the channel wins.
func (l *Loop) drainRefactor(state *CycleState) {
	if l.RefactorCh == nil {
		return
	}
	var latest string
	for {
		select {
		case body := <-l.RefactorCh:
			latest = body
		default:
			if latest != "" {
				state.OriginalDescription = state.TaskTitle
				state.RefactorDescription = latest
				state.TaskTitle = latest
				state.Refactored = true
			}
			return
		}
	}
}
