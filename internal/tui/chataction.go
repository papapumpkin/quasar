package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/papapumpkin/quasar/internal/chat"
)

// startNewConversation creates a new empty conversation and focuses the chat input.
func (m ChatModel) startNewConversation() (tea.Model, tea.Cmd) {
	conv := &chat.Conversation{
		Model:     m.Model,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	m.ActiveConv = conv
	m.ChatView.Messages = nil
	m.ChatView.Title = "New conversation"
	m.ChatView.ModelTag = m.Model
	m.ChatView.SetLoading(false)
	m.ChatView.ClearInput()
	m.Focus = FocusChatArea
	m.InputMode = ChatModeCompose
	m.ChatView.Input.Focus()
	m.recalcLayout()

	return m, nil
}

// sendMessage sends the current input to the AI provider and returns
// a command that delivers streaming response chunks.
func (m ChatModel) sendMessage() (tea.Model, tea.Cmd) {
	text := m.ChatView.InputValue()
	if text == "" {
		return m, nil
	}

	// Create conversation if needed.
	if m.ActiveConv == nil {
		conv := &chat.Conversation{
			Model:     m.Model,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		m.ActiveConv = conv
	}

	// Append user message.
	userMsg := chat.Message{
		Role:      chat.RoleUser,
		Content:   text,
		Timestamp: time.Now(),
	}
	m.ActiveConv.Messages = append(m.ActiveConv.Messages, userMsg)
	m.ChatView.AddMessage(userMsg)
	m.ChatView.ClearInput()
	m.ChatView.SetLoading(true)

	// Update title from first user message.
	m.ActiveConv.AutoTitle()
	m.ChatView.Title = m.ActiveConv.Title

	// Launch streaming inference.
	//
	// TODO: dispatch chunks incrementally for progressive rendering.
	// Currently all chunks are collected into a single MsgChatResponse,
	// making the UX identical to a non-streaming Chat() call. This is
	// acceptable because ClaudeProvider.ChatStream also delivers the full
	// response as one chunk. When true token-by-token streaming is added
	// to the provider, this should be reworked to emit per-chunk messages
	// using a tea.Cmd chain or tea.Program.Send().
	convID := m.ActiveConv.ID
	messages := make([]chat.Message, len(m.ActiveConv.Messages))
	copy(messages, m.ActiveConv.Messages)
	model := m.Model
	ctx := m.ctx
	provider := m.Provider

	cmd := func() tea.Msg {
		chunks, errs := provider.ChatStream(ctx, messages, model)

		var fullResponse strings.Builder
		for chunk := range chunks {
			fullResponse.WriteString(chunk)
		}

		if err := <-errs; err != nil {
			return MsgChatDone{ConversationID: convID, Err: err}
		}

		return MsgChatResponse{
			ConversationID: convID,
			Content:        fullResponse.String(),
		}
	}

	return m, cmd
}

// renameConversation returns a tea.Cmd that updates the title of the selected
// conversation and persists it.
func (m ChatModel) renameConversation(title string) tea.Cmd {
	sel := m.Sidebar.SelectedConversation()
	if sel == nil || m.Store == nil {
		return nil
	}
	store := m.Store
	id := sel.ID
	return func() tea.Msg {
		conv, err := store.Load(id)
		if err != nil {
			return MsgConvSaved{ConversationID: id, Err: err}
		}
		conv.Title = title
		err = store.Save(conv)
		return MsgConvSaved{ConversationID: id, Err: err}
	}
}

// handleConvListUpdated processes a refreshed conversation list.
func (m ChatModel) handleConvListUpdated(msg MsgConvListUpdated) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		// Log error but don't crash — sidebar stays with old data.
		fmt.Fprintf(os.Stderr, "chat: failed to list conversations: %v\n", msg.Err)
		return m, nil
	}
	m.Sidebar.Conversations = msg.Conversations
	m.Sidebar.clampCursor()
	if m.Sidebar.IsSearching() {
		m.Sidebar.updateFilter()
	}
	return m, nil
}

// handleConvLoaded processes a loaded conversation.
func (m ChatModel) handleConvLoaded(msg MsgConvLoaded) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		fmt.Fprintf(os.Stderr, "chat: failed to load conversation: %v\n", msg.Err)
		return m, nil
	}

	m.ActiveConv = msg.Conversation
	m.ChatView.Messages = msg.Conversation.Messages
	m.ChatView.Title = msg.Conversation.AutoTitle()
	m.ChatView.ModelTag = msg.Conversation.Model
	m.ChatView.SetLoading(false)
	m.Focus = FocusChatArea
	m.InputMode = ChatModeCompose
	m.ChatView.Input.Focus()
	m.recalcLayout()
	m.ChatView.ScrollToBottom()

	return m, nil
}

// handleChatResponse processes an AI response chunk.
func (m ChatModel) handleChatResponse(msg MsgChatResponse) (tea.Model, tea.Cmd) {
	if m.ActiveConv == nil {
		return m, nil
	}

	// Append assistant message to conversation.
	assistantMsg := chat.Message{
		Role:      chat.RoleAssistant,
		Content:   msg.Content,
		Timestamp: time.Now(),
	}
	m.ActiveConv.Messages = append(m.ActiveConv.Messages, assistantMsg)
	m.ChatView.AddMessage(assistantMsg)
	m.ChatView.SetLoading(false)

	// Signal inference is done.
	return m, func() tea.Msg {
		return MsgChatDone{ConversationID: msg.ConversationID}
	}
}

// handleChatDone processes the end of an AI inference call.
func (m ChatModel) handleChatDone(msg MsgChatDone) (tea.Model, tea.Cmd) {
	m.ChatView.SetLoading(false)

	if msg.Err != nil {
		// Show error as a system message.
		errMsg := chat.Message{
			Role:      chat.RoleSystem,
			Content:   fmt.Sprintf("Error: %v", msg.Err),
			Timestamp: time.Now(),
		}
		if m.ActiveConv != nil {
			m.ActiveConv.Messages = append(m.ActiveConv.Messages, errMsg)
		}
		m.ChatView.AddMessage(errMsg)
		return m, nil
	}

	// Save conversation to store.
	if m.ActiveConv != nil && m.Store != nil {
		return m, m.saveConversation(m.ActiveConv)
	}

	return m, nil
}

// handleConvDeleted processes a conversation deletion result.
func (m ChatModel) handleConvDeleted(msg MsgConvDeleted) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		fmt.Fprintf(os.Stderr, "chat: failed to delete conversation: %v\n", msg.Err)
		return m, nil
	}

	// If the deleted conversation was active, clear the chat view.
	if m.ActiveConv != nil && m.ActiveConv.ID == msg.ConversationID {
		m.ActiveConv = nil
		m.ChatView.Messages = nil
		m.ChatView.Title = ""
		m.ChatView.ModelTag = ""
		m.ChatView.SetLoading(false)
		m.recalcLayout()
	}

	// Refresh sidebar.
	return m, m.loadConversationList()
}

// loadConversationList returns a tea.Cmd that loads conversations from the store.
func (m ChatModel) loadConversationList() tea.Cmd {
	store := m.Store
	return func() tea.Msg {
		convs, err := store.List()
		return MsgConvListUpdated{Conversations: convs, Err: err}
	}
}

// loadConversation returns a tea.Cmd that loads a specific conversation.
func (m ChatModel) loadConversation(id string) tea.Cmd {
	store := m.Store
	return func() tea.Msg {
		conv, err := store.Load(id)
		return MsgConvLoaded{Conversation: conv, Err: err}
	}
}

// saveConversation returns a tea.Cmd that persists a conversation.
func (m ChatModel) saveConversation(conv *chat.Conversation) tea.Cmd {
	store := m.Store
	return func() tea.Msg {
		err := store.Save(conv)
		return MsgConvSaved{ConversationID: conv.ID, Err: err}
	}
}

// deleteConversation returns a tea.Cmd that deletes a conversation.
func (m ChatModel) deleteConversation(id string) tea.Cmd {
	store := m.Store
	return func() tea.Msg {
		err := store.Delete(id)
		return MsgConvDeleted{ConversationID: id, Err: err}
	}
}
