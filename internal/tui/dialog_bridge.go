package tui

import (
	"context"

	"github.com/papapumpkin/quasar/internal/dialog"
)

// DialogBridge implements dialog.Opener for the TUI display path.
// It creates a MemSession and sends MsgDialogOpen to the TUI program,
// which opens the dialog overlay.
type DialogBridge struct {
	program *Program
}

// NewDialogBridge creates a DialogBridge that sends dialog events
// to the given Bubbletea program.
func NewDialogBridge(program *Program) *DialogBridge {
	return &DialogBridge{program: program}
}

// Open creates a new dialog session and sends it to the TUI for display.
func (b *DialogBridge) Open(_ context.Context, req dialog.Request) (dialog.Session, error) {
	sess := dialog.NewSession(req)
	b.program.Send(MsgDialogOpen{Session: sess})
	return sess, nil
}
