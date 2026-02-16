package tui

import "github.com/charmbracelet/lipgloss"

// Logo style definitions for the TUI status bar logo.
var (
	styleLogoJet  = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleLogoCore = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
)

// Logo returns a styled single-line quasar logo for the TUI status bar.
// The design evokes a quasar's bright core with radiating jets.
func Logo() string {
	return styleLogoJet.Render("━━╋━━") +
		" " +
		styleLogoCore.Render("QUASAR") +
		" " +
		styleLogoJet.Render("━━╋━━")
}

// LogoPlain returns the unstyled ASCII logo text for use in README and plain contexts.
func LogoPlain() string {
	return "━━╋━━ QUASAR ━━╋━━"
}
