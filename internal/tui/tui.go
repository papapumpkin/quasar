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
func NewProgram(mode Mode, opts ...tea.ProgramOption) *Program {
	model := NewAppModel(mode)
	model.Detail = NewDetailPanel(80, 10)

	allOpts := []tea.ProgramOption{
		tea.WithAltScreen(),
	}
	allOpts = append(allOpts, opts...)

	return tea.NewProgram(model, allOpts...)
}

// NewProgramRaw creates a BubbleTea program with alt-screen enabled
// and no additional options. This is the primary entry point for callers
// that need to hold the program reference (e.g. to create a UIBridge).
func NewProgramRaw(mode Mode) *Program {
	return NewProgram(mode)
}

// NewNebulaProgram creates a nebula-mode TUI with the phase table pre-populated.
// This avoids needing to Send a MsgNebulaInit before Run() starts.
func NewNebulaProgram(name string, phases []PhaseInfo) *Program {
	model := NewAppModel(ModeNebula)
	model.Detail = NewDetailPanel(80, 10)
	model.StatusBar.Name = name
	model.StatusBar.Total = len(phases)
	model.NebulaView.InitPhases(phases)
	return tea.NewProgram(model, tea.WithAltScreen())
}

// Run creates and runs a TUI program, blocking until it exits.
// Returns an error if the program encounters a fatal error.
func Run(mode Mode) error {
	p := NewProgram(mode)
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
