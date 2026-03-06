package tui

import (
	"context"

	"github.com/papapumpkin/quasar/internal/dialogue"
)

// DialogueBridge implements dialogue.Opener for the TUI display path.
// It creates a MemSession and sends MsgDialogueOpen to the TUI program,
// which opens the dialogue overlay.
type DialogueBridge struct {
	program *Program
}

// NewDialogueBridge creates a DialogueBridge that sends dialogue events
// to the given Bubbletea program.
func NewDialogueBridge(program *Program) *DialogueBridge {
	return &DialogueBridge{program: program}
}

// Open creates a new dialogue session and sends it to the TUI for display.
func (b *DialogueBridge) Open(_ context.Context, req dialogue.Request) (dialogue.Session, error) {
	sess := dialogue.NewSession(req)
	b.program.Send(MsgDialogueOpen{Session: sess})
	return sess, nil
}
