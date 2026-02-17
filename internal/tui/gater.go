package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/papapumpkin/quasar/internal/nebula"
)

// Gater implements nebula.Gater by sending a gate prompt message
// to the BubbleTea program and blocking until the user responds.
type Gater struct {
	program *tea.Program
}

// Verify Gater satisfies nebula.Gater at compile time.
var _ nebula.Gater = (*Gater)(nil)

// NewGater creates a Gater that routes gate decisions through the TUI.
func NewGater(p *tea.Program) *Gater {
	return &Gater{program: p}
}

// Prompt sends a gate prompt to the TUI and blocks until the user responds
// or the context is canceled.
func (g *Gater) Prompt(ctx context.Context, cp *nebula.Checkpoint) (nebula.GateAction, error) {
	responseCh := make(chan nebula.GateAction, 1)

	g.program.Send(MsgGatePrompt{
		Checkpoint: cp,
		ResponseCh: responseCh,
	})

	select {
	case <-ctx.Done():
		return nebula.GateActionSkip, ctx.Err()
	case action := <-responseCh:
		return action, nil
	}
}
