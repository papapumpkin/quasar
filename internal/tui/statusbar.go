package tui

import (
	"fmt"
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
}

// View renders the status bar as a single line.
func (s StatusBar) View() string {
	var left string
	if s.Total > 0 {
		// Nebula mode.
		left = fmt.Sprintf(" QUASAR  nebula: %s  %d/%d done", s.Name, s.Completed, s.Total)
	} else if s.BeadID != "" {
		// Loop mode.
		left = fmt.Sprintf(" QUASAR  task %s  cycle %d/%d", s.BeadID, s.Cycle, s.MaxCycles)
	} else {
		left = " QUASAR"
	}

	var right string
	if s.BudgetUSD > 0 {
		right = fmt.Sprintf("$%.2f / $%.2f", s.CostUSD, s.BudgetUSD)
	} else {
		right = fmt.Sprintf("$%.2f", s.CostUSD)
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
	padding := ""
	for range gap {
		padding += " "
	}

	line := left + padding + right
	return styleStatusBar.Width(s.Width).Render(line)
}
