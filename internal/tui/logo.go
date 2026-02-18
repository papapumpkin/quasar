package tui

import "github.com/charmbracelet/lipgloss"

// Logo style definitions for the TUI status bar logo.
var (
	styleLogoJet  = lipgloss.NewStyle().Foreground(colorMutedLight)
	styleLogoCore = lipgloss.NewStyle().Foreground(colorMutedLight)
)

// Logo returns a styled single-line quasar logo for the TUI status bar.
// The design evokes a quasar's bright core with radiating jets.
// Background is inherited from the parent status bar container.
func Logo() string {
	sp := styleLogoJet.Render(" ")
	return styleLogoJet.Render("━━╋━━") +
		sp +
		styleLogoCore.Render("QUASAR") +
		sp +
		styleLogoJet.Render("━━╋━━")
}

// LogoPlain returns the unstyled ASCII logo text for use in README and plain contexts.
func LogoPlain() string {
	return "━━╋━━ QUASAR ━━╋━━"
}
