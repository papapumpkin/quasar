package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderBoardEntry renders a single phase card within a column.
// Cards show up to 4-5 lines: title, progress+health, cost, activity subtitle.
func (bv BoardView) renderBoardEntry(p PhaseEntry, selected bool, colWidth int) string {
	var sb strings.Builder

	icon, _ := phaseIconAndStyleStatic(p)

	// Line 1: icon + title (+ attention marker if needed).
	titleWidth := colWidth - 4 // icon + space + padding
	if p.HasPendingHails || p.Status == PhaseGate {
		titleWidth -= 2 // room for attention marker
	}
	if titleWidth < 4 {
		titleWidth = 4
	}
	title := TruncateWithEllipsis(p.Title, titleWidth)
	if title == "" {
		title = TruncateWithEllipsis(p.ID, titleWidth)
	}

	attention := ""
	if p.HasPendingHails || p.Status == PhaseGate {
		attention = " " + styleAttentionMarker.Render("!")
	}

	if selected {
		indicator := styleSelectionIndicator.Render(selectionIndicator)
		styledTitle := styleRowSelected.Render(title)
		sb.WriteString(fmt.Sprintf("%s %s %s%s", indicator, icon, styledTitle, attention))
	} else {
		sb.WriteString(fmt.Sprintf("  %s %s%s", icon, title, attention))
	}

	// Line 2: health dot + progress indicator + cost badge (for active/completed phases).
	if p.Status == PhaseWorking || p.Status == PhaseDone || p.Status == PhaseFailed || p.Status == PhaseGate {
		sb.WriteString("\n")
		sb.WriteString(bv.renderCardMetaLine(p, colWidth))
	}

	// Line 3: activity subtitle (for running phases only).
	if p.Status == PhaseWorking && p.LastActivity != "" {
		actWidth := colWidth - 4
		if actWidth < 4 {
			actWidth = 4
		}
		truncated := TruncateWithEllipsis(p.LastActivity, actWidth)
		sb.WriteString("\n    ")
		sb.WriteString(styleCardSubtitle.Render(truncated))
	}

	// Line 3 (done/failed): summary snippet from the last reviewer.
	if (p.Status == PhaseDone || p.Status == PhaseFailed) && p.CompletionNote != "" {
		noteWidth := colWidth - 4
		if noteWidth < 4 {
			noteWidth = 4
		}
		truncated := TruncateWithEllipsis(p.CompletionNote, noteWidth)
		sb.WriteString("\n    ")
		sb.WriteString(styleCardSubtitle.Render(truncated))
	}

	return sb.String()
}

// renderCardMetaLine renders the health dot, progress indicator, and cost badge
// as a compact second line on a board card.
func (bv BoardView) renderCardMetaLine(p PhaseEntry, colWidth int) string {
	var parts []string

	// Health dot.
	parts = append(parts, renderHealthDot(p.Health()))

	// Progress indicator (cycle fraction).
	if p.MaxCycles > 0 {
		progress := renderProgress(p.Cycles, p.MaxCycles)
		parts = append(parts, progress)
	}

	// Cost badge.
	if p.CostUSD > 0 {
		parts = append(parts, renderCostBadge(p.CostUSD, p.MaxBudgetUSD))
	}

	return "    " + strings.Join(parts, " ")
}

// renderHealthDot returns a colored dot based on the health signal.
func renderHealthDot(h PhaseHealth) string {
	switch h {
	case HealthYellow:
		return styleHealthYellow.Render(healthDot)
	case HealthRed:
		return styleHealthRed.Render(healthDot)
	default:
		return styleHealthGreen.Render(healthDot)
	}
}

// renderProgress returns a compact cycle progress indicator like "[3/5]",
// color-coded by urgency.
func renderProgress(cycles, maxCycles int) string {
	text := fmt.Sprintf("[%d/%d]", cycles, maxCycles)
	ratio := float64(cycles) / float64(maxCycles)
	switch {
	case ratio >= 1.0:
		return styleProgressRed.Render(text)
	case ratio > 0.6:
		return styleProgressYellow.Render(text)
	default:
		return styleProgressGreen.Render(text)
	}
}

// renderCostBadge returns a formatted cost string, highlighted when approaching budget.
func renderCostBadge(cost, budget float64) string {
	text := fmt.Sprintf("$%.2f", cost)
	if budget > 0 && cost/budget > 0.8 {
		return styleCardCostDanger.Render(text)
	}
	return styleCardCost.Render(text)
}

// phaseIconAndStyleStatic returns the status icon for a phase (package-level, no spinner).
func phaseIconAndStyleStatic(p PhaseEntry) (string, lipgloss.Style) {
	switch p.Status {
	case PhaseDone:
		return styleRowDone.Render(iconDone), styleRowDone
	case PhaseWorking:
		return styleRowWorking.Render(iconWorking), styleRowWorking
	case PhaseFailed:
		return styleRowFailed.Render(iconFailed), styleRowFailed
	case PhaseGate:
		return styleRowGate.Render(iconGate), styleRowGate
	case PhaseSkipped:
		return styleRowWaiting.Render(iconSkipped), styleRowWaiting
	default:
		return styleRowWaiting.Render(iconWaiting), styleRowWaiting
	}
}
