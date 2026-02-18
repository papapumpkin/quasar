package tui

import "github.com/charmbracelet/lipgloss"

// Logo style definitions for the TUI status bar logo.
var (
	styleLogoJet  = lipgloss.NewStyle().Background(colorSurface).Foreground(colorMutedLight)
	styleLogoCore = lipgloss.NewStyle().Background(colorSurface).Foreground(colorMutedLight)
)

// Logo returns a styled single-line quasar logo for the TUI status bar.
// The design evokes a quasar's bright core with radiating jets.
// All spaces carry colorSurface background to prevent gaps in the status bar.
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
