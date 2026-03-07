package tui

import (
	"fmt"
	"time"

	"github.com/papapumpkin/quasar/internal/chat"
)

// StartPhaseChat creates a new conversation pre-seeded with phase execution
// context. Called when the user presses the contextual chat keybinding on the
// board view. The phase context is injected as a system message at the top of
// the conversation.
func (m *ChatModel) StartPhaseChat(pc chat.PhaseContext) {
	// Cancel any in-flight inference from the previous conversation.
	if m.streaming || m.ChatView.Loading {
		m.cancel()
		m.resetStreamingState()
	}

	cb := chat.NewContextBuilder()
	contextMsg := cb.Build(pc)

	now := time.Now()
	sysMsg := chat.Message{
		Role:      chat.RoleSystem,
		Content:   contextMsg,
		Timestamp: now,
	}

	conv := &chat.Conversation{
		Model:     m.Model,
		PhaseID:   pc.PhaseID,
		CreatedAt: now,
		UpdatedAt: now,
		Messages:  []chat.Message{sysMsg},
	}

	m.ActiveConv = conv
	m.PhaseContext = &pc
	m.ChatView.Messages = conv.Messages
	m.ChatView.Title = fmt.Sprintf("Chat: %s", pc.PhaseID)
	m.ChatView.PhaseBadge = pc.PhaseID
	m.ChatView.ModelTag = m.Model
	m.ChatView.ModelIndex = m.modelIndex
	m.ChatView.ModelCount = len(m.Models)
	m.ChatView.SetLoading(false)
	m.ChatView.ClearInput()
	m.ChatView.RefreshContent()
	m.Focus = FocusChatArea
	m.InputMode = ChatModeCompose
	m.ChatView.Input.Focus()
	m.recalcLayout()
}

// RefreshPhaseContext re-fetches the latest phase execution state and appends
// it as a new system message to the active conversation. This keeps
// long-running contextual conversations current.
func (m *ChatModel) RefreshPhaseContext(pc chat.PhaseContext) {
	if m.ActiveConv == nil {
		return
	}

	cb := chat.NewContextBuilder()
	contextMsg := cb.Build(pc)

	sysMsg := chat.Message{
		Role:      chat.RoleSystem,
		Content:   "[Context Refresh]\n\n" + contextMsg,
		Timestamp: time.Now(),
	}

	m.ActiveConv.Messages = append(m.ActiveConv.Messages, sysMsg)
	m.PhaseContext = &pc
	m.ChatView.AddMessage(sysMsg)
}
