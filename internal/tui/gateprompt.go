package tui

import (
	"fmt"
	"strings"

	"github.com/aaronsalm/quasar/internal/nebula"
)

// GateOption represents one selectable action in the gate prompt.
type GateOption struct {
	Label  string
	Action nebula.GateAction
}

// GatePrompt renders an overlay for gate decisions.
type GatePrompt struct {
	PhaseID    string
	Options    []GateOption
	Cursor     int
	ResponseCh chan<- nebula.GateAction
	IsPlan     bool
	Width      int
}

// NewGatePrompt creates a gate prompt for the given checkpoint.
func NewGatePrompt(cp *nebula.Checkpoint, responseCh chan<- nebula.GateAction) *GatePrompt {
	isPlan := cp != nil && cp.PhaseID == nebula.PlanPhaseID
	phaseID := "unknown"
	if cp != nil {
		phaseID = cp.PhaseID
	}

	var options []GateOption
	if isPlan {
		options = []GateOption{
			{Label: "[a]ccept", Action: nebula.GateActionAccept},
			{Label: "[s]kip", Action: nebula.GateActionSkip},
		}
	} else {
		options = []GateOption{
			{Label: "[a]ccept", Action: nebula.GateActionAccept},
			{Label: "[x] reject", Action: nebula.GateActionReject},
			{Label: "[r]etry", Action: nebula.GateActionRetry},
			{Label: "[s]kip", Action: nebula.GateActionSkip},
		}
	}

	return &GatePrompt{
		PhaseID:    phaseID,
		Options:    options,
		ResponseCh: responseCh,
		IsPlan:     isPlan,
	}
}

// Resolve sends the selected action and closes the response channel.
func (g *GatePrompt) Resolve(action nebula.GateAction) {
	if g.ResponseCh != nil {
		g.ResponseCh <- action
	}
}

// MoveLeft moves cursor left.
func (g *GatePrompt) MoveLeft() {
	if g.Cursor > 0 {
		g.Cursor--
	}
}

// MoveRight moves cursor right.
func (g *GatePrompt) MoveRight() {
	if g.Cursor < len(g.Options)-1 {
		g.Cursor++
	}
}

// SelectedAction returns the currently highlighted action.
func (g *GatePrompt) SelectedAction() nebula.GateAction {
	if g.Cursor < 0 || g.Cursor >= len(g.Options) {
		return nebula.GateActionAccept
	}
	return g.Options[g.Cursor].Action
}

// View renders the gate prompt overlay.
func (g GatePrompt) View() string {
	var b strings.Builder

	header := fmt.Sprintf("Gate: %s", g.PhaseID)
	b.WriteString(styleGateAction.Render(header))
	b.WriteString("\n\n")

	var optParts []string
	for i, opt := range g.Options {
		if i == g.Cursor {
			optParts = append(optParts, styleGateSelected.Render(opt.Label))
		} else {
			optParts = append(optParts, styleGateNormal.Render(opt.Label))
		}
	}
	b.WriteString(strings.Join(optParts, "  "))

	return styleGateOverlay.Render(b.String())
}
