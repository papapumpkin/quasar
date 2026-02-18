package tui

import (
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

// Program is an alias for tea.Program, exposed so callers don't need
// to import bubbletea directly.
type Program = tea.Program

// NewProgram creates a BubbleTea program for the given mode.
// The program uses the alternate screen buffer for a clean TUI experience.
// If noSplash is true, the binary-star splash animation is skipped.
func NewProgram(mode Mode, noSplash bool, opts ...tea.ProgramOption) *Program {
	model := NewAppModel(mode)
	model.Detail = NewDetailPanel(80, 10)
	if noSplash {
		model.DisableSplash()
	}

	allOpts := []tea.ProgramOption{
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	}
	allOpts = append(allOpts, opts...)

	return tea.NewProgram(model, allOpts...)
}

// NewProgramRaw creates a BubbleTea program with alt-screen enabled
// and no additional options. This is the primary entry point for callers
// that need to hold the program reference (e.g. to create a UIBridge).
func NewProgramRaw(mode Mode) *Program {
	return NewProgram(mode, false)
}

// NewNebulaProgram creates a nebula-mode TUI with the phase table pre-populated.
// This avoids needing to Send a MsgNebulaInit before Run() starts.
// nebulaDir is the path to the nebula directory, used for writing intervention
// files (PAUSE/STOP) from TUI keyboard shortcuts.
// If noSplash is true, the binary-star splash animation is skipped.
func NewNebulaProgram(name string, phases []PhaseInfo, nebulaDir string, noSplash bool) *Program {
	model := NewAppModel(ModeNebula)
	model.Detail = NewDetailPanel(80, 10)
	if noSplash {
		model.DisableSplash()
	}
	model.StatusBar.Name = name
	model.StatusBar.Total = len(phases)
	model.NebulaView.InitPhases(phases)
	model.NebulaDir = nebulaDir
	return tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
}

// NewHomeProgram creates a home-mode TUI with the nebula list pre-populated.
// nebulaeDir is the parent directory containing all nebula subdirectories.
// If noSplash is true, the binary-star splash animation is skipped.
func NewHomeProgram(nebulaeDir string, choices []NebulaChoice, noSplash bool) *Program {
	model := NewAppModel(ModeHome)
	model.Detail = NewDetailPanel(80, 10)
	if noSplash {
		model.DisableSplash()
	}
	model.HomeNebulae = choices
	model.HomeDir = nebulaeDir
	return tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
}

// Run creates and runs a TUI program, blocking until it exits.
// Returns an error if the program encounters a fatal error.
func Run(mode Mode, noSplash bool) error {
	p := NewProgram(mode, noSplash)
	_, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}

// WithOutput returns a program option that directs TUI output to the given writer.
// Useful for testing or redirecting output.
func WithOutput(w io.Writer) tea.ProgramOption {
	return tea.WithOutput(w)
}
