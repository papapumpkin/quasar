package loop

import (
	"context"
	"fmt"
	"strings"

	"github.com/aaronsalm/quasar/internal/agent"
	"github.com/aaronsalm/quasar/internal/beads"
	"github.com/aaronsalm/quasar/internal/ui"
)

type Loop struct {
	Invoker      agent.Invoker
	Beads        beads.BeadsClient
	UI           *ui.Printer
	MaxCycles    int
	MaxBudgetUSD float64
	Model        string
	CoderPrompt  string
	ReviewPrompt string
	WorkDir      string
}

// TaskResult holds the outcome of a completed task loop.
type TaskResult struct {
	TotalCostUSD float64
	CyclesUsed   int
	Report       *ReviewReport // From final reviewer cycle (may be nil)
}

func (l *Loop) RunTask(ctx context.Context, taskDescription string) (*TaskResult, error) {
	// Create task bead.
	beadID, err := l.Beads.Create(taskDescription, beads.CreateOpts{
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
	// Compute per-agent budget: split total evenly between coder and reviewer
	// across all cycles.
	perAgentBudget := 0.0
	if l.MaxBudgetUSD > 0 {
		perAgentBudget = l.MaxBudgetUSD / float64(2*l.MaxCycles)
	}

	state := &CycleState{
		TaskBeadID:   beadID,
		TaskTitle:    taskDescription,
		Phase:        PhaseBeadCreated,
		MaxCycles:    l.MaxCycles,
		MaxBudgetUSD: l.MaxBudgetUSD,
	}

	l.UI.TaskStarted(beadID, taskDescription)

	if err := l.Beads.Update(beadID, beads.UpdateOpts{Status: "in_progress", Assignee: "quasar-coder"}); err != nil {
		l.UI.Error(fmt.Sprintf("failed to update bead: %v", err))
	}

	for cycle := 1; cycle <= l.MaxCycles; cycle++ {
		state.Cycle = cycle
		l.UI.CycleStart(cycle, l.MaxCycles)

		// --- Coder phase ---
		state.Phase = PhaseCoding
		coderAgent := agent.Agent{
			Role:         agent.RoleCoder,
			SystemPrompt: l.CoderPrompt,
			Model:        l.Model,
			MaxBudgetUSD: perAgentBudget,
		}

		coderPrompt := l.buildCoderPrompt(state)
		l.UI.AgentStart("coder")

		coderResult, err := l.Invoker.Invoke(ctx, coderAgent, coderPrompt, l.WorkDir)
		if err != nil {
			state.Phase = PhaseError
			return nil, fmt.Errorf("coder invocation failed: %w", err)
		}

		state.CoderOutput = coderResult.ResultText
		state.TotalCostUSD += coderResult.CostUSD
		state.Phase = PhaseCodeComplete
		l.UI.AgentDone("coder", coderResult.CostUSD, coderResult.DurationMs)

		// Record coder output as bead comment.
		_ = l.Beads.AddComment(beadID, fmt.Sprintf("[coder cycle %d]\n%s", cycle, truncate(coderResult.ResultText, 2000)))

		// Budget check.
		if l.MaxBudgetUSD > 0 && state.TotalCostUSD >= l.MaxBudgetUSD {
			l.UI.BudgetExceeded(state.TotalCostUSD, l.MaxBudgetUSD)
			_ = l.Beads.AddComment(beadID, fmt.Sprintf("Budget exceeded: $%.4f / $%.2f", state.TotalCostUSD, l.MaxBudgetUSD))
			return nil, ErrBudgetExceeded
		}

		// --- Reviewer phase ---
		state.Phase = PhaseReviewing
		reviewerAgent := agent.Agent{
			Role:         agent.RoleReviewer,
			SystemPrompt: l.ReviewPrompt,
			Model:        l.Model,
			MaxBudgetUSD: perAgentBudget,
		}

		reviewerPrompt := l.buildReviewerPrompt(state)
		l.UI.AgentStart("reviewer")

		if err := l.Beads.Update(beadID, beads.UpdateOpts{Assignee: "quasar-reviewer"}); err != nil {
			l.UI.Error(fmt.Sprintf("failed to update bead: %v", err))
		}

		reviewResult, err := l.Invoker.Invoke(ctx, reviewerAgent, reviewerPrompt, l.WorkDir)
		if err != nil {
			state.Phase = PhaseError
			return nil, fmt.Errorf("reviewer invocation failed: %w", err)
		}

		state.ReviewOutput = reviewResult.ResultText
		state.TotalCostUSD += reviewResult.CostUSD
		state.Phase = PhaseReviewComplete
		l.UI.AgentDone("reviewer", reviewResult.CostUSD, reviewResult.DurationMs)

		// Record reviewer output as bead comment.
		_ = l.Beads.AddComment(beadID, fmt.Sprintf("[reviewer cycle %d]\n%s", cycle, truncate(reviewResult.ResultText, 2000)))

		// Budget check.
		if l.MaxBudgetUSD > 0 && state.TotalCostUSD >= l.MaxBudgetUSD {
			l.UI.BudgetExceeded(state.TotalCostUSD, l.MaxBudgetUSD)
			_ = l.Beads.AddComment(beadID, fmt.Sprintf("Budget exceeded: $%.4f / $%.2f", state.TotalCostUSD, l.MaxBudgetUSD))
			return nil, ErrBudgetExceeded
		}

		// Parse review findings.
		findings := ParseReviewFindings(reviewResult.ResultText)
		state.Findings = findings

		if isApproved(reviewResult.ResultText) {
			state.Phase = PhaseApproved
			l.UI.Approved()
			report := ParseReviewReport(reviewResult.ResultText)
			_ = l.Beads.Close(beadID, "Approved by reviewer")
			if report != nil {
				_ = l.Beads.AddComment(beadID, FormatReportComment(report))
			}
			l.UI.TaskComplete(beadID, state.TotalCostUSD)
			return &TaskResult{
				TotalCostUSD: state.TotalCostUSD,
				CyclesUsed:   cycle,
				Report:       report,
			}, nil
		}

		// Issues found.
		l.UI.IssuesFound(len(findings))
		state.Phase = PhaseResolvingIssues

		// Create child beads for each issue.
		state.ChildBeadIDs = nil
		for _, f := range findings {
			childID, err := l.Beads.Create(
				fmt.Sprintf("[%s] %s", f.Severity, truncate(f.Description, 80)),
				beads.CreateOpts{
					Type:        "bug",
					Labels:      []string{"quasar", "review-finding"},
					Parent:      beadID,
					Description: f.Description,
				},
			)
			if err != nil {
				l.UI.Error(fmt.Sprintf("failed to create child bead: %v", err))
				continue
			}
			state.ChildBeadIDs = append(state.ChildBeadIDs, childID)
		}

		if err := l.Beads.Update(beadID, beads.UpdateOpts{Assignee: "quasar-coder"}); err != nil {
			l.UI.Error(fmt.Sprintf("failed to update bead: %v", err))
		}
	}

	l.UI.MaxCyclesReached(l.MaxCycles)
	_ = l.Beads.AddComment(beadID, fmt.Sprintf("Max cycles reached (%d). Manual review recommended.", l.MaxCycles))
	return nil, ErrMaxCycles
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
	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])

		if line == "ISSUE:" {
			f := ReviewFinding{}
			i++
			for i < len(lines) {
				inner := strings.TrimSpace(lines[i])
				if inner == "" || inner == "ISSUE:" {
					break
				}
				if strings.HasPrefix(inner, "SEVERITY:") {
					f.Severity = strings.TrimSpace(strings.TrimPrefix(inner, "SEVERITY:"))
				} else if strings.HasPrefix(inner, "DESCRIPTION:") {
					f.Description = strings.TrimSpace(strings.TrimPrefix(inner, "DESCRIPTION:"))
					// Collect continuation lines.
					i++
					for i < len(lines) {
						cont := strings.TrimSpace(lines[i])
						if cont == "" || cont == "ISSUE:" || strings.HasPrefix(cont, "SEVERITY:") || strings.HasPrefix(cont, "APPROVED:") {
							break
						}
						f.Description += " " + cont
						i++
					}
					continue
				}
				i++
			}
			if f.Description != "" {
				if f.Severity == "" {
					f.Severity = "major"
				}
				findings = append(findings, f)
			}
			continue
		}
		i++
	}

	return findings
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
