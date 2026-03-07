package tui

import (
	"fmt"
	"strings"
)

// Satisfaction represents the reviewer's satisfaction level for a cycle.
type Satisfaction int

const (
	// SatisfactionNone means no reviewer verdict is available yet.
	SatisfactionNone Satisfaction = iota
	// SatisfactionGreen means the reviewer approved — no issues.
	SatisfactionGreen
	// SatisfactionYellow means the reviewer found minor issues.
	SatisfactionYellow
	// SatisfactionRed means the reviewer found critical issues.
	SatisfactionRed
)

// maxSummaryLines is the maximum number of lines in an extracted summary.
const maxSummaryLines = 3

// summaryHeadings are markdown-style headings that signal a summary section
// in agent output. Order matters — earlier entries take priority.
var summaryHeadings = []string{
	"## summary",
	"## what i changed",
	"## changes",
	"## what changed",
	"### summary",
	"what i changed",
	"what i did",
}

// ExtractAgentSummary extracts a concise 2-3 line summary from agent output.
// It searches for structured sections (e.g. "## Summary", "What I changed")
// and returns their first few lines. If no structured section is found, it
// falls back to the last non-empty lines of the output.
// Returns an empty string if the output is empty.
func ExtractAgentSummary(output string) string {
	if output == "" {
		return ""
	}

	lines := strings.Split(output, "\n")

	// Try structured section extraction first.
	if summary := extractFromSection(lines); summary != "" {
		return summary
	}

	// Fallback: use the last non-empty lines, which often contain conclusions.
	return extractLastLines(lines, maxSummaryLines)
}

// extractFromSection searches for a known summary heading and returns the
// first maxSummaryLines non-empty lines after it.
func extractFromSection(lines []string) string {
	for _, heading := range summaryHeadings {
		for i, line := range lines {
			trimmed := strings.TrimSpace(strings.ToLower(line))
			if trimmed == heading || strings.HasPrefix(trimmed, heading+":") || strings.HasPrefix(trimmed, heading+" ") {
				return collectLinesAfter(lines, i+1, maxSummaryLines)
			}
		}
	}
	return ""
}

// collectLinesAfter collects up to maxLines non-empty lines starting at startIdx.
func collectLinesAfter(lines []string, startIdx, maxLines int) string {
	var collected []string
	for i := startIdx; i < len(lines) && len(collected) < maxLines; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			// Stop at a blank line after we've collected at least one line.
			if len(collected) > 0 {
				break
			}
			continue
		}
		// Stop at the next heading (new section).
		if strings.HasPrefix(trimmed, "##") {
			break
		}
		collected = append(collected, trimmed)
	}
	return strings.Join(collected, "\n")
}

// extractLastLines returns the last n non-empty lines from the output.
func extractLastLines(lines []string, n int) string {
	var nonEmpty []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			nonEmpty = append(nonEmpty, trimmed)
		}
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	start := len(nonEmpty) - n
	if start < 0 {
		start = 0
	}
	return strings.Join(nonEmpty[start:], "\n")
}

// SetSatisfaction sets the reviewer satisfaction level on the last reviewer in
// the current cycle.
func (lv *LoopView) SetSatisfaction(s Satisfaction) {
	if len(lv.Cycles) == 0 {
		return
	}
	c := &lv.Cycles[len(lv.Cycles)-1]
	for i := len(c.Agents) - 1; i >= 0; i-- {
		if c.Agents[i].Role == "reviewer" {
			c.Agents[i].Satisfaction = s
			return
		}
	}
}

// renderAgentSummaryLines writes indented summary lines below a done agent row
// in the loop view. Uses tree continuation characters matching the agent's
// position (last vs non-last) in the cycle.
func renderAgentSummaryLines(b *strings.Builder, a AgentEntry, isLast bool, width int) {
	if !a.Done || a.Summary == "" {
		return
	}
	summaryLines := strings.SplitN(a.Summary, "\n", maxSummaryLines+1)
	if len(summaryLines) > maxSummaryLines {
		summaryLines = summaryLines[:maxSummaryLines]
	}
	continuation := "│   "
	if isLast {
		continuation = "    "
	}
	for _, sl := range summaryLines {
		summaryText := styleDetailDim.Render(TruncateWithEllipsis(sl, width-10))
		b.WriteString("  " + styleTreeConnector.Render(continuation) + summaryText + "\n")
	}
}

// SatisfactionFromReview derives a Satisfaction level from review outcome data.
func SatisfactionFromReview(approved bool, issueCount int) Satisfaction {
	if approved {
		return SatisfactionGreen
	}
	if issueCount <= 2 {
		return SatisfactionYellow
	}
	return SatisfactionRed
}

// SatisfactionBadge returns a styled badge string for the satisfaction level.
func SatisfactionBadge(s Satisfaction) string {
	switch s {
	case SatisfactionGreen:
		return styleRowDone.Render("✓ approved")
	case SatisfactionYellow:
		return styleHighlightIssue.Render("⚠ issues")
	case SatisfactionRed:
		return styleHighlightCritical.Render("✗ critical")
	default:
		return ""
	}
}

// SetPhaseCompletionNote sets the completion note displayed on board cards.
func (nv *NebulaView) SetPhaseCompletionNote(phaseID string, note string) {
	for i := range nv.Phases {
		if nv.Phases[i].ID == phaseID {
			nv.Phases[i].CompletionNote = note
			return
		}
	}
}

// FormatFileSummary renders a compact list of modified files with +/- stats.
func FormatFileSummary(files []FileStatEntry) string {
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	for i, f := range files {
		if i >= 5 {
			b.WriteString(styleDetailDim.Render(fmt.Sprintf("  +%d more files", len(files)-5)))
			break
		}
		adds := styleDiffStatAdd.Render(fmt.Sprintf("+%d", f.Additions))
		dels := styleDiffStatDel.Render(fmt.Sprintf("-%d", f.Deletions))
		b.WriteString(fmt.Sprintf("  %s %s %s", f.Path, adds, dels))
		if i < len(files)-1 && i < 4 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// FormatPhaseSummary renders a completion summary panel for a finished phase.
// It includes total cycles, diff stats, cost, duration, and final assessment.
func FormatPhaseSummary(p PhaseEntry, lv *LoopView) string {
	var b strings.Builder

	label := styleDetailHeaderLabel.Render
	value := styleDetailHeaderValue.Render

	// Cycles and cost.
	b.WriteString(label("cycles: "))
	b.WriteString(value(fmt.Sprintf("%d", p.Cycles)))
	b.WriteString("  ")
	b.WriteString(label("cost: "))
	b.WriteString(value(fmt.Sprintf("$%.2f", p.CostUSD)))

	// Duration.
	elapsed := formatDuration(p.StartedAt, p.CompletedAt)
	if elapsed != "" {
		b.WriteString("  ")
		b.WriteString(label("duration: "))
		b.WriteString(value(elapsed))
	}

	// Final assessment — derive from loop view state.
	if lv != nil && lv.Approved {
		b.WriteString("\n")
		b.WriteString(label("assessment: "))
		b.WriteString(SatisfactionBadge(SatisfactionGreen))
	}

	// Files modified — aggregate from all coder diffs.
	if lv != nil {
		allFiles := aggregateFiles(lv)
		if len(allFiles) > 0 {
			b.WriteString("\n")
			b.WriteString(label("files modified:"))
			b.WriteString("\n")
			b.WriteString(FormatFileSummary(allFiles))
		}
	}

	// Last reviewer summary.
	if lv != nil {
		if summary := lastReviewerSummary(lv); summary != "" {
			b.WriteString("\n")
			b.WriteString(label("reviewer: "))
			b.WriteString(value(summary))
		}
	}

	return b.String()
}

// aggregateFiles collects unique file entries from all coder agents across cycles.
func aggregateFiles(lv *LoopView) []FileStatEntry {
	seen := make(map[string]FileStatEntry)
	var order []string
	for _, c := range lv.Cycles {
		for _, a := range c.Agents {
			if a.Role != "coder" {
				continue
			}
			for _, f := range a.DiffFiles {
				if _, exists := seen[f.Path]; !exists {
					order = append(order, f.Path)
				}
				// Overwrite with latest stats.
				seen[f.Path] = f
			}
		}
	}
	result := make([]FileStatEntry, 0, len(order))
	for _, path := range order {
		result = append(result, seen[path])
	}
	return result
}

// lastReviewerSummary returns the summary from the last reviewer agent.
func lastReviewerSummary(lv *LoopView) string {
	for i := len(lv.Cycles) - 1; i >= 0; i-- {
		for j := len(lv.Cycles[i].Agents) - 1; j >= 0; j-- {
			a := lv.Cycles[i].Agents[j]
			if a.Role == "reviewer" && a.Summary != "" {
				return a.Summary
			}
		}
	}
	return ""
}
