package loop

import (
	"context"
	"fmt"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/beads"
	"github.com/papapumpkin/quasar/internal/ui"
)

// Loop orchestrates the coder-reviewer cycle for a single task.
type Loop struct {
	Invoker      agent.Invoker
	Beads        beads.Client
	UI           ui.UI
	Git          CycleCommitter // Optional; nil disables per-cycle commits.
	MaxCycles    int
	MaxBudgetUSD float64
	Model        string
	CoderPrompt  string
	ReviewPrompt string
	WorkDir      string
	MCP          *agent.MCPConfig // Optional MCP server config passed to agents.
	RefactorCh   <-chan string    // Optional channel carrying updated task descriptions from phase edits.
}

// TaskResult holds the outcome of a completed task loop.
type TaskResult struct {
	TotalCostUSD float64
	CyclesUsed   int
	Report       *agent.ReviewReport // From final reviewer cycle (may be nil)
}

// RunTask creates a new bead for the given task and runs the coder-reviewer loop.
func (l *Loop) RunTask(ctx context.Context, taskDescription string) (*TaskResult, error) {
	// Create task bead.
	beadID, err := l.Beads.Create(ctx, taskDescription, beads.CreateOpts{
		Type:        "task",
		Labels:      []string{"quasar"},
		Description: taskDescription,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create task bead: %w", err)
	}
	return l.runLoop(ctx, beadID, taskDescription)
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
		if err := l.runReviewerPhase(ctx, state, perAgentBudget); err != nil {
			return nil, err
		}
		if err := l.checkBudget(ctx, state); err != nil {
			return nil, err
		}

		if isApproved(state.ReviewOutput) {
			return l.handleApproval(ctx, state)
		}

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

		l.beadUpdate(ctx, beadID, beads.UpdateOpts{Assignee: "quasar-coder"})
	}

	l.UI.MaxCyclesReached(l.MaxCycles)
	l.beadComment(ctx, beadID, fmt.Sprintf("Max cycles reached (%d). Manual review recommended.", l.MaxCycles))
	return nil, ErrMaxCycles
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

// initCycleState creates the initial cycle state and marks the bead as in-progress.
func (l *Loop) initCycleState(ctx context.Context, beadID, taskDescription string) *CycleState {
	l.UI.TaskStarted(beadID, taskDescription)
	l.beadUpdate(ctx, beadID, beads.UpdateOpts{Status: "in_progress", Assignee: "quasar-coder"})

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

// runCoderPhase invokes the coder agent, updates state and UI, and records a bead comment.
// When a refactor is pending, it posts a bead comment documenting the change before
// building the prompt (which clears the refactor flag).
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
	if l.Git != nil {
		sha, err := l.Git.CommitCycle(ctx, state.TaskBeadID, state.Cycle)
		if err != nil {
			l.UI.Error(fmt.Sprintf("failed to commit cycle %d: %v", state.Cycle, err))
		} else {
			state.CycleCommits = append(state.CycleCommits, sha)
		}
	}

	if wasRefactored {
		comment := fmt.Sprintf("[refactor cycle %d] User updated task description mid-execution.\nOriginal: %s\nUpdated: %s",
			state.Cycle, truncate(origDesc, 500), truncate(refactorDesc, 500))
		l.beadComment(ctx, state.TaskBeadID, comment)
		l.UI.RefactorApplied(state.TaskBeadID)
	}
	l.beadComment(ctx, state.TaskBeadID,
		fmt.Sprintf("[coder cycle %d]\n%s", state.Cycle, truncate(result.ResultText, 2000)))
	return nil
}

// runReviewerPhase invokes the reviewer agent, updates state and UI, parses
// findings, and records a bead comment.
func (l *Loop) runReviewerPhase(ctx context.Context, state *CycleState, perAgentBudget float64) error {
	state.Phase = PhaseReviewing
	l.UI.AgentStart("reviewer")

	l.beadUpdate(ctx, state.TaskBeadID, beads.UpdateOpts{Assignee: "quasar-reviewer"})

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
	l.beadComment(ctx, state.TaskBeadID,
		fmt.Sprintf("[reviewer cycle %d]\n%s", state.Cycle, truncate(result.ResultText, 2000)))
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
	l.beadComment(ctx, state.TaskBeadID,
		fmt.Sprintf("Budget exceeded: $%.4f / $%.2f", state.TotalCostUSD, l.MaxBudgetUSD))
	return ErrBudgetExceeded
}

// handleApproval closes the bead, records the review report, and returns the final result.
func (l *Loop) handleApproval(ctx context.Context, state *CycleState) (*TaskResult, error) {
	state.Phase = PhaseApproved
	l.UI.Approved()

	report := ParseReviewReport(state.ReviewOutput)
	l.beadClose(ctx, state.TaskBeadID, "Approved by reviewer")
	l.emitBeadUpdate(state, "closed")
	if report != nil {
		l.beadComment(ctx, state.TaskBeadID, FormatReportComment(report))
	}

	l.UI.TaskComplete(state.TaskBeadID, state.TotalCostUSD)
	return &TaskResult{
		TotalCostUSD: state.TotalCostUSD,
		CyclesUsed:   state.Cycle,
		Report:       report,
	}, nil
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
			title = truncate(state.AllFindings[i].Description, 80)
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

// beadComment logs a comment on the task bead, logging any error.
func (l *Loop) beadComment(ctx context.Context, beadID, body string) {
	if err := l.Beads.AddComment(ctx, beadID, body); err != nil {
		l.UI.Error(fmt.Sprintf("failed to add bead comment: %v", err))
	}
}

// beadUpdate updates the task bead, logging any error.
func (l *Loop) beadUpdate(ctx context.Context, beadID string, opts beads.UpdateOpts) {
	if err := l.Beads.Update(ctx, beadID, opts); err != nil {
		l.UI.Error(fmt.Sprintf("failed to update bead: %v", err))
	}
}

// beadClose closes the task bead with a reason, logging any error.
func (l *Loop) beadClose(ctx context.Context, beadID, reason string) {
	if err := l.Beads.Close(ctx, beadID, reason); err != nil {
		l.UI.Error(fmt.Sprintf("failed to close bead: %v", err))
	}
}

// createFindingBeads creates a child bead for each review finding and returns
// the IDs of successfully created beads.
func (l *Loop) createFindingBeads(ctx context.Context, state *CycleState) []string {
	var ids []string
	for _, f := range state.Findings {
		childID, err := l.Beads.Create(ctx,
			fmt.Sprintf("[%s] %s", f.Severity, truncate(f.Description, 80)),
			beads.CreateOpts{
				Type:        "bug",
				Labels:      []string{"quasar", "review-finding"},
				Parent:      state.TaskBeadID,
				Description: f.Description,
			},
		)
		if err != nil {
			l.UI.Error(fmt.Sprintf("failed to create child bead: %v", err))
			continue
		}
		ids = append(ids, childID)
	}
	return ids
}
