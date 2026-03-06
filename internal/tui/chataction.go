package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	m.ChatView.ModelIndex = m.modelIndex
	m.ChatView.ModelCount = len(m.Models)
	m.ChatView.SetLoading(false)
	m.ChatView.ClearInput()
	m.Focus = FocusChatArea
	m.InputMode = ChatModeCompose
	m.ChatView.Input.Focus()
	m.recalcLayout()

	return m, nil
}

// sendMessage sends the current input to the AI provider and returns
// a command that streams response chunks incrementally via MsgChatChunk
// messages. Each chunk handler reads the next token from the provider's
// channel, enabling real-time progressive rendering.
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

	// Create a fresh context for this inference call so that Ctrl+C
	// cancellation only affects the current request.
	ctx, cancel := context.WithCancel(context.Background())
	m.ctx = ctx
	m.cancel = cancel

	// Start streaming inference and store the channels for incremental
	// reading via readNextChunk.
	messages := make([]chat.Message, len(m.ActiveConv.Messages))
	copy(messages, m.ActiveConv.Messages)

	chunks, errs := m.Provider.ChatStream(ctx, messages, m.Model)
	m.streamChunks = chunks
	m.streamErrs = errs
	m.streaming = true

	return m, m.readNextChunk()
}

// readNextChunk returns a tea.Cmd that reads the next token from the
// active stream channels. It yields MsgChatChunk for each token,
// MsgChatError if the provider reports an error, or MsgChatDone when
// the stream completes successfully.
func (m ChatModel) readNextChunk() tea.Cmd {
	chunks := m.streamChunks
	errs := m.streamErrs
	convID := ""
	if m.ActiveConv != nil {
		convID = m.ActiveConv.ID
	}

	return func() tea.Msg {
		chunk, ok := <-chunks
		if ok {
			return MsgChatChunk{ConversationID: convID, Content: chunk}
		}
		// Chunks channel closed — check for error.
		if err := <-errs; err != nil {
			return MsgChatError{ConversationID: convID, Err: err}
		}
		return MsgChatDone{ConversationID: convID}
	}
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

// handleChatChunk processes a streaming token from the AI provider.
// It appends the chunk to the in-progress assistant message (creating
// one if this is the first chunk) and issues a command to read the
// next chunk from the stream.
func (m ChatModel) handleChatChunk(msg MsgChatChunk) (tea.Model, tea.Cmd) {
	if m.ActiveConv == nil {
		return m, nil
	}

	// Transition from "thinking…" to streaming on first chunk.
	if m.ChatView.Loading {
		m.ChatView.SetLoading(false)
		m.ChatView.Streaming = true
	}

	// Append to existing assistant message or create a new one.
	msgs := m.ActiveConv.Messages
	if len(msgs) > 0 && msgs[len(msgs)-1].Role == chat.RoleAssistant && m.streaming {
		m.ActiveConv.Messages[len(msgs)-1].Content += msg.Content
	} else {
		m.ActiveConv.Messages = append(m.ActiveConv.Messages, chat.Message{
			Role:      chat.RoleAssistant,
			Content:   msg.Content,
			Timestamp: time.Now(),
		})
	}

	// Update the chat view with the chunk.
	m.ChatView.AppendStreamChunk(msg.Content)

	return m, m.readNextChunk()
}

// handleChatResponse processes a complete AI response (non-streaming path).
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

// handleChatError processes an inference error. Context cancellation
// errors are treated specially: the partial response is preserved with
// a "[cancelled]" suffix. Other errors are displayed inline as system
// messages.
func (m ChatModel) handleChatError(msg MsgChatError) (tea.Model, tea.Cmd) {
	m.ChatView.SetLoading(false)
	m.ChatView.Streaming = false
	m.streaming = false
	m.streamChunks = nil
	m.streamErrs = nil

	// Renew context so the next request gets a fresh one.
	ctx, cancel := context.WithCancel(context.Background())
	m.ctx = ctx
	m.cancel = cancel

	if errors.Is(msg.Err, context.Canceled) {
		// Preserve partial response with cancelled suffix.
		if m.ActiveConv != nil {
			msgs := m.ActiveConv.Messages
			if len(msgs) > 0 && msgs[len(msgs)-1].Role == chat.RoleAssistant {
				m.ActiveConv.Messages[len(msgs)-1].Content += " [cancelled]"
				// Sync ChatView — replace last message content.
				cvMsgs := m.ChatView.Messages
				if len(cvMsgs) > 0 {
					cvMsgs[len(cvMsgs)-1].Content = m.ActiveConv.Messages[len(msgs)-1].Content
				}
				m.ChatView.RefreshContent()
			}
		}
		// Save partial conversation.
		if m.ActiveConv != nil && m.Store != nil {
			return m, m.saveConversation(m.ActiveConv)
		}
		return m, nil
	}

	// Non-cancellation error — show as system message.
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

// handleChatDone processes the successful end of an AI inference stream.
func (m ChatModel) handleChatDone(msg MsgChatDone) (tea.Model, tea.Cmd) {
	m.ChatView.SetLoading(false)
	m.ChatView.Streaming = false
	m.streaming = false
	m.streamChunks = nil
	m.streamErrs = nil

	// Renew context for next request.
	ctx, cancel := context.WithCancel(context.Background())
	m.ctx = ctx
	m.cancel = cancel

	if msg.Err != nil {
		// Legacy error path — show as system message.
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
