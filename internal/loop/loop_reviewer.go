package loop

import (
	"context"
	"fmt"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/ui"
)

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
		MCP: l.MCP,
	}
}

// runReviewerPhase invokes the reviewer agent, updates state and UI, parses
// findings, and emits lifecycle events.
func (l *Loop) runReviewerPhase(ctx context.Context, state *CycleState, perAgentBudget float64) error {
	state.Phase = PhaseReviewing
	l.UI.AgentStart("reviewer")

	prompt := l.buildReviewerPrompt(state)
	relayBlock, relayIDs := l.pendingHailRelay()
	if relayBlock != "" {
		prompt = relayBlock + "\n" + prompt
	}
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
	l.markHailsRelayed(relayIDs)
	state.Findings = ParseReviewFindings(result.ResultText)
	state.Verifications = ParseVerifications(result.ResultText)
	l.emit(ctx, Event{
		Kind:    EventAgentDone,
		BeadID:  state.TaskBeadID,
		Cycle:   state.Cycle,
		Agent:   "reviewer",
		Result:  &result,
		Message: fmt.Sprintf("[reviewer cycle %d]\n%s", state.Cycle, truncate(result.ResultText, 2000)),
	})
	l.trackCacheMetrics(ctx, state, "reviewer", &result)
	l.emitCycleSummary(state, PhaseReviewComplete, result)
	return nil
}

// extractAndPostHails parses the reviewer's report and queries fabric
// discoveries, converting them into Hail objects posted to l.HailQueue.
// It also applies escalation rules: critical findings and high-risk/low-
// satisfaction reports generate additional hails. A nil HailQueue makes
// this a no-op.
func (l *Loop) extractAndPostHails(ctx context.Context, state *CycleState) {
	if l.HailQueue == nil {
		return
	}

	phaseID := l.TaskID

	// 1. Extract hails from the reviewer's report (NEEDS_HUMAN_REVIEW flag).
	report := ParseReviewReport(state.ReviewOutput)
	for _, h := range extractReviewerHails(report, state, phaseID) {
		if err := l.HailQueue.Post(h); err != nil {
			l.UI.Error(fmt.Sprintf("failed to post reviewer hail: %v", err))
		}
	}

	// 2. Escalate critical-severity findings to blocker hails.
	for _, h := range escalateCriticalFindings(state.Findings, state, phaseID) {
		if err := l.HailQueue.Post(h); err != nil {
			l.UI.Error(fmt.Sprintf("failed to post critical finding hail: %v", err))
		}
	}

	// 3. Escalate high risk + low satisfaction to a decision-needed hail.
	if h := escalateHighRiskLowSatisfaction(report, state, phaseID); h != nil {
		if err := l.HailQueue.Post(*h); err != nil {
			l.UI.Error(fmt.Sprintf("failed to post risk escalation hail: %v", err))
		}
	}

	// 4. Bridge fabric discoveries to hails, skipping any already bridged
	//    in previous cycles to avoid duplicate hails.
	if l.FabricEnabled && l.Fabric != nil {
		discoveries, err := l.Fabric.UnresolvedDiscoveries(ctx)
		if err != nil {
			l.UI.Error(fmt.Sprintf("failed to fetch fabric discoveries: %v", err))
			return
		}

		// Filter out discoveries already bridged in earlier cycles.
		var newDiscoveries []fabric.Discovery
		for _, d := range discoveries {
			if !state.bridgedDiscoveryIDs[d.ID] {
				newDiscoveries = append(newDiscoveries, d)
			}
		}

		for _, h := range bridgeDiscoveryHails(newDiscoveries, phaseID, state.Cycle) {
			if err := l.HailQueue.Post(h); err != nil {
				l.UI.Error(fmt.Sprintf("failed to post discovery hail: %v", err))
			}
		}

		// Record all new discovery IDs as bridged so subsequent cycles skip them.
		if len(newDiscoveries) > 0 {
			if state.bridgedDiscoveryIDs == nil {
				state.bridgedDiscoveryIDs = make(map[int64]bool)
			}
			for _, d := range newDiscoveries {
				state.bridgedDiscoveryIDs[d.ID] = true
			}
		}
	}
}

// postMaxCyclesHail creates and posts a blocker hail when the loop exhausts
// its maximum cycle count without approval. A nil HailQueue makes this a no-op.
func (l *Loop) postMaxCyclesHail(state *CycleState) {
	if l.HailQueue == nil {
		return
	}
	h := buildMaxCyclesHail(state, l.TaskID)
	if err := l.HailQueue.Post(h); err != nil {
		l.UI.Error(fmt.Sprintf("failed to post max-cycles hail: %v", err))
	}
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
