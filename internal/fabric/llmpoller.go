package fabric

import (
	"context"
	"fmt"
	"strings"

	"github.com/papapumpkin/quasar/internal/agent"
)

// LLMPoller uses an LLM call to evaluate fabric readiness for a phase.
// It builds a prompt containing the phase body and rendered fabric snapshot,
// then parses the LLM response into a PollResult. Malformed responses
// default to PROCEED (fail-open).
type LLMPoller struct {
	// Invoker executes the LLM call using the same configuration as the
	// main coder-reviewer loop.
	Invoker agent.Invoker

	// Phases maps phase IDs to their specs for body lookup.
	Phases map[string]*PhaseSpec
}

// PhaseSpec holds the minimal phase information needed by the poller.
// It mirrors the fields from nebula.PhaseSpec that the poll prompt requires.
type PhaseSpec struct {
	// ID is the unique phase identifier.
	ID string

	// Body is the markdown body describing the phase's task.
	Body string
}

// Poll checks whether phaseID has enough context from the fabric to proceed.
// It constructs a prompt with the phase body and fabric snapshot, invokes the
// LLM, and parses the response into a PollResult.
func (p *LLMPoller) Poll(ctx context.Context, phaseID string, snap Snapshot) (PollResult, error) {
	spec, ok := p.Phases[phaseID]
	if !ok {
		return PollResult{}, fmt.Errorf("unknown phase %q", phaseID)
	}

	prompt := buildPollPrompt(spec.Body, snap)

	a := agent.Agent{
		Role:         agent.RoleArchitect,
		SystemPrompt: "You evaluate whether coding tasks have sufficient context to begin.",
	}

	result, err := p.Invoker.Invoke(ctx, a, prompt, ".")
	if err != nil {
		return PollResult{}, fmt.Errorf("poll LLM call for %q: %w", phaseID, err)
	}

	return parseResponse(result.ResultText), nil
}

// buildPollPrompt constructs the LLM prompt for evaluating fabric readiness.
func buildPollPrompt(phaseBody string, snap Snapshot) string {
	var b strings.Builder

	b.WriteString("You are evaluating whether a coding task has sufficient context to begin.\n\n")
	b.WriteString("## Your Task\n")
	b.WriteString(phaseBody)
	b.WriteString("\n\n")
	b.WriteString("## Available Context (Fabric State)\n")
	b.WriteString(RenderSnapshot(snap))
	b.WriteString("\n")
	b.WriteString("## Instructions\n")
	b.WriteString("Based on the task description and the available entanglements/interfaces on the fabric, determine if you can proceed.\n\n")
	b.WriteString("Respond with EXACTLY ONE of these decisions:\n\n")
	b.WriteString("PROCEED — You have all the interfaces, types, and function signatures needed to write compilable code. ")
	b.WriteString("Minor uncertainty about implementation details is fine — proceed with your best judgment and the reviewer will catch issues.\n\n")
	b.WriteString("NEED_INFO — You literally cannot write compilable code without a missing type, interface, or function signature. ")
	b.WriteString("Specify exactly what's missing.\n\n")
	b.WriteString("CONFLICT — An entanglement on the fabric contradicts another entanglement, or a file you need to modify is actively claimed by a running phase. ")
	b.WriteString("Specify the conflict.\n\n")
	b.WriteString("IMPORTANT: Only respond NEED_INFO or CONFLICT for structural blockers. ")
	b.WriteString("If you're unsure about a return type or implementation detail but could make a reasonable assumption, respond PROCEED.\n\n")
	b.WriteString("Your response (PROCEED, NEED_INFO, or CONFLICT followed by explanation):")

	return b.String()
}

// parseResponse extracts a PollResult from the raw LLM response text.
// It looks for the first word as the decision keyword. Malformed responses
// default to PROCEED (fail-open, not fail-closed).
func parseResponse(raw string) PollResult {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return PollResult{Decision: PollProceed, Reason: "empty response, defaulting to proceed"}
	}

	// Extract the first word as the decision keyword.
	firstWord, rest := splitFirst(trimmed)
	decision := strings.ToUpper(firstWord)
	reason := strings.TrimSpace(rest)

	// Strip a leading em-dash or dash separator if present.
	reason = strings.TrimLeft(reason, "—–- ")
	reason = strings.TrimSpace(reason)

	switch PollDecision(decision) {
	case PollProceed:
		return PollResult{Decision: PollProceed, Reason: reason}
	case PollNeedInfo:
		return PollResult{
			Decision:    PollNeedInfo,
			Reason:      reason,
			MissingInfo: extractBullets(reason),
		}
	case PollConflict:
		return PollResult{
			Decision:     PollConflict,
			Reason:       reason,
			ConflictWith: extractConflictTarget(reason),
		}
	default:
		// Malformed response: fail-open.
		return PollResult{
			Decision: PollProceed,
			Reason:   fmt.Sprintf("unrecognized decision %q, defaulting to proceed: %s", firstWord, reason),
		}
	}
}

// splitFirst splits s into the first whitespace-delimited word and the rest.
func splitFirst(s string) (string, string) {
	i := strings.IndexAny(s, " \t\n\r")
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i+1:]
}

// extractBullets parses bulleted items from the reason text. It looks for
// lines starting with "- " or "* " and returns the trimmed text of each.
func extractBullets(reason string) []string {
	var items []string
	for _, line := range strings.Split(reason, "\n") {
		trimmed := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(trimmed, "- "); ok {
			items = append(items, strings.TrimSpace(after))
		} else if after, ok := strings.CutPrefix(trimmed, "* "); ok {
			items = append(items, strings.TrimSpace(after))
		}
	}
	return items
}

// extractConflictTarget attempts to extract a phase ID or file path from the
// conflict reason. It looks for a backtick-delimited identifier first, then
// falls back to the first word after common prepositions.
func extractConflictTarget(reason string) string {
	// Try backtick-delimited identifier (e.g., `phase-auth` or `internal/auth.go`).
	if start := strings.Index(reason, "`"); start >= 0 {
		rest := reason[start+1:]
		if end := strings.Index(rest, "`"); end >= 0 {
			return rest[:end]
		}
	}

	// Look for "with <target>" or "on <target>" patterns.
	for _, prep := range []string{" with ", " on ", " by "} {
		if idx := strings.Index(strings.ToLower(reason), prep); idx >= 0 {
			after := strings.TrimSpace(reason[idx+len(prep):])
			target, _ := splitFirst(after)
			// Strip trailing punctuation.
			target = strings.TrimRight(target, ".,;:!")
			if target != "" {
				return target
			}
		}
	}

	return ""
}
