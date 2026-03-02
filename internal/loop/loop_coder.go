package loop

import (
	"context"
	"fmt"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/filter"
)

// DefaultMaxFilterFixes is the maximum number of inner fix attempts per
// filter failure before falling through to the outer coder-reviewer cycle.
const DefaultMaxFilterFixes = 3

// SingleCheckRunner can execute a single named check. Defined in the loop
// package where consumed, satisfied by *filter.Chain.
type SingleCheckRunner interface {
	RunCheck(ctx context.Context, workDir string, name string) (*filter.CheckResult, error)
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
		MCP: l.MCP,
	}
}

// runCoderPhase invokes the coder agent, updates state and UI, and emits
// lifecycle events. When a refactor is pending, it emits a refactor event
// before building the prompt (which clears the refactor flag).
func (l *Loop) runCoderPhase(ctx context.Context, state *CycleState, perAgentBudget float64) error {
	state.Phase = PhaseCoding
	l.UI.AgentStart("coder")

	// Capture refactor state before buildCoderPrompt clears the flag.
	wasRefactored := state.Refactored
	origDesc := state.OriginalDescription
	refactorDesc := state.RefactorDescription

	prompt := l.buildCoderPrompt(state)
	relayBlock, relayIDs := l.pendingHailRelay()
	if relayBlock != "" {
		prompt = relayBlock + "\n" + prompt
	}
	prompt = l.composeVolatilePrefix(ctx, prompt)

	result, err := l.Invoker.Invoke(ctx, l.coderAgent(perAgentBudget), prompt, l.WorkDir)
	if err != nil {
		state.Phase = PhaseError
		return fmt.Errorf("coder invocation failed: %w", err)
	}

	state.CoderOutput = result.ResultText
	state.TotalCostUSD += result.CostUSD
	state.Phase = PhaseCodeComplete
	l.UI.AgentOutput("coder", state.Cycle, result.ResultText)
	l.UI.AgentDone("coder", result.CostUSD, result.DurationMs)
	l.emitCycleSummary(state, PhaseCodeComplete, result)
	l.markHailsRelayed(relayIDs)

	// Commit the coder's changes for this cycle.
	// The SHA is stored in lastCycleSHA and sealed into CycleCommits at cycle end.
	if l.Git != nil {
		summary := l.CommitSummary
		if summary == "" {
			summary = firstLine(state.TaskTitle, 72)
		}
		sha, err := l.Git.CommitCycle(ctx, state.TaskBeadID, state.Cycle, summary)
		if err != nil {
			l.UI.Error(fmt.Sprintf("failed to commit cycle %d: %v", state.Cycle, err))
		} else {
			state.lastCycleSHA = sha
		}
	}

	if wasRefactored {
		comment := fmt.Sprintf("[refactor cycle %d] User updated task description mid-execution.\nOriginal: %s\nUpdated: %s",
			state.Cycle, truncate(origDesc, 500), truncate(refactorDesc, 500))
		l.emit(ctx, Event{Kind: EventRefactored, BeadID: state.TaskBeadID, Cycle: state.Cycle, Message: comment})
		l.UI.RefactorApplied(state.TaskBeadID)
	}
	l.emit(ctx, Event{
		Kind:    EventAgentDone,
		BeadID:  state.TaskBeadID,
		Cycle:   state.Cycle,
		Agent:   "coder",
		Result:  &result,
		Message: fmt.Sprintf("[coder cycle %d]\n%s", state.Cycle, truncate(result.ResultText, 2000)),
	})
	l.trackCacheMetrics(ctx, state, "coder", &result)
	return nil
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
		result, err := l.Invoker.Invoke(ctx, l.coderAgent(perAgentBudget), lintPrompt, l.WorkDir)
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
			sha, commitErr := l.Git.CommitCycle(ctx, state.TaskBeadID, state.Cycle, summary+" (lint fix)")
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
	l.emitBeadUpdate(state, "in_progress")

	return true, nil
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
			Role:         agent.RoleCoder,
			SystemPrompt: sysPrompt,
			Model:        l.Model,
			MaxBudgetUSD: fixBudget,
			AllowedTools: []string{"Read", "Edit", "Write", "Glob"},
			MCP:          l.MCP,
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
			sha, commitErr := l.Git.CommitCycle(ctx, state.TaskBeadID, state.Cycle, summary+" (filter fix)")
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
			BeadID: state.TaskBeadID,
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
				BeadID: state.TaskBeadID,
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
		BeadID: state.TaskBeadID,
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
