package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/papapumpkin/quasar/internal/chat"
)

// ChatFocus identifies which panel has keyboard focus.
type ChatFocus int

const (
	// FocusSidebar routes input to the conversation list.
	FocusSidebar ChatFocus = iota
	// FocusChatArea routes input to the chat view (messages + input).
	FocusChatArea
)

// ChatState represents the current interaction state of the chat model.
type ChatState int

const (
	// ChatStateNormal is the default state with sidebar and chat side-by-side.
	ChatStateNormal ChatState = iota
	// ChatStateSearch activates search filtering in the sidebar.
	ChatStateSearch
	// ChatStateDeleteConfirm shows a confirmation overlay before deleting.
	ChatStateDeleteConfirm
)

// sidebarWidth is the fixed width of the conversation sidebar.
const sidebarWidth = 28

// sidebarMinTermWidth is the minimum terminal width to show the sidebar.
// Below this width the sidebar is hidden to preserve chat area space.
const sidebarMinTermWidth = 60

// ChatSidebar holds the state for the conversation list panel.
type ChatSidebar struct {
	Conversations []chat.Conversation
	Cursor        int
	Offset        int    // scroll offset for long lists
	SearchQuery   string // active search filter (empty = no filter)
	filtered      []int  // indices into Conversations matching the search
}

// SelectedConversation returns the currently highlighted conversation, or nil
// if the list is empty.
func (s *ChatSidebar) SelectedConversation() *chat.Conversation {
	list := s.visibleList()
	if len(list) == 0 {
		return nil
	}
	if s.Cursor < 0 || s.Cursor >= len(list) {
		return nil
	}
	return list[s.Cursor]
}

// visibleList returns the conversations that match the current search filter.
// If no filter is active, all conversations are returned.
func (s *ChatSidebar) visibleList() []*chat.Conversation {
	if s.SearchQuery == "" {
		result := make([]*chat.Conversation, len(s.Conversations))
		for i := range s.Conversations {
			result[i] = &s.Conversations[i]
		}
		return result
	}
	result := make([]*chat.Conversation, 0, len(s.filtered))
	for _, idx := range s.filtered {
		if idx < len(s.Conversations) {
			result = append(result, &s.Conversations[idx])
		}
	}
	return result
}

// visibleCount returns the number of conversations visible after filtering.
func (s *ChatSidebar) visibleCount() int {
	if s.SearchQuery == "" {
		return len(s.Conversations)
	}
	return len(s.filtered)
}

// updateFilter recomputes the filtered index list based on the search query.
func (s *ChatSidebar) updateFilter() {
	if s.SearchQuery == "" {
		s.filtered = nil
		return
	}
	q := strings.ToLower(s.SearchQuery)
	s.filtered = s.filtered[:0]
	for i, c := range s.Conversations {
		title := strings.ToLower(c.AutoTitle())
		if strings.Contains(title, q) {
			s.filtered = append(s.filtered, i)
		}
	}
	// Clamp cursor.
	if s.Cursor >= len(s.filtered) {
		s.Cursor = len(s.filtered) - 1
	}
	if s.Cursor < 0 {
		s.Cursor = 0
	}
}

// clampCursor ensures the cursor is within bounds.
func (s *ChatSidebar) clampCursor() {
	count := s.visibleCount()
	if count == 0 {
		s.Cursor = 0
		return
	}
	if s.Cursor >= count {
		s.Cursor = count - 1
	}
	if s.Cursor < 0 {
		s.Cursor = 0
	}
}

// ChatModel is the root BubbleTea model for chat mode. It composes a
// sidebar (conversation list) and a chat view (message thread + input)
// into a horizontal split layout, managing focus, state transitions,
// and message routing.
type ChatModel struct {
	Sidebar  ChatSidebar
	ChatView ChatView
	Store    chat.Store
	Provider chat.Provider

	Focus ChatFocus
	State ChatState
	Model string // AI model name for display and provider calls

	ActiveConv *chat.Conversation // currently loaded conversation

	Width  int
	Height int

	// deleteTarget holds the conversation ID pending delete confirmation.
	deleteTarget string

	// spinner drives the loading animation.
	spinner spinner.Model

	// ctx is the context for cancelling in-flight AI calls.
	ctx    context.Context
	cancel context.CancelFunc
}

// NewChatModel creates a ChatModel with the given store, provider, and model name.
func NewChatModel(store chat.Store, provider chat.Provider, model string) ChatModel {
	ctx, cancel := context.WithCancel(context.Background())

	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = lipgloss.NewStyle().Foreground(colorBlue)

	return ChatModel{
		ChatView: NewChatView(),
		Store:    store,
		Provider: provider,
		Model:    model,
		Focus:    FocusChatArea,
		State:    ChatStateNormal,
		spinner:  s,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Init implements tea.Model. It loads the conversation list and starts the spinner.
func (m ChatModel) Init() tea.Cmd {
	return tea.Batch(
		m.loadConversationList(),
		m.spinner.Tick,
	)
}

// Update implements tea.Model. It dispatches messages to the appropriate handler
// based on state and focus.
func (m ChatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.recalcLayout()
		return m, nil

	case tea.KeyMsg:
		model, cmd := m.handleKey(msg)
		return model, cmd

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
		var cmd2 tea.Cmd
		m.ChatView.Spinner, cmd2 = m.ChatView.Spinner.Update(msg)
		cmds = append(cmds, cmd2)
		return m, tea.Batch(cmds...)

	case MsgConvListUpdated:
		return m.handleConvListUpdated(msg)

	case MsgConvLoaded:
		return m.handleConvLoaded(msg)

	case MsgChatResponse:
		return m.handleChatResponse(msg)

	case MsgChatDone:
		return m.handleChatDone(msg)

	case MsgConvDeleted:
		return m.handleConvDeleted(msg)

	case MsgConvSaved:
		// Refresh the sidebar after save.
		return m, m.loadConversationList()
	}

	return m, tea.Batch(cmds...)
}

// View implements tea.Model. It renders the sidebar and chat view in a
// horizontal split layout.
func (m ChatModel) View() string {
	if m.Width < MinWidth || m.Height < MinHeight {
		return centerOverlay(
			styleOverlayWarning.Render("Terminal too small"),
			m.Width, m.Height,
		)
	}

	base := m.renderLayout()

	// Overlay: delete confirmation.
	if m.State == ChatStateDeleteConfirm {
		overlay := m.renderDeleteConfirm()
		dimmed := styleOverlayDimmed.Render(base)
		overlayBox := centerOverlay(overlay, m.Width, m.Height)
		return compositeOverlay(dimmed, overlayBox, m.Width, m.Height)
	}

	return base
}

// renderLayout composes the sidebar and chat view into the horizontal split.
func (m ChatModel) renderLayout() string {
	showSidebar := m.Width >= sidebarMinTermWidth

	var sections []string

	if showSidebar {
		sidebar := m.renderSidebar()
		sections = append(sections, sidebar)
	}

	chatArea := m.ChatView.View()
	sections = append(sections, chatArea)

	content := lipgloss.JoinHorizontal(lipgloss.Top, sections...)

	// Footer with keybindings.
	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left,
		content,
		footer,
	)
}

// renderSidebar renders the conversation list panel.
func (m ChatModel) renderSidebar() string {
	w := sidebarWidth
	// Available height for conversation entries: total minus header, search bar, footer.
	h := m.Height - 3 // header (1) + separator (1) + footer hint (1)
	if h < 1 {
		h = 1
	}

	var b strings.Builder

	// Header.
	headerStyle := styleChatTitleBar.Width(w)
	if m.Focus == FocusSidebar {
		b.WriteString(headerStyle.Render(styleChatTitle.Render("Conversations")))
	} else {
		b.WriteString(headerStyle.Render(styleDetailDim.Render("Conversations")))
	}
	b.WriteString("\n")

	// Search bar (when in search state).
	if m.State == ChatStateSearch {
		searchLine := fmt.Sprintf("🔍 %s", m.Sidebar.SearchQuery)
		b.WriteString(styleChatInputBorder.Width(w).Render(searchLine))
		b.WriteString("\n")
		h-- // consume one line for search
	}

	// Conversation list.
	list := m.Sidebar.visibleList()
	if len(list) == 0 {
		empty := styleChatEmpty.Render("  No conversations")
		b.WriteString(empty)
	} else {
		// Clamp offset for scrolling.
		offset := m.Sidebar.Offset
		if offset > len(list)-h {
			offset = len(list) - h
		}
		if offset < 0 {
			offset = 0
		}

		end := offset + h
		if end > len(list) {
			end = len(list)
		}

		for i := offset; i < end; i++ {
			conv := list[i]
			title := conv.AutoTitle()
			title = TruncateWithEllipsis(title, w-4)

			var line string
			if i == m.Sidebar.Cursor {
				indicator := styleSelectionIndicator.Render(selectionIndicator)
				line = indicator + " " + styleRowSelected.Render(title)
				if m.Focus == FocusSidebar {
					line = padToWidth(line, w, colorSelectionBg)
				}
			} else {
				line = "  " + styleRowNormal.Render(title)
			}
			b.WriteString(line)
			if i < end-1 {
				b.WriteString("\n")
			}
		}
	}

	// Pad remaining lines to fill the sidebar height.
	rendered := b.String()
	lineCount := strings.Count(rendered, "\n") + 1
	for lineCount < m.Height-1 {
		rendered += "\n"
		lineCount++
	}

	// Vertical separator on the right edge.
	lines := strings.Split(rendered, "\n")
	sep := styleChatInputBorder.Render("│")
	for i, line := range lines {
		lines[i] = line + sep
	}

	return strings.Join(lines, "\n")
}

// renderDeleteConfirm renders the delete confirmation overlay.
func (m ChatModel) renderDeleteConfirm() string {
	var b strings.Builder

	title := styleOverlayTitle.Foreground(colorDanger).
		Render("⚠  Delete conversation?")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString("This action cannot be undone.")
	b.WriteString("\n\n")
	b.WriteString(styleOverlayHint.Render("[y] Yes, delete    [n] Cancel"))

	return styleOverlayWarning.Render(b.String())
}

// renderFooter renders context-sensitive keybinding hints.
func (m ChatModel) renderFooter() string {
	var parts []string
	add := func(k, desc string) {
		parts = append(parts,
			styleFooterKey.Render(k)+
				styleFooterSep.Render(":")+
				styleFooterDesc.Render(desc))
	}

	switch m.State {
	case ChatStateSearch:
		add("esc", "cancel")
		add("enter", "select")
	case ChatStateDeleteConfirm:
		add("y", "confirm")
		add("n", "cancel")
	default:
		add("tab", "switch focus")
		if m.Focus == FocusSidebar {
			add("j/k", "navigate")
			add("enter", "open")
			add("n", "new chat")
			add("d", "delete")
			add("/", "search")
		} else {
			add("enter", "send")
			add("j/k", "scroll")
		}
		add("q", "quit")
	}

	line := strings.Join(parts, "  ")
	return styleFooter.Width(m.Width).Render(line)
}

// recalcLayout resizes the chat view based on current terminal dimensions
// and sidebar visibility.
func (m *ChatModel) recalcLayout() {
	showSidebar := m.Width >= sidebarMinTermWidth

	chatWidth := m.Width
	if showSidebar {
		chatWidth = m.Width - sidebarWidth - 1 // -1 for separator
	}
	if chatWidth < 20 {
		chatWidth = 20
	}

	// Reserve 1 line for footer.
	chatHeight := m.Height - 1
	if chatHeight < 1 {
		chatHeight = 1
	}

	m.ChatView.SetSize(chatWidth, chatHeight)
}

// handleKey dispatches keyboard input based on current state and focus.
func (m ChatModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys that work regardless of state.
	switch msg.String() {
	case "ctrl+c":
		m.cancel()
		return m, tea.Quit
	}

	// State-specific handlers.
	switch m.State {
	case ChatStateDeleteConfirm:
		return m.handleDeleteConfirmKey(msg)
	case ChatStateSearch:
		return m.handleSearchKey(msg)
	default:
		return m.handleNormalKey(msg)
	}
}

// handleNormalKey handles keys in the default state.
func (m ChatModel) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.String() == "q" && m.Focus == FocusSidebar:
		m.cancel()
		return m, tea.Quit
	case msg.String() == "tab":
		m.toggleFocus()
		return m, nil
	case msg.String() == "h" && !m.ChatView.Input.Focused():
		m.Focus = FocusSidebar
		m.ChatView.Input.Blur()
		return m, nil
	case msg.String() == "l" && m.Focus == FocusSidebar:
		m.Focus = FocusChatArea
		m.ChatView.Input.Focus()
		return m, nil
	}

	if m.Focus == FocusSidebar {
		return m.handleSidebarKey(msg)
	}
	return m.handleChatAreaKey(msg)
}

// handleSidebarKey handles keys when the sidebar has focus.
func (m ChatModel) handleSidebarKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("j", "down"))):
		m.Sidebar.Cursor++
		m.Sidebar.clampCursor()
		m.ensureSidebarVisible()
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
		m.Sidebar.Cursor--
		m.Sidebar.clampCursor()
		m.ensureSidebarVisible()
		return m, nil

	case msg.String() == "enter":
		conv := m.Sidebar.SelectedConversation()
		if conv != nil {
			return m, m.loadConversation(conv.ID)
		}
		return m, nil

	case msg.String() == "n":
		return m.startNewConversation()

	case msg.String() == "d":
		conv := m.Sidebar.SelectedConversation()
		if conv != nil {
			m.State = ChatStateDeleteConfirm
			m.deleteTarget = conv.ID
		}
		return m, nil

	case msg.String() == "/":
		m.State = ChatStateSearch
		m.Sidebar.SearchQuery = ""
		m.ChatView.Input.Blur()
		return m, nil

	case msg.String() == "G":
		count := m.Sidebar.visibleCount()
		if count > 0 {
			m.Sidebar.Cursor = count - 1
			m.ensureSidebarVisible()
		}
		return m, nil

	case msg.String() == "g":
		m.Sidebar.Cursor = 0
		m.Sidebar.Offset = 0
		return m, nil
	}

	return m, nil
}

// handleChatAreaKey handles keys when the chat area has focus.
func (m ChatModel) handleChatAreaKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.String() == "enter" && !m.ChatView.Loading:
		return m.sendMessage()

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+k"))):
		m.ChatView.ScrollUp()
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+j"))):
		m.ChatView.ScrollDown()
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+u"))):
		m.ChatView.ScrollPageUp()
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("ctrl+d"))):
		m.ChatView.ScrollPageDown()
		return m, nil

	default:
		// Forward to text input.
		var cmd tea.Cmd
		m.ChatView.Input, cmd = m.ChatView.Input.Update(msg)
		return m, cmd
	}
}

// handleSearchKey handles keys in the search state.
func (m ChatModel) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.State = ChatStateNormal
		m.Sidebar.SearchQuery = ""
		m.Sidebar.filtered = nil
		m.Sidebar.clampCursor()
		return m, nil

	case "enter":
		m.State = ChatStateNormal
		// Keep the filter active but exit search mode.
		conv := m.Sidebar.SelectedConversation()
		if conv != nil {
			return m, m.loadConversation(conv.ID)
		}
		return m, nil

	case "backspace":
		if len(m.Sidebar.SearchQuery) > 0 {
			m.Sidebar.SearchQuery = m.Sidebar.SearchQuery[:len(m.Sidebar.SearchQuery)-1]
			m.Sidebar.updateFilter()
		}
		return m, nil

	default:
		// Append printable characters to search query.
		if len(msg.String()) == 1 && msg.String()[0] >= ' ' {
			m.Sidebar.SearchQuery += msg.String()
			m.Sidebar.updateFilter()
		}
		return m, nil
	}
}

// handleDeleteConfirmKey handles keys in the delete confirmation state.
func (m ChatModel) handleDeleteConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		id := m.deleteTarget
		m.State = ChatStateNormal
		m.deleteTarget = ""
		return m, m.deleteConversation(id)

	case "n", "N", "esc":
		m.State = ChatStateNormal
		m.deleteTarget = ""
		return m, nil
	}
	return m, nil
}

// toggleFocus switches between sidebar and chat area.
func (m *ChatModel) toggleFocus() {
	if m.Focus == FocusSidebar {
		m.Focus = FocusChatArea
		m.ChatView.Input.Focus()
	} else {
		m.Focus = FocusSidebar
		m.ChatView.Input.Blur()
	}
}

// ensureSidebarVisible adjusts the scroll offset so the cursor is visible.
func (m *ChatModel) ensureSidebarVisible() {
	visibleHeight := m.Height - 3
	if visibleHeight < 1 {
		visibleHeight = 1
	}
	if m.Sidebar.Cursor < m.Sidebar.Offset {
		m.Sidebar.Offset = m.Sidebar.Cursor
	}
	if m.Sidebar.Cursor >= m.Sidebar.Offset+visibleHeight {
		m.Sidebar.Offset = m.Sidebar.Cursor - visibleHeight + 1
	}
}

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

// handleConvListUpdated processes a refreshed conversation list.
func (m ChatModel) handleConvListUpdated(msg MsgConvListUpdated) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		// Log error but don't crash — sidebar stays with old data.
		fmt.Fprintf(m.stderrWriter(), "chat: failed to list conversations: %v\n", msg.Err)
		return m, nil
	}
	m.Sidebar.Conversations = msg.Conversations
	m.Sidebar.clampCursor()
	if m.State == ChatStateSearch {
		m.Sidebar.updateFilter()
	}
	return m, nil
}

// handleConvLoaded processes a loaded conversation.
func (m ChatModel) handleConvLoaded(msg MsgConvLoaded) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		fmt.Fprintf(m.stderrWriter(), "chat: failed to load conversation: %v\n", msg.Err)
		return m, nil
	}

	m.ActiveConv = msg.Conversation
	m.ChatView.Messages = msg.Conversation.Messages
	m.ChatView.Title = msg.Conversation.AutoTitle()
	m.ChatView.ModelTag = msg.Conversation.Model
	m.ChatView.SetLoading(false)
	m.Focus = FocusChatArea
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
		fmt.Fprintf(m.stderrWriter(), "chat: failed to delete conversation: %v\n", msg.Err)
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

// stderrWriter returns a writer suitable for debug output.
// This is a helper to keep import of "os" out of this file; callers
// can substitute in tests.
type stderrW struct{}

func (stderrW) Write(p []byte) (int, error) { return len(p), nil }

func (m ChatModel) stderrWriter() stderrW { return stderrW{} }
