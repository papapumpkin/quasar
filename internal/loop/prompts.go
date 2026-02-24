package loop

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// buildCoderPrompt constructs the prompt sent to the coder agent for a given
// cycle. On the first cycle it provides the task description; on subsequent
// cycles it includes the reviewer's findings for the coder to address.
func (l *Loop) buildCoderPrompt(state *CycleState) string {
	var b strings.Builder

	if state.Refactored {
		b.WriteString(l.buildRefactorPrompt(state))
		// Clear refactor flag so subsequent cycles use the normal prompt
		// with the updated description as the new baseline.
		state.Refactored = false
		state.OriginalDescription = ""
		state.RefactorDescription = ""
		return b.String()
	}

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

// buildRefactorPrompt constructs the coder prompt when the user has updated
// the task description mid-execution. It includes both the original and updated
// descriptions so the coder understands the course correction, plus previous
// cycle context to preserve good progress.
func (l *Loop) buildRefactorPrompt(state *CycleState) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Task (bead %s):\n\n", state.TaskBeadID)
	b.WriteString("[REFACTOR — USER UPDATE]\n")
	b.WriteString("The user has updated the task description while you were working.\n")
	b.WriteString("The original task was:\n---\n")
	b.WriteString(state.OriginalDescription)
	b.WriteString("\n---\n\n")
	b.WriteString("The UPDATED task description is:\n---\n")
	b.WriteString(state.RefactorDescription)
	b.WriteString("\n---\n\n")
	b.WriteString("Important: The user is actively watching and has provided this updated\n")
	b.WriteString("guidance based on your work so far. Prioritize the new instructions\n")
	b.WriteString("while preserving any good progress from previous cycles.\n\n")

	if state.CoderOutput != "" || len(state.Findings) > 0 {
		b.WriteString("[PREVIOUS WORK]\n")
		if state.CoderOutput != "" {
			b.WriteString("Your output from the last cycle:\n")
			b.WriteString(truncate(state.CoderOutput, 2000))
			b.WriteString("\n\n")
		}
		if len(state.Findings) > 0 {
			b.WriteString("Reviewer feedback:\n")
			for i, f := range state.Findings {
				fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, f.Severity, f.Description)
			}
		}
	}

	return b.String()
}

// buildLintFixPrompt constructs the prompt sent to the coder when lint
// commands report issues that need fixing.
func (l *Loop) buildLintFixPrompt(state *CycleState) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Task (bead %s): %s\n\n", state.TaskBeadID, state.TaskTitle)
	b.WriteString("Your code has lint issues that need to be fixed before reviewer handoff.\n\n")
	b.WriteString("LINT OUTPUT:\n")
	b.WriteString(truncate(state.LintOutput, 3000))
	b.WriteString("\n\nFix all reported lint issues. Read the relevant files, apply fixes, and ensure the code is clean.")

	return b.String()
}

// buildReviewerPrompt constructs the prompt sent to the reviewer agent,
// including the coder's output for evaluation.
func (l *Loop) buildReviewerPrompt(state *CycleState) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Task (bead %s): %s\n\n", state.TaskBeadID, state.TaskTitle)
	b.WriteString("The coder has completed their work. Here is their summary:\n\n")
	b.WriteString(truncate(state.CoderOutput, 3000))

	if state.LintOutput != "" {
		b.WriteString("\n\nNOTE: The following lint issues were not fully resolved by the coder:\n")
		b.WriteString(truncate(state.LintOutput, 2000))
	}

	b.WriteString("\n\nREVIEW INSTRUCTIONS:\n")
	b.WriteString("1. READ THE ACTUAL SOURCE FILES to verify the changes — do not rely solely on the summary above.\n")
	b.WriteString("2. Check for correctness, security, error handling, code quality, and edge cases.\n")
	b.WriteString("3. Check for any linting issues (`go vet`, `go fmt`). If linting problems exist, flag them as issues for the coder to fix.\n")
	b.WriteString("4. End your review with either APPROVED: or one or more ISSUE: blocks.\n")

	return b.String()
}

// PrependFabricContext adds current entanglements, claims, and pulses to the
// task description so the agent starts with full coordination context rather
// than needing to query fabric state as its first action.
func PrependFabricContext(desc string, snap fabric.Snapshot) string {
	var b strings.Builder
	b.WriteString("## Current Fabric State\n\n")
	b.WriteString(fabric.RenderSnapshot(snap))
	b.WriteString("\n\n---\n\n")
	b.WriteString(desc)
	return b.String()
}

// buildFabricSnapshot queries the Fabric store for current state and returns
// a Snapshot suitable for injection into agent prompts. Errors from individual
// queries are non-fatal — the snapshot will contain whatever data was available.
func (l *Loop) buildFabricSnapshot(ctx context.Context) fabric.Snapshot {
	entanglements, _ := l.Fabric.AllEntanglements(ctx)
	claims, _ := l.Fabric.AllClaims(ctx)
	states, _ := l.Fabric.AllPhaseStates(ctx)
	discoveries, _ := l.Fabric.UnresolvedDiscoveries(ctx)
	pulses, _ := l.Fabric.AllPulses(ctx)

	// Partition phases into completed and in-progress.
	var completed, inProgress []string
	for id, s := range states {
		switch s {
		case fabric.StateDone:
			completed = append(completed, id)
		case fabric.StateRunning:
			inProgress = append(inProgress, id)
		}
	}
	sort.Strings(completed)
	sort.Strings(inProgress)

	// Build claim map from filepath to owning phase.
	claimMap := make(map[string]string, len(claims))
	for _, c := range claims {
		claimMap[c.Filepath] = c.OwnerTask
	}

	return fabric.Snapshot{
		Entanglements:         entanglements,
		FileClaims:            claimMap,
		Completed:             completed,
		InProgress:            inProgress,
		UnresolvedDiscoveries: discoveries,
		Pulses:                pulses,
	}
}
