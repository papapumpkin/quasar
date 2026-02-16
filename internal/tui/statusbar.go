package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// StatusBar renders the persistent top bar with task name, progress, budget, elapsed.
type StatusBar struct {
	Name      string
	BeadID    string
	Cycle     int
	MaxCycles int
	Completed int
	Total     int
	CostUSD   float64
	BudgetUSD float64
	StartTime time.Time
	Width     int
	Paused    bool
	Stopping  bool
}

// View renders the status bar as a single line.
func (s StatusBar) View() string {
	var left string
	if s.Total > 0 {
		// Nebula mode: show progress bar.
		bar := renderProgressBar(s.Completed, s.Total, 12)
		left = " " + styleStatusLabel.Render("QUASAR") + "  " +
			styleStatusValue.Render(fmt.Sprintf("nebula: %s  %s %d/%d", s.Name, bar, s.Completed, s.Total))
	} else if s.BeadID != "" {
		// Loop mode: show cycle mini-bar.
		cycleBar := renderCycleBar(s.Cycle, s.MaxCycles)
		left = " " + styleStatusLabel.Render("QUASAR") + "  " +
			styleStatusValue.Render(fmt.Sprintf("task %s  cycle %d/%d %s", s.BeadID, s.Cycle, s.MaxCycles, cycleBar))
	} else {
		left = " " + styleStatusLabel.Render("QUASAR")
	}

	// Show execution state indicators.
	if s.Stopping {
		left += "  " + styleStatusStopping.Render("STOPPING")
	} else if s.Paused {
		left += "  " + styleStatusPaused.Render("PAUSED")
	}

	var right string
	if s.BudgetUSD > 0 {
		budgetBar := renderBudgetBar(s.CostUSD, s.BudgetUSD, 10)
		right = styleStatusCost.Render(fmt.Sprintf("$%.2f", s.CostUSD)) + " " +
			budgetBar + " " +
			styleStatusCost.Render(fmt.Sprintf("$%.2f", s.BudgetUSD))
	} else {
		right = styleStatusCost.Render(fmt.Sprintf("$%.2f", s.CostUSD))
	}

	if !s.StartTime.IsZero() {
		elapsed := time.Since(s.StartTime).Truncate(time.Second)
		right += fmt.Sprintf("  %s", elapsed)
	}
	right += " "

	// Pad the gap between left and right.
	gap := s.Width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	padding := strings.Repeat(" ", gap)

	line := left + padding + right
	return styleStatusBar.Width(s.Width).Render(line)
}

// renderProgressBar creates a filled/empty bar showing completed/total progress.
// Uses blue→green color transition as progress increases.
func renderProgressBar(completed, total, width int) string {
	if total <= 0 || width <= 0 {
		return ""
	}
	ratio := float64(completed) / float64(total)
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width))
	empty := width - filled

	barColor := progressColor(ratio)
	style := lipgloss.NewStyle().Foreground(barColor)
	emptyStyle := lipgloss.NewStyle().Foreground(colorMuted)

	return style.Render(strings.Repeat("━", filled)) +
		emptyStyle.Render(strings.Repeat("░", empty))
}

// renderBudgetBar creates an inline budget consumption indicator.
// Color transitions green → yellow → red as budget approaches limit.
func renderBudgetBar(spent, budget float64, width int) string {
	if budget <= 0 || width <= 0 {
		return ""
	}
	ratio := spent / budget
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width))
	empty := width - filled

	barColor := budgetColor(ratio)
	style := lipgloss.NewStyle().Foreground(barColor)
	emptyStyle := lipgloss.NewStyle().Foreground(colorMuted)

	return style.Render(strings.Repeat("━", filled)) +
		emptyStyle.Render(strings.Repeat("░", empty))
}

// renderCycleBar creates a mini progress bar for cycle progress: [██░░░].
func renderCycleBar(cycle, maxCycles int) string {
	if maxCycles <= 0 {
		return ""
	}
	const barWidth = 5
	filled := cycle * barWidth / maxCycles
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	return "[" +
		lipgloss.NewStyle().Foreground(colorPrimary).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(colorMuted).Render(strings.Repeat("░", empty)) +
		"]"
}

// progressColor returns a color that transitions from blue to green based on ratio (0.0-1.0).
func progressColor(ratio float64) lipgloss.Color {
	switch {
	case ratio >= 0.8:
		return colorSuccess
	case ratio >= 0.4:
		return colorPrimary
	default:
		return colorBlue
	}
}

// budgetColor returns a color that transitions green → yellow → red based on ratio (0.0-1.0).
func budgetColor(ratio float64) lipgloss.Color {
	switch {
	case ratio >= 0.9:
		return colorDanger
	case ratio >= 0.7:
		return colorBudgetWarn
	case ratio >= 0.5:
		return colorAccent
	default:
		return colorSuccess
	}
}
