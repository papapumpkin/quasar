package tui

import (
	"context"
	"fmt"
	"os"
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

// ChatInputMode controls how keyboard input is interpreted in the chat area.
type ChatInputMode int

const (
	// ChatModeNormal interprets keys as vim-style navigation commands.
	ChatModeNormal ChatInputMode = iota
	// ChatModeCompose routes keys to the text input for message authoring.
	ChatModeCompose
)

// defaultModels is the built-in list of models available for {/} cycling.
var defaultModels = []string{
	"claude-sonnet-4-20250514",
	"claude-haiku-4-20250414",
	"claude-opus-4-20250514",
}

// sidebarMinTermWidth is the minimum terminal width to show the sidebar.
// Below this width the sidebar is hidden to preserve chat area space.
const sidebarMinTermWidth = 60

// ChatModel is the root BubbleTea model for chat mode. It composes a
// sidebar (conversation list) and a chat view (message thread + input)
// into a horizontal split layout, managing focus, state transitions,
// and message routing.
type ChatModel struct {
	Sidebar  ChatSidebar
	ChatView ChatView
	Store    chat.Store
	Provider chat.Provider

	Focus     ChatFocus
	InputMode ChatInputMode
	Model     string // AI model name for display and provider calls

	// Models is the list of available model names for {/} cycling.
	Models     []string
	modelIndex int // index into Models for the current selection

	ActiveConv *chat.Conversation // currently loaded conversation

	Width  int
	Height int

	// titleEdit holds the in-progress title string during inline editing.
	titleEdit string
	// titleEditing is true when the user is editing a sidebar title.
	titleEditing bool

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

	models := make([]string, len(defaultModels))
	copy(models, defaultModels)

	// Find the initial model in the list, or prepend it.
	idx := 0
	found := false
	for i, name := range models {
		if name == model {
			idx = i
			found = true
			break
		}
	}
	if !found && model != "" {
		models = append([]string{model}, models...)
		idx = 0
	}

	return ChatModel{
		Sidebar:    NewChatSidebar(),
		ChatView:   NewChatView(),
		Store:      store,
		Provider:   provider,
		Model:      model,
		Models:     models,
		modelIndex: idx,
		InputMode:  ChatModeCompose,
		Focus:      FocusChatArea,
		spinner:    s,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// cycleModel moves through the model list by dir (+1 forward, -1 backward)
// and updates the display tag.
func (m *ChatModel) cycleModel(dir int) {
	if len(m.Models) == 0 {
		return
	}
	m.modelIndex = (m.modelIndex + dir + len(m.Models)) % len(m.Models)
	m.Model = m.Models[m.modelIndex]
	m.ChatView.ModelTag = m.Model
	if m.ActiveConv != nil {
		m.ActiveConv.Model = m.Model
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
// horizontal split layout. Delete confirmation is shown as a centered overlay.
func (m ChatModel) View() string {
	if m.Width < MinWidth || m.Height < MinHeight {
		return centerOverlay(
			styleOverlayWarning.Render("Terminal too small"),
			m.Width, m.Height,
		)
	}

	if m.Sidebar.IsConfirmingDelete() {
		return m.renderDeleteOverlay()
	}

	return m.renderLayout()
}

// renderDeleteOverlay renders a centered delete confirmation dialog.
func (m ChatModel) renderDeleteOverlay() string {
	sel := m.Sidebar.SelectedConversation()
	var text string
	if sel != nil {
		title := TruncateWithEllipsis(sel.AutoTitle(), 30)
		text = fmt.Sprintf("Delete conversation %q?\n\n  y:yes  n:cancel", title)
	} else {
		text = "Delete conversation?\n\n  y:yes  n:cancel"
	}
	return centerOverlay(
		styleOverlayWarning.Render(text),
		m.Width, m.Height,
	)
}

// renderLayout composes the sidebar and chat view into the horizontal split.
func (m ChatModel) renderLayout() string {
	showSidebar := m.Width >= sidebarMinTermWidth && !m.Sidebar.Collapsed

	var sections []string

	if showSidebar {
		sidebarView := m.Sidebar.View()
		// Append a vertical separator to each line.
		lines := strings.Split(sidebarView, "\n")
		sep := lipgloss.NewStyle().Foreground(colorMuted).Render("│")
		for i := range lines {
			lines[i] = lines[i] + sep
		}
		sections = append(sections, strings.Join(lines, "\n"))
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

// renderFooter renders context-sensitive keybinding hints.
func (m ChatModel) renderFooter() string {
	var parts []string
	add := func(k, desc string) {
		parts = append(parts,
			styleFooterKey.Render(k)+
				styleFooterSep.Render(":")+
				styleFooterDesc.Render(desc))
	}

	switch {
	case m.Sidebar.IsSearching():
		add("esc", "cancel")
		add("enter", "select")
	case m.Sidebar.IsConfirmingDelete():
		add("y", "confirm")
		add("n", "cancel")
	default:
		add("{/}", "model")
		if m.Focus == FocusSidebar {
			add("h/l", "focus")
			add("j/k", "navigate")
			add("enter", "open")
			add("n", "new chat")
			add("d", "delete")
			add("/", "search")
			add("t", "rename")
			add("q", "quit")
		} else if m.InputMode == ChatModeCompose {
			add("enter", "send")
			add("esc", "normal")
		} else {
			add("i", "compose")
			add("j/k", "scroll")
			add("g/G", "top/bot")
			add("h", "sidebar")
			add("q", "quit")
		}
	}

	line := strings.Join(parts, "  ")
	return styleFooter.Width(m.Width).Render(line)
}

// recalcLayout resizes the sidebar and chat view based on current terminal
// dimensions and sidebar visibility.
func (m *ChatModel) recalcLayout() {
	showSidebar := m.Width >= sidebarMinTermWidth && !m.Sidebar.Collapsed

	// Reserve 1 line for footer.
	contentHeight := m.Height - 1
	if contentHeight < 1 {
		contentHeight = 1
	}

	chatWidth := m.Width
	if showSidebar {
		sw := SidebarCalcWidth(m.Width)
		m.Sidebar.SetSize(sw, contentHeight)
		chatWidth = m.Width - sw - 1 // -1 for separator
	}
	if chatWidth < 20 {
		chatWidth = 20
	}

	m.ChatView.SetSize(chatWidth, contentHeight)
}

// handleKey dispatches keyboard input based on current state and focus.
func (m ChatModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys that work regardless of state.
	switch msg.String() {
	case "ctrl+c":
		m.cancel()
		return m, tea.Quit
	}

	// Title editing intercepts all keys.
	if m.titleEditing {
		return m.handleTitleEditKey(msg)
	}

	// Mode-specific handlers.
	switch {
	case m.Sidebar.IsConfirmingDelete():
		return m.handleDeleteConfirmKey(msg)
	case m.Sidebar.IsSearching():
		return m.handleSearchKey(msg)
	default:
		return m.handleNormalKey(msg)
	}
}

// handleNormalKey handles keys in the default state.
func (m ChatModel) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Model cycling works regardless of focus.
	switch msg.String() {
	case "}":
		m.cycleModel(1)
		return m, nil
	case "{":
		m.cycleModel(-1)
		return m, nil
	}

	switch {
	case msg.String() == "q" && m.Focus == FocusSidebar:
		m.cancel()
		return m, tea.Quit
	case msg.String() == "q" && m.Focus == FocusChatArea && m.InputMode == ChatModeNormal:
		m.cancel()
		return m, tea.Quit
	case msg.String() == "tab":
		m.toggleFocus()
		return m, nil
	case msg.String() == "h" && m.InputMode == ChatModeNormal && m.Focus == FocusChatArea:
		m.Focus = FocusSidebar
		m.ChatView.Input.Blur()
		return m, nil
	case msg.String() == "l" && m.Focus == FocusSidebar:
		m.Focus = FocusChatArea
		m.InputMode = ChatModeCompose
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
		m.Sidebar.MoveDown()
		return m, nil

	case key.Matches(msg, key.NewBinding(key.WithKeys("k", "up"))):
		m.Sidebar.MoveUp()
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
		m.Sidebar.RequestDelete()
		return m, nil

	case msg.String() == "/":
		m.Sidebar.EnterSearch()
		m.ChatView.Input.Blur()
		return m, nil

	case msg.String() == "G":
		count := m.Sidebar.visibleCount()
		if count > 0 {
			m.Sidebar.Cursor = count - 1
		}
		return m, nil

	case msg.String() == "g":
		m.Sidebar.Cursor = 0
		m.Sidebar.Offset = 0
		return m, nil

	case msg.String() == "t":
		m.startTitleEdit()
		return m, nil
	}

	return m, nil
}

// handleChatAreaKey handles keys when the chat area has focus. It dispatches
// to compose or normal mode handlers based on InputMode.
func (m ChatModel) handleChatAreaKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.InputMode == ChatModeNormal {
		return m.handleChatNormalKey(msg)
	}
	return m.handleComposeKey(msg)
}

// handleComposeKey handles keys in compose mode (text input focused).
func (m ChatModel) handleComposeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc:
		m.InputMode = ChatModeNormal
		m.ChatView.Input.Blur()
		return m, nil

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

// handleChatNormalKey handles keys in chat normal (vim navigation) mode.
func (m ChatModel) handleChatNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "i":
		m.InputMode = ChatModeCompose
		m.ChatView.Input.Focus()
		return m, nil

	case "j":
		m.ChatView.ScrollDown()
		return m, nil

	case "k":
		m.ChatView.ScrollUp()
		return m, nil

	case "G":
		m.ChatView.ScrollToBottom()
		return m, nil

	case "g":
		m.ChatView.ScrollToTop()
		return m, nil

	case "ctrl+u":
		m.ChatView.ScrollPageUp()
		return m, nil

	case "ctrl+d":
		m.ChatView.ScrollPageDown()
		return m, nil
	}

	return m, nil
}

// handleSearchKey handles keys when the sidebar is in search mode.
func (m ChatModel) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.Sidebar.ExitSearch()
		return m, nil

	case "enter":
		// Capture selection before exiting search (which resets cursor).
		conv := m.Sidebar.SelectedConversation()
		m.Sidebar.ExitSearch()
		if conv != nil {
			return m, m.loadConversation(conv.ID)
		}
		return m, nil

	case "backspace":
		m.Sidebar.SearchBackspace()
		return m, nil

	default:
		// Append printable characters to search query.
		if len(msg.String()) == 1 && msg.String()[0] >= ' ' {
			m.Sidebar.SearchInput(rune(msg.String()[0]))
		}
		return m, nil
	}
}

// handleDeleteConfirmKey handles keys when the sidebar is confirming deletion.
func (m ChatModel) handleDeleteConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		id := m.Sidebar.ConfirmDelete()
		if id != "" {
			return m, m.deleteConversation(id)
		}
		return m, nil

	case "n", "N", "esc":
		m.Sidebar.CancelDelete()
		return m, nil
	}
	return m, nil
}

// handleTitleEditKey handles keys during inline title editing.
func (m ChatModel) handleTitleEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.titleEditing = false
		m.titleEdit = ""
		return m, nil

	case tea.KeyEnter:
		m.titleEditing = false
		title := strings.TrimSpace(m.titleEdit)
		m.titleEdit = ""
		if title == "" {
			return m, nil
		}
		return m, m.renameConversation(title)

	case tea.KeyBackspace:
		if len(m.titleEdit) > 0 {
			runes := []rune(m.titleEdit)
			m.titleEdit = string(runes[:len(runes)-1])
		}
		return m, nil

	default:
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			m.titleEdit += string(msg.Runes)
		}
		return m, nil
	}
}

// startTitleEdit begins inline title editing for the selected sidebar item.
func (m *ChatModel) startTitleEdit() {
	sel := m.Sidebar.SelectedConversation()
	if sel == nil {
		return
	}
	m.titleEditing = true
	m.titleEdit = sel.AutoTitle()
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

// toggleFocus switches between sidebar and chat area.
func (m *ChatModel) toggleFocus() {
	if m.Focus == FocusSidebar {
		m.Focus = FocusChatArea
		m.InputMode = ChatModeCompose
		m.ChatView.Input.Focus()
	} else {
		m.Focus = FocusSidebar
		m.InputMode = ChatModeNormal
		m.ChatView.Input.Blur()
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
