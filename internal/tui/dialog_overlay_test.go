package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/papapumpkin/quasar/internal/dialog"
	"github.com/papapumpkin/quasar/internal/nebula"
)

func TestDialogOverlayTextInput(t *testing.T) {
	sess := dialog.NewSession(dialog.Request{Title: "test"})
	overlay := NewDialogOverlay(sess)

	if !overlay.Input.Focused() {
		t.Fatal("textinput should be focused after creation")
	}
	if overlay.Mode != DialogModeCompose {
		t.Fatal("mode should be compose")
	}

	keys := DefaultKeyMap()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	action, _ := overlay.HandleKey(msg, keys)
	if action != DialogNone {
		t.Fatalf("expected DialogNone, got %d", action)
	}
	if overlay.Input.Value() != "a" {
		t.Fatalf("expected input value 'a', got %q", overlay.Input.Value())
	}

	msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}}
	overlay.HandleKey(msg, keys)
	if overlay.Input.Value() != "ab" {
		t.Fatalf("expected input value 'ab', got %q", overlay.Input.Value())
	}
}

func TestDialogModelKeyRouting(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.DisableSplash()
	m.Width = 120
	m.Height = 40

	sess := dialog.NewSession(dialog.Request{
		Title:   "test escalation",
		Context: "some context",
		Kind:    "escalation",
		Options: []string{"retry", "skip", "fail"},
	})

	// Open dialog via message.
	result, _ := m.Update(MsgDialogOpen{Session: sess})
	m = result.(AppModel)

	if m.Dialog == nil {
		t.Fatal("dialog overlay should be set after MsgDialogOpen")
	}
	if !m.Dialog.Input.Focused() {
		t.Fatal("dialog textinput should be focused")
	}

	// Send a character key through the full Update path.
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}
	result, _ = m.Update(keyMsg)
	m = result.(AppModel)

	if m.Dialog == nil {
		t.Fatal("dialog should still be open after typing")
	}
	if m.Dialog.Input.Value() != "h" {
		t.Fatalf("expected input value 'h', got %q", m.Dialog.Input.Value())
	}

	// Type more characters.
	for _, ch := range "ello" {
		keyMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}}
		result, _ = m.Update(keyMsg)
		m = result.(AppModel)
	}

	if m.Dialog.Input.Value() != "hello" {
		t.Fatalf("expected input value 'hello', got %q", m.Dialog.Input.Value())
	}

	// Verify Enter sends the message.
	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
	result, _ = m.Update(enterMsg)
	m = result.(AppModel)

	if m.Dialog.Input.Value() != "" {
		t.Fatalf("expected empty input after Enter, got %q", m.Dialog.Input.Value())
	}
	if len(m.Dialog.Messages) != 1 {
		t.Fatalf("expected 1 message after Enter, got %d", len(m.Dialog.Messages))
	}
	if m.Dialog.Messages[0].Content != "hello" {
		t.Fatalf("expected message 'hello', got %q", m.Dialog.Messages[0].Content)
	}
}

func TestDialogPriorityOverGate(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.DisableSplash()
	m.Width = 120
	m.Height = 40

	// Activate a gate prompt (simulating a completed phase awaiting review).
	responseCh := make(chan nebula.GateAction, 1)
	m.Gate = NewGatePrompt(&nebula.Checkpoint{PhaseID: "phase-a"}, responseCh)
	m.Gate.Width = 100
	m.Gate.Height = 40

	// Now open a dialog (simulating an escalation on a different phase).
	sess := dialog.NewSession(dialog.Request{
		Title:   "escalation",
		Kind:    "escalation",
		Options: []string{"retry", "skip", "fail"},
	})
	result, _ := m.Update(MsgDialogOpen{Session: sess})
	m = result.(AppModel)

	if m.Dialog == nil {
		t.Fatal("dialog should be open")
	}
	if m.Gate == nil {
		t.Fatal("gate should still be active")
	}

	// Type 'a' — this should go to the dialog textinput, NOT resolve the gate.
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	result, _ = m.Update(keyMsg)
	m = result.(AppModel)

	if m.Dialog == nil {
		t.Fatal("dialog should still be open after typing 'a'")
	}
	if m.Gate == nil {
		t.Fatal("gate should NOT have been resolved by typing in the dialog")
	}
	if m.Dialog.Input.Value() != "a" {
		t.Fatalf("expected dialog input 'a', got %q", m.Dialog.Input.Value())
	}
}

func TestDialogAgentMessages(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.DisableSplash()
	m.Width = 120
	m.Height = 40

	sess := dialog.NewSession(dialog.Request{Title: "test"})

	result, _ := m.Update(MsgDialogOpen{Session: sess})
	m = result.(AppModel)

	agentMsg := dialog.Message{
		Role:    dialog.RoleAgent,
		Content: "Phase blocked: missing dependency",
	}
	result, _ = m.Update(MsgDialogAgentMsg{SessionID: sess.ID(), Message: agentMsg})
	m = result.(AppModel)

	if m.Dialog == nil {
		t.Fatal("dialog should still be open")
	}
	if len(m.Dialog.Messages) != 1 {
		t.Fatalf("expected 1 agent message, got %d", len(m.Dialog.Messages))
	}
	if m.Dialog.Messages[0].Content != "Phase blocked: missing dependency" {
		t.Fatalf("unexpected message content: %q", m.Dialog.Messages[0].Content)
	}
}
