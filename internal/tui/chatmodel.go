package tui

import (
	"context"
	"fmt"
	"strings"

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

	// spinner drives the loading animation.
	spinner spinner.Model

	// ctx is the context for cancelling in-flight AI calls.
	ctx    context.Context
	cancel context.CancelFunc

	// streaming is true while chunk messages are being received from
	// the provider. streamChunks and streamErrs hold the active channel
	// pair returned by Provider.ChatStream.
	streaming    bool
	streamChunks <-chan string
	streamErrs   <-chan error
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

	cv := NewChatView()
	cv.ModelTag = model
	cv.ModelIndex = idx
	cv.ModelCount = len(models)

	return ChatModel{
		Sidebar:    NewChatSidebar(),
		ChatView:   cv,
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
// and updates the display tag and position indicator.
func (m *ChatModel) cycleModel(dir int) {
	if len(m.Models) == 0 {
		return
	}
	m.modelIndex = (m.modelIndex + dir + len(m.Models)) % len(m.Models)
	m.Model = m.Models[m.modelIndex]
	m.ChatView.ModelTag = m.Model
	m.ChatView.ModelIndex = m.modelIndex
	m.ChatView.ModelCount = len(m.Models)
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

	case MsgChatChunk:
		return m.handleChatChunk(msg)

	case MsgChatResponse:
		return m.handleChatResponse(msg)

	case MsgChatDone:
		return m.handleChatDone(msg)

	case MsgChatError:
		return m.handleChatError(msg)

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
