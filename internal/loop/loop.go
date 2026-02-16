package loop

import (
	"context"
	"fmt"
	"strings"

	"github.com/aaronsalm/quasar/internal/agent"
	"github.com/aaronsalm/quasar/internal/beads"
	"github.com/aaronsalm/quasar/internal/ui"
)

// Loop orchestrates the coder-reviewer cycle for a single task.
type Loop struct {
	Invoker      agent.Invoker
	Beads        beads.Client
	UI           ui.UI
	MaxCycles    int
	MaxBudgetUSD float64
	Model        string
	CoderPrompt  string
	ReviewPrompt string
	WorkDir      string
	MCP          *agent.MCPConfig // Optional MCP server config passed to agents.
}

// TaskResult holds the outcome of a completed task loop.
type TaskResult struct {
	TotalCostUSD float64
	CyclesUsed   int
	Report       *ReviewReport // From final reviewer cycle (may be nil)
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

		l.UI.IssuesFound(len(state.Findings))
		state.Phase = PhaseResolvingIssues
		state.ChildBeadIDs = l.createFindingBeads(ctx, state)

		if err := l.Beads.Update(ctx, beadID, beads.UpdateOpts{Assignee: "quasar-coder"}); err != nil {
			l.UI.Error(fmt.Sprintf("failed to update bead: %v", err))
		}
	}

	l.UI.MaxCyclesReached(l.MaxCycles)
	if err := l.Beads.AddComment(ctx, beadID, fmt.Sprintf("Max cycles reached (%d). Manual review recommended.", l.MaxCycles)); err != nil {
		l.UI.Error(fmt.Sprintf("failed to add bead comment: %v", err))
	}
	return nil, ErrMaxCycles
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
	if err := l.Beads.Update(ctx, beadID, beads.UpdateOpts{Status: "in_progress", Assignee: "quasar-coder"}); err != nil {
		l.UI.Error(fmt.Sprintf("failed to update bead: %v", err))
	}
	return &CycleState{
		TaskBeadID:   beadID,
		TaskTitle:    taskDescription,
		Phase:        PhaseBeadCreated,
		MaxCycles:    l.MaxCycles,
		MaxBudgetUSD: l.MaxBudgetUSD,
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
func (l *Loop) runCoderPhase(ctx context.Context, state *CycleState, perAgentBudget float64) error {
	state.Phase = PhaseCoding
	l.UI.AgentStart("coder")

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

	if err := l.Beads.AddComment(ctx, state.TaskBeadID,
		fmt.Sprintf("[coder cycle %d]\n%s", state.Cycle, truncate(result.ResultText, 2000))); err != nil {
		l.UI.Error(fmt.Sprintf("failed to add bead comment: %v", err))
	}
	return nil
}

// runReviewerPhase invokes the reviewer agent, updates state and UI, parses
// findings, and records a bead comment.
func (l *Loop) runReviewerPhase(ctx context.Context, state *CycleState, perAgentBudget float64) error {
	state.Phase = PhaseReviewing
	l.UI.AgentStart("reviewer")

	if err := l.Beads.Update(ctx, state.TaskBeadID, beads.UpdateOpts{Assignee: "quasar-reviewer"}); err != nil {
		l.UI.Error(fmt.Sprintf("failed to update bead: %v", err))
	}

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
	if err := l.Beads.AddComment(ctx, state.TaskBeadID,
		fmt.Sprintf("[reviewer cycle %d]\n%s", state.Cycle, truncate(result.ResultText, 2000))); err != nil {
		l.UI.Error(fmt.Sprintf("failed to add bead comment: %v", err))
	}
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
	if err := l.Beads.AddComment(ctx, state.TaskBeadID,
		fmt.Sprintf("Budget exceeded: $%.4f / $%.2f", state.TotalCostUSD, l.MaxBudgetUSD)); err != nil {
		l.UI.Error(fmt.Sprintf("failed to add bead comment: %v", err))
	}
	return ErrBudgetExceeded
}

// handleApproval closes the bead, records the review report, and returns the final result.
func (l *Loop) handleApproval(ctx context.Context, state *CycleState) (*TaskResult, error) {
	state.Phase = PhaseApproved
	l.UI.Approved()

	report := ParseReviewReport(state.ReviewOutput)
	if err := l.Beads.Close(ctx, state.TaskBeadID, "Approved by reviewer"); err != nil {
		l.UI.Error(fmt.Sprintf("failed to close bead: %v", err))
	}
	if report != nil {
		if err := l.Beads.AddComment(ctx, state.TaskBeadID, FormatReportComment(report)); err != nil {
			l.UI.Error(fmt.Sprintf("failed to add bead comment: %v", err))
		}
	}

	l.UI.TaskComplete(state.TaskBeadID, state.TotalCostUSD)
	return &TaskResult{
		TotalCostUSD: state.TotalCostUSD,
		CyclesUsed:   state.Cycle,
		Report:       report,
	}, nil
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

func (l *Loop) buildCoderPrompt(state *CycleState) string {
	var b strings.Builder

	if state.Cycle == 1 {
		fmt.Fprintf(&b, "Task (bead %s): %s\n\n", state.TaskBeadID, state.TaskTitle)
		b.WriteString("Implement this task. Read existing code first to understand the codebase, then make the necessary changes.")
	} else {
		fmt.Fprintf(&b, "Task (bead %s): %s\n\n", state.TaskBeadID, state.TaskTitle)
		b.WriteString("The reviewer found issues with your previous implementation. Please address them:\n\n")
		for i, f := range state.Findings {
			fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, f.Severity, f.Description)
		}
		b.WriteString("\nFix these issues. Read the relevant files to understand current state before making changes.")
	}

	return b.String()
}

func (l *Loop) buildReviewerPrompt(state *CycleState) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Task (bead %s): %s\n\n", state.TaskBeadID, state.TaskTitle)
	b.WriteString("The coder has completed their work. Here is their summary:\n\n")
	b.WriteString(truncate(state.CoderOutput, 3000))
	b.WriteString("\n\nREVIEW INSTRUCTIONS:\n")
	b.WriteString("1. READ THE ACTUAL SOURCE FILES to verify the changes — do not rely solely on the summary above.\n")
	b.WriteString("2. Check for correctness, security, error handling, code quality, and edge cases.\n")
	b.WriteString("3. End your review with either APPROVED: or one or more ISSUE: blocks.\n")

	return b.String()
}

// ParseReviewFindings scans reviewer output for structured ISSUE: blocks.
func ParseReviewFindings(output string) []ReviewFinding {
	var findings []ReviewFinding
	lines := strings.Split(output, "\n")
	for i := 0; i < len(lines); {
		if strings.TrimSpace(lines[i]) == "ISSUE:" {
			f, next := parseIssueBlock(lines, i+1)
			if f.Description != "" {
				if f.Severity == "" {
					f.Severity = "major"
				}
				findings = append(findings, f)
			}
			i = next
			continue
		}
		i++
	}
	return findings
}

// parseIssueBlock parses a single ISSUE: block starting at index start.
// It returns the parsed finding and the index to resume scanning from.
func parseIssueBlock(lines []string, start int) (ReviewFinding, int) {
	f := ReviewFinding{}
	i := start
	for i < len(lines) {
		inner := strings.TrimSpace(lines[i])
		if inner == "" || inner == "ISSUE:" {
			break
		}
		switch {
		case strings.HasPrefix(inner, "SEVERITY:"):
			f.Severity = strings.TrimSpace(strings.TrimPrefix(inner, "SEVERITY:"))
			i++
		case strings.HasPrefix(inner, "DESCRIPTION:"):
			f.Description = strings.TrimSpace(strings.TrimPrefix(inner, "DESCRIPTION:"))
			i++
			i = collectContinuationLines(&f, lines, i)
		default:
			i++
		}
	}
	return f, i
}

// collectContinuationLines appends subsequent non-field lines to the finding's
// description. It returns the index of the first non-continuation line.
func collectContinuationLines(f *ReviewFinding, lines []string, start int) int {
	i := start
	for i < len(lines) {
		cont := strings.TrimSpace(lines[i])
		if cont == "" || cont == "ISSUE:" || strings.HasPrefix(cont, "SEVERITY:") || strings.HasPrefix(cont, "APPROVED:") {
			break
		}
		f.Description += " " + cont
		i++
	}
	return i
}

func isApproved(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "APPROVED:") {
			return true
		}
	}
	return false
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... [truncated]"
}
