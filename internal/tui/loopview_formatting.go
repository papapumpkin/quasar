package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
)

// formatElapsed returns a human-readable elapsed time string from a start time.
// Returns an empty string if the start time is zero.
func formatElapsed(start time.Time) string {
	if start.IsZero() {
		return ""
	}
	d := time.Since(start).Truncate(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}

// formatDuration returns a human-readable duration between start and end.
// If end is zero, falls back to time.Since(start).
func formatDuration(start, end time.Time) string {
	if start.IsZero() {
		return ""
	}
	var d time.Duration
	if end.IsZero() {
		d = time.Since(start)
	} else {
		d = end.Sub(start)
	}
	d = d.Truncate(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}

// roleColoredSpinner renders the spinner frame with a color matching the agent role.
// Coder gets blue, reviewer gets yellow/gold.
func roleColoredSpinner(role string, s spinner.Model) string {
	frame := s.View()
	switch role {
	case "reviewer":
		return lipgloss.NewStyle().Foreground(colorReviewer).Render(frame)
	default:
		return lipgloss.NewStyle().Foreground(colorBlue).Render(frame)
	}
}
