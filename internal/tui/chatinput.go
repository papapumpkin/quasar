package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

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
