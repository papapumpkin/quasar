package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/papapumpkin/quasar/internal/dialogue"
	"github.com/papapumpkin/quasar/internal/nebula"
)

func TestDialogueOverlayTextInput(t *testing.T) {
	sess := dialogue.NewSession(dialogue.Request{Title: "test"})
	overlay := NewDialogueOverlay(sess)

	if !overlay.Input.Focused() {
		t.Fatal("textinput should be focused after creation")
	}
	if overlay.Mode != DialogueModeCompose {
		t.Fatal("mode should be compose")
	}

	keys := DefaultKeyMap()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	action, _ := overlay.HandleKey(msg, keys)
	if action != DialogueNone {
		t.Fatalf("expected DialogueNone, got %d", action)
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

func TestDialogueModelKeyRouting(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.DisableSplash()
	m.Width = 120
	m.Height = 40

	sess := dialogue.NewSession(dialogue.Request{
		Title:   "test escalation",
		Context: "some context",
		Kind:    "escalation",
		Options: []string{"retry", "skip", "fail"},
	})

	// Open dialogue via message.
	result, _ := m.Update(MsgDialogueOpen{Session: sess})
	m = result.(AppModel)

	if m.Dialogue == nil {
		t.Fatal("dialogue overlay should be set after MsgDialogueOpen")
	}
	if !m.Dialogue.Input.Focused() {
		t.Fatal("dialogue textinput should be focused")
	}

	// Send a character key through the full Update path.
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}
	result, _ = m.Update(keyMsg)
	m = result.(AppModel)

	if m.Dialogue == nil {
		t.Fatal("dialogue should still be open after typing")
	}
	if m.Dialogue.Input.Value() != "h" {
		t.Fatalf("expected input value 'h', got %q", m.Dialogue.Input.Value())
	}

	// Type more characters.
	for _, ch := range "ello" {
		keyMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}}
		result, _ = m.Update(keyMsg)
		m = result.(AppModel)
	}

	if m.Dialogue.Input.Value() != "hello" {
		t.Fatalf("expected input value 'hello', got %q", m.Dialogue.Input.Value())
	}

	// Verify Enter sends the message.
	enterMsg := tea.KeyMsg{Type: tea.KeyEnter}
	result, _ = m.Update(enterMsg)
	m = result.(AppModel)

	if m.Dialogue.Input.Value() != "" {
		t.Fatalf("expected empty input after Enter, got %q", m.Dialogue.Input.Value())
	}
	if len(m.Dialogue.Messages) != 1 {
		t.Fatalf("expected 1 message after Enter, got %d", len(m.Dialogue.Messages))
	}
	if m.Dialogue.Messages[0].Content != "hello" {
		t.Fatalf("expected message 'hello', got %q", m.Dialogue.Messages[0].Content)
	}
}

func TestDialoguePriorityOverGate(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.DisableSplash()
	m.Width = 120
	m.Height = 40

	// Activate a gate prompt (simulating a completed phase awaiting review).
	responseCh := make(chan nebula.GateAction, 1)
	m.Gate = NewGatePrompt(&nebula.Checkpoint{PhaseID: "phase-a"}, responseCh)
	m.Gate.Width = 100
	m.Gate.Height = 40

	// Now open a dialogue (simulating an escalation on a different phase).
	sess := dialogue.NewSession(dialogue.Request{
		Title:   "escalation",
		Kind:    "escalation",
		Options: []string{"retry", "skip", "fail"},
	})
	result, _ := m.Update(MsgDialogueOpen{Session: sess})
	m = result.(AppModel)

	if m.Dialogue == nil {
		t.Fatal("dialogue should be open")
	}
	if m.Gate == nil {
		t.Fatal("gate should still be active")
	}

	// Type 'a' — this should go to the dialogue textinput, NOT resolve the gate.
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	result, _ = m.Update(keyMsg)
	m = result.(AppModel)

	if m.Dialogue == nil {
		t.Fatal("dialogue should still be open after typing 'a'")
	}
	if m.Gate == nil {
		t.Fatal("gate should NOT have been resolved by typing in the dialogue")
	}
	if m.Dialogue.Input.Value() != "a" {
		t.Fatalf("expected dialogue input 'a', got %q", m.Dialogue.Input.Value())
	}
}

func TestDialogueAgentMessages(t *testing.T) {
	m := NewAppModel(ModeNebula)
	m.DisableSplash()
	m.Width = 120
	m.Height = 40

	sess := dialogue.NewSession(dialogue.Request{Title: "test"})

	result, _ := m.Update(MsgDialogueOpen{Session: sess})
	m = result.(AppModel)

	agentMsg := dialogue.Message{
		Role:    dialogue.RoleAgent,
		Content: "Phase blocked: missing dependency",
	}
	result, _ = m.Update(MsgDialogueAgentMsg{SessionID: sess.ID(), Message: agentMsg})
	m = result.(AppModel)

	if m.Dialogue == nil {
		t.Fatal("dialogue should still be open")
	}
	if len(m.Dialogue.Messages) != 1 {
		t.Fatalf("expected 1 agent message, got %d", len(m.Dialogue.Messages))
	}
	if m.Dialogue.Messages[0].Content != "Phase blocked: missing dependency" {
		t.Fatalf("unexpected message content: %q", m.Dialogue.Messages[0].Content)
	}
}
