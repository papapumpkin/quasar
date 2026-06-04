package loop

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/filter"
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
		fmt.Fprintf(&b, "Task (bead %s): %s\n\n", state.TaskID, state.TaskTitle)
		b.WriteString("Implement this task. Read existing code first to understand the codebase, then make the necessary changes.")
	} else {
		fmt.Fprintf(&b, "Task (bead %s): %s\n\n", state.TaskID, state.TaskTitle)
		b.WriteString("The reviewer found issues with your previous implementation. Please address them:\n\n")
		// Filter out findings already marked as fixed so the coder only
		// works on unresolved issues.
		n := 0
		for _, f := range state.Findings {
			if f.Status == FindingStatusFixed {
				continue
			}
			n++
			fmt.Fprintf(&b, "%d. [%s] %s\n", n, f.Severity, f.Description)
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

	fmt.Fprintf(&b, "Task (bead %s):\n\n", state.TaskID)
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
// commands report issues that need fixing. It delegates to buildFilterFixPrompt
// when a ParseResult is available from the lint output, falling back to legacy
// behavior otherwise.
func (l *Loop) buildLintFixPrompt(state *CycleState) string {
	// When lint output exists, parse it as a lint check and delegate to the
	// shared filter fix prompt for consistency.
	if state.LintOutput != "" {
		parsed := filter.ParseCheckOutput(filter.CheckResult{
			Name:   "lint",
			Output: state.LintOutput,
		})
		return l.buildFilterFixPrompt(state, parsed)
	}

	// No lint output — shouldn't normally happen, but return a minimal prompt.
	var b strings.Builder
	fmt.Fprintf(&b, "Task (bead %s): Fix failing lint check\n\n", state.TaskID)
	b.WriteString("Your code has lint issues that need to be fixed before reviewer handoff.\n")
	b.WriteString("\nRead each affected file, fix the listed errors, and verify your fix compiles.\n")
	b.WriteString("Do NOT make any other changes. Stay focused on these specific errors.\n")
	return b.String()
}

// buildFilterFixPrompt constructs a minimal prompt for the coder to fix
// specific filter errors. It includes only the failing check name, the
// structured errors (file, line, message), and instructions to fix them.
// When no structured errors were parsed, it falls back to including the
// raw output (similar to the current behavior but with tighter framing).
func (l *Loop) buildFilterFixPrompt(state *CycleState, parsed filter.ParseResult) string {
	var b strings.Builder

	// 1. Context header — minimal, just bead ID and check name.
	fmt.Fprintf(&b, "Task (bead %s): Fix failing %s check\n\n", state.TaskID, parsed.CheckName)
	fmt.Fprintf(&b, "Your code failed the %s filter check. Fix ONLY the errors listed below.\n", parsed.CheckName)
	b.WriteString("Do not refactor, do not add features, do not change anything unrelated to these errors.\n\n")

	// 2. Error list — structured when available, raw fallback otherwise.
	if len(parsed.Errors) > 0 {
		writeStructuredErrors(&b, parsed.Errors)
	} else {
		b.WriteString("RAW OUTPUT:\n")
		b.WriteString(truncate(parsed.RawOutput, 2000))
		b.WriteString("\n")
	}

	// 3. Instructions.
	b.WriteString("\nRead each affected file, fix the listed errors, and verify your fix compiles.\n")
	b.WriteString("Do NOT make any other changes. Stay focused on these specific errors.\n")

	// Budget hint.
	if budget := l.filterFixBudget(); budget > 0 {
		fmt.Fprintf(&b, "\nThis is a targeted fix pass. Keep your budget under $%.2f — read the affected files, apply minimal fixes, and stop.\n", budget)
	}

	return b.String()
}

// writeStructuredErrors writes the ERRORS and AFFECTED FILES sections from
// a list of filter.Error entries. Errors are grouped by file path and sorted by error
// count descending; within each file, errors are sorted by line number ascending.
func writeStructuredErrors(b *strings.Builder, errs []filter.Error) {
	// Group errors by file.
	type fileGroup struct {
		File   string
		Errors []filter.Error
	}
	byFile := make(map[string]*fileGroup, len(errs))
	var order []string
	for _, e := range errs {
		g, ok := byFile[e.File]
		if !ok {
			g = &fileGroup{File: e.File}
			byFile[e.File] = g
			order = append(order, e.File)
		}
		g.Errors = append(g.Errors, e)
	}

	// Sort file groups by error count descending, then file name ascending for stability.
	sort.Slice(order, func(i, j int) bool {
		ci, cj := len(byFile[order[i]].Errors), len(byFile[order[j]].Errors)
		if ci != cj {
			return ci > cj
		}
		return order[i] < order[j]
	})

	// Sort errors within each group by line ascending.
	for _, g := range byFile {
		sort.Slice(g.Errors, func(i, j int) bool {
			return g.Errors[i].Line < g.Errors[j].Line
		})
	}

	// Write numbered error list.
	b.WriteString("ERRORS:\n")
	n := 0
	for _, file := range order {
		for _, e := range byFile[file].Errors {
			n++
			if e.Column > 0 {
				fmt.Fprintf(b, "%d. %s:%d:%d — %s\n", n, e.File, e.Line, e.Column, e.Message)
			} else {
				fmt.Fprintf(b, "%d. %s:%d — %s\n", n, e.File, e.Line, e.Message)
			}
		}
	}

	// Write affected files summary.
	b.WriteString("\nAFFECTED FILES:\n")
	for _, file := range order {
		count := len(byFile[file].Errors)
		if count == 1 {
			fmt.Fprintf(b, "- %s (1 error)\n", file)
		} else {
			fmt.Fprintf(b, "- %s (%d errors)\n", file, count)
		}
	}
}

// filterFixBudget returns a per-invocation budget for filter fix attempts.
// It's smaller than perAgentBudget since these should be quick, mechanical fixes.
func (l *Loop) filterFixBudget() float64 {
	full := l.perAgentBudget()
	if full <= 0 {
		return 0
	}
	// Use 1/4 of the normal per-agent budget for targeted fixes.
	return full / 4
}

// buildReviewerPrompt constructs the prompt sent to the reviewer agent,
// including the coder's output for evaluation.
func (l *Loop) buildReviewerPrompt(state *CycleState) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Task (bead %s): %s\n\n", state.TaskID, state.TaskTitle)
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

	// Inject prior findings for verification when this is not the first cycle.
	if len(state.AllFindings) > 0 {
		b.WriteString("\n")
		b.WriteString(buildPriorFindingsBlock(state.AllFindings))
	}

	return b.String()
}

// buildPriorFindingsBlock constructs the prior-findings section injected into
// the reviewer prompt on cycles > 1. It serializes all accumulated findings
// and adds explicit instructions for the reviewer to verify each one.
func buildPriorFindingsBlock(findings []ReviewFinding) string {
	var b strings.Builder
	b.WriteString("[PRIOR FINDINGS]\n")
	b.WriteString("The following issues were identified in previous review cycles.\n")
	b.WriteString("You MUST verify each one against the current code and report its status.\n\n")
	b.WriteString(SerializeFindings(findings, 200))
	b.WriteString("\nFor EACH prior finding, include a VERIFICATION: block in your response:\n\n")
	b.WriteString("VERIFICATION:\n")
	b.WriteString("FINDING_ID: <id from above>\n")
	b.WriteString("STATUS: fixed|still_present|regressed\n")
	b.WriteString("COMMENT: Brief explanation of what you observed.\n\n")
	b.WriteString("After verifying all prior findings, proceed with your normal review.\n")
	b.WriteString("Report any NEW issues as ISSUE: blocks (they will get new IDs automatically).\n")
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
// queries are non-fatal — the snapshot will contain whatever data was available,
// but errors are logged via the UI so operators can diagnose degraded snapshots.
func (l *Loop) buildFabricSnapshot(ctx context.Context) fabric.Snapshot {
	entanglements, err := l.Fabric.AllEntanglements(ctx)
	if err != nil {
		l.UI.Error(fmt.Sprintf("fabric: failed to query entanglements: %v", err))
	}
	claims, err := l.Fabric.AllClaims(ctx)
	if err != nil {
		l.UI.Error(fmt.Sprintf("fabric: failed to query claims: %v", err))
	}
	states, err := l.Fabric.AllPhaseStates(ctx)
	if err != nil {
		l.UI.Error(fmt.Sprintf("fabric: failed to query phase states: %v", err))
	}
	discoveries, err := l.Fabric.UnresolvedDiscoveries(ctx)
	if err != nil {
		l.UI.Error(fmt.Sprintf("fabric: failed to query unresolved discoveries: %v", err))
	}
	pulses, err := l.Fabric.AllPulses(ctx)
	if err != nil {
		l.UI.Error(fmt.Sprintf("fabric: failed to query pulses: %v", err))
	}

	// Partition phases into completed, in-progress, and blocked.
	var completed, inProgress, blocked []string
	for id, s := range states {
		switch s {
		case fabric.StateDone:
			completed = append(completed, id)
		case fabric.StateRunning:
			inProgress = append(inProgress, id)
		case fabric.StateBlocked:
			blocked = append(blocked, id)
		}
	}
	sort.Strings(completed)
	sort.Strings(inProgress)
	sort.Strings(blocked)

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
		Blocked:               blocked,
		UnresolvedDiscoveries: discoveries,
		Pulses:                pulses,
		PhaseStates:           states,
		// PhaseCycles is populated by the nebula orchestrator when available;
		// the fabric store does not track cycle counts directly.
	}
}

// composeVolatilePrefix prepends volatile context (fabric snapshot) to the task
// prompt. Stable content (project context) is handled exclusively by
// BuildSystemPrompt and must NOT be duplicated here. This separation ensures
// the system prompt has a byte-identical prefix across cycles for prompt cache
// hits, while volatile per-cycle data (fabric state) lives in the user prompt.
func (l *Loop) composeVolatilePrefix(ctx context.Context, taskPrompt string) string {
	if !l.FabricEnabled || l.Fabric == nil {
		return taskPrompt
	}

	snap := l.buildFabricSnapshot(ctx)
	return PrependFabricContext(taskPrompt, snap)
}
