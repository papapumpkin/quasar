package loop

import (
	"context"
	"fmt"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/filter"
	"github.com/papapumpkin/quasar/internal/ui"
)

// Loop orchestrates the coder-reviewer cycle for a single task.
type Loop struct {
	Invoker        agent.Invoker
	UI             ui.UI
	Git            CycleCommitter // Optional; nil disables per-cycle commits.
	Hooks          []Hook         // Lifecycle hooks (e.g., BeadHook for tracking).
	Linter         Linter         // Optional; nil disables lint checks between coder and reviewer.
	Filter         filter.Filter  // Optional; nil skips pre-reviewer filtering and goes straight to reviewer.
	MaxCycles      int
	MaxLintRetries int // Max times coder is asked to fix lint issues per cycle. 0 uses DefaultMaxLintRetries.
	MaxBudgetUSD   float64
	Model          string
	CoderPrompt    string
	ReviewPrompt   string
	WorkDir        string
	MCP            *agent.MCPConfig // Optional MCP server config passed to agents.
	RefactorCh     <-chan string    // Optional channel carrying updated task descriptions from phase edits.
	CommitSummary  string           // Short label for cycle commit messages. If empty, derived from task title.
}

// TaskResult holds the outcome of a completed task loop.
type TaskResult struct {
	TotalCostUSD   float64
	CyclesUsed     int
	Report         *agent.ReviewReport // From final reviewer cycle (may be nil)
	BaseCommitSHA  string              // HEAD captured at task start
	FinalCommitSHA string              // last cycle's sealed SHA (or current HEAD as fallback)
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
	perAgentBudget := l.perAgentBudget()
	state := l.initCycleState(ctx, beadID, taskDescription)
	l.emitBeadUpdate(state, "in_progress")

	for cycle := 1; cycle <= l.MaxCycles; cycle++ {
		state.Cycle = cycle
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

		// Run pre-reviewer filter checks. If the filter fails, bounce
		// the failure back to the coder as findings instead of invoking
		// the reviewer.
		if l.Filter != nil {
			failed, err := l.runFilterChecks(ctx, state)
			if err != nil {
				return nil, err
			}
			if failed {
				// Filter failed — skip reviewer, continue to next cycle.
				l.sealCycleSHA(state)
				l.drainRefactor(state)
				l.emit(ctx, Event{Kind: EventCycleStart, BeadID: beadID, Cycle: cycle})
				continue
			}
		}

		if err := l.runReviewerPhase(ctx, state, perAgentBudget); err != nil {
			return nil, err
		}
		if err := l.checkBudget(ctx, state); err != nil {
			return nil, err
		}

		if isApproved(state.ReviewOutput) {
			return l.handleApproval(ctx, state)
		}

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

		l.emit(ctx, Event{Kind: EventCycleStart, BeadID: beadID, Cycle: cycle})
	}

	l.UI.MaxCyclesReached(l.MaxCycles)
	l.emit(ctx, Event{
		Kind:    EventTaskFailed,
		BeadID:  beadID,
		Message: fmt.Sprintf("Max cycles reached (%d). Manual review recommended.", l.MaxCycles),
	})
	return &TaskResult{
		TotalCostUSD:   state.TotalCostUSD,
		CyclesUsed:     state.Cycle,
		BaseCommitSHA:  state.BaseCommitSHA,
		FinalCommitSHA: l.finalCommitSHA(ctx, state),
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
		// Infrastructure error (e.g. context cancelled) is fatal.
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

// coderAgent builds the agent configuration for the coder role.
func (l *Loop) coderAgent(budget float64) agent.Agent {
	return agent.Agent{
		Role:         agent.RoleCoder,
		SystemPrompt: l.CoderPrompt,
		Model:        l.Model,
		MaxBudgetUSD: budget,
		AllowedTools: []string{
			"Read", "Edit", "Write", "Glob", "Grep",
			"Bash(go *)", "Bash(git diff *)", "Bash(git status)", "Bash(git log *)",
		},
		MCP: l.MCP,
	}
}

// reviewerAgent builds the agent configuration for the reviewer role.
func (l *Loop) reviewerAgent(budget float64) agent.Agent {
	return agent.Agent{
		Role:         agent.RoleReviewer,
		SystemPrompt: l.ReviewPrompt,
		Model:        l.Model,
		MaxBudgetUSD: budget,
		AllowedTools: []string{
			"Read", "Glob", "Grep",
			"Bash(go vet *)", "Bash(git diff *)", "Bash(git log *)",
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

	result, err := l.Invoker.Invoke(ctx, l.coderAgent(perAgentBudget), l.buildCoderPrompt(state), l.WorkDir)
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
	return nil
}

// runReviewerPhase invokes the reviewer agent, updates state and UI, parses
// findings, and emits lifecycle events.
func (l *Loop) runReviewerPhase(ctx context.Context, state *CycleState, perAgentBudget float64) error {
	state.Phase = PhaseReviewing
	l.UI.AgentStart("reviewer")

	result, err := l.Invoker.Invoke(ctx, l.reviewerAgent(perAgentBudget), l.buildReviewerPrompt(state), l.WorkDir)
	if err != nil {
		state.Phase = PhaseError
		return fmt.Errorf("reviewer invocation failed: %w", err)
	}

	state.ReviewOutput = result.ResultText
	state.TotalCostUSD += result.CostUSD
	state.Phase = PhaseReviewComplete
	l.UI.AgentOutput("reviewer", state.Cycle, result.ResultText)
	l.UI.AgentDone("reviewer", result.CostUSD, result.DurationMs)
	l.emit(ctx, Event{
		Kind:    EventAgentDone,
		BeadID:  state.TaskBeadID,
		Cycle:   state.Cycle,
		Agent:   "reviewer",
		Result:  &result,
		Message: fmt.Sprintf("[reviewer cycle %d]\n%s", state.Cycle, truncate(result.ResultText, 2000)),
	})
	state.Findings = ParseReviewFindings(result.ResultText)
	l.emitCycleSummary(state, PhaseReviewComplete, result)
	return nil
}

// emitCycleSummary sends a cycle summary to the UI for the given phase.
func (l *Loop) emitCycleSummary(state *CycleState, phase Phase, result agent.InvocationResult) {
	l.UI.CycleSummary(ui.CycleSummaryData{
		Cycle:        state.Cycle,
		MaxCycles:    l.MaxCycles,
		Phase:        phase.String(),
		CostUSD:      result.CostUSD,
		TotalCostUSD: state.TotalCostUSD,
		MaxBudgetUSD: l.MaxBudgetUSD,
		DurationMs:   result.DurationMs,
		Approved:     isApproved(state.ReviewOutput),
		IssueCount:   len(state.Findings),
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
		TotalCostUSD:   state.TotalCostUSD,
		CyclesUsed:     state.Cycle,
		Report:         report,
		BaseCommitSHA:  state.BaseCommitSHA,
		FinalCommitSHA: l.finalCommitSHA(ctx, state),
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

// emitBeadUpdate sends the current bead hierarchy to the UI.
// It uses AllFindings (accumulated across cycles) to match ChildBeadIDs,
// so that children from earlier cycles are preserved in the hierarchy.
// When the parent task is closed (approved), all children are marked closed
// since we don't track per-child status independently.
func (l *Loop) emitBeadUpdate(state *CycleState, status string) {
	// When the task is closed, all child issues are considered resolved.
	childStatus := "open"
	if status == "closed" {
		childStatus = "closed"
	}
	var children []ui.BeadChild
	for i, id := range state.ChildBeadIDs {
		title := "review finding"
		severity := "major"
		cycle := 0
		if i < len(state.AllFindings) {
			title = firstLine(state.AllFindings[i].Description, 80)
			severity = state.AllFindings[i].Severity
			cycle = state.AllFindings[i].Cycle
		}
		children = append(children, ui.BeadChild{
			ID:       id,
			Title:    title,
			Status:   childStatus,
			Severity: severity,
			Cycle:    cycle,
		})
	}
	l.UI.BeadUpdate(state.TaskBeadID, state.TaskTitle, status, children)
}

// createFindingBeads delegates to hooks that implement FindingCreator to
// create child beads for each review finding. Returns the IDs of
// successfully created beads.
func (l *Loop) createFindingBeads(ctx context.Context, state *CycleState) []string {
	var ids []string
	for _, h := range l.Hooks {
		if fc, ok := h.(FindingCreator); ok {
			ids = append(ids, fc.CreateFindingChildIDs(ctx, state.TaskBeadID, state.Findings)...)
		}
	}
	return ids
}
