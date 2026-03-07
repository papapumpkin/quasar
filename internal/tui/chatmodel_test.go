package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/papapumpkin/quasar/internal/chat"
)

// mockStore implements chat.Store for testing.
type mockStore struct {
	conversations []chat.Conversation
	saved         []*chat.Conversation
	deleted       []string
	loadErr       error
	saveErr       error
	deleteErr     error
	listErr       error
}

func (m *mockStore) List() ([]chat.Conversation, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.conversations, nil
}

func (m *mockStore) Load(id string) (*chat.Conversation, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	for i := range m.conversations {
		if m.conversations[i].ID == id {
			conv := m.conversations[i]
			return &conv, nil
		}
	}
	return nil, chat.ErrNotFound
}

func (m *mockStore) Save(conv *chat.Conversation) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	if conv.ID == "" {
		conv.ID = "mock-id"
	}
	conv.UpdatedAt = time.Now()
	m.saved = append(m.saved, conv)
	return nil
}

func (m *mockStore) Delete(id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.deleted = append(m.deleted, id)
	return nil
}

// mockProvider implements chat.Provider for testing.
type mockProvider struct {
	response string
	err      error
}

func (p *mockProvider) Chat(_ context.Context, _ []chat.Message, _ string) (string, error) {
	if p.err != nil {
		return "", p.err
	}
	return p.response, nil
}

func (p *mockProvider) ChatStream(_ context.Context, _ []chat.Message, _ string) (<-chan string, <-chan error) {
	chunks := make(chan string, 1)
	errs := make(chan error, 1)
	if p.err != nil {
		errs <- p.err
	} else {
		chunks <- p.response
	}
	close(chunks)
	close(errs)
	return chunks, errs
}

func newTestChatModel() (ChatModel, *mockStore, *mockProvider) {
	store := &mockStore{}
	provider := &mockProvider{response: "Hello from AI"}
	m := NewChatModel(store, provider, "test-model")
	m.Width = 100
	m.Height = 30
	m.recalcLayout()
	return m, store, provider
}

func TestNewChatModel(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()

	if m.Focus != FocusChatArea {
		t.Errorf("initial focus = %d, want FocusChatArea(%d)", m.Focus, FocusChatArea)
	}
	if m.Sidebar.Mode() != SidebarNormal {
		t.Errorf("initial sidebar mode = %d, want SidebarNormal(%d)", m.Sidebar.Mode(), SidebarNormal)
	}
	if m.Model != "test-model" {
		t.Errorf("model = %q, want %q", m.Model, "test-model")
	}
}

func TestChatModelInit(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	cmd := m.Init()

	if cmd == nil {
		t.Fatal("Init should return a batch command")
	}
}

func TestChatModelWindowResize(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()

	resizeMsg := tea.WindowSizeMsg{Width: 120, Height: 40}
	result, _ := m.Update(resizeMsg)
	updated := result.(ChatModel)

	if updated.Width != 120 {
		t.Errorf("width = %d, want 120", updated.Width)
	}
	if updated.Height != 40 {
		t.Errorf("height = %d, want 40", updated.Height)
	}
}

func TestChatModelToggleFocus(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()

	if m.Focus != FocusChatArea {
		t.Fatal("expected initial focus on chat area")
	}

	// Tab to sidebar.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated := result.(ChatModel)

	if updated.Focus != FocusSidebar {
		t.Errorf("focus after tab = %d, want FocusSidebar(%d)", updated.Focus, FocusSidebar)
	}

	// Tab back to chat area.
	result, _ = updated.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated = result.(ChatModel)

	if updated.Focus != FocusChatArea {
		t.Errorf("focus after second tab = %d, want FocusChatArea(%d)", updated.Focus, FocusChatArea)
	}
}

func TestChatModelVimFocusKeys(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	// Start on sidebar.
	m.Focus = FocusSidebar
	m.ChatView.Input.Blur()

	// 'l' should move to chat area.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	updated := result.(ChatModel)

	if updated.Focus != FocusChatArea {
		t.Errorf("focus after 'l' = %d, want FocusChatArea", updated.Focus)
	}

	// 'h' should move back to sidebar when in normal mode.
	updated.InputMode = ChatModeNormal
	updated.ChatView.Input.Blur()
	result, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	updated = result.(ChatModel)

	if updated.Focus != FocusSidebar {
		t.Errorf("focus after 'h' = %d, want FocusSidebar", updated.Focus)
	}
}

func TestChatModelSidebarNavigation(t *testing.T) {
	t.Parallel()

	m, store, _ := newTestChatModel()
	now := time.Now()
	store.conversations = []chat.Conversation{
		{ID: "c1", Title: "First", UpdatedAt: now},
		{ID: "c2", Title: "Second", UpdatedAt: now.Add(-time.Hour)},
		{ID: "c3", Title: "Third", UpdatedAt: now.Add(-2 * time.Hour)},
	}

	// Load conversations.
	result, _ := m.Update(MsgConvListUpdated{Conversations: store.conversations})
	m = result.(ChatModel)
	m.Focus = FocusSidebar
	m.ChatView.Input.Blur()

	if m.Sidebar.Cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.Sidebar.Cursor)
	}

	// Navigate down with 'j'.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = result.(ChatModel)

	if m.Sidebar.Cursor != 1 {
		t.Errorf("cursor after j = %d, want 1", m.Sidebar.Cursor)
	}

	// Navigate up with 'k'.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = result.(ChatModel)

	if m.Sidebar.Cursor != 0 {
		t.Errorf("cursor after k = %d, want 0", m.Sidebar.Cursor)
	}

	// Navigate past the top should clamp.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = result.(ChatModel)

	if m.Sidebar.Cursor != 0 {
		t.Errorf("cursor after k at top = %d, want 0", m.Sidebar.Cursor)
	}

	// 'G' goes to end.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = result.(ChatModel)

	if m.Sidebar.Cursor != 2 {
		t.Errorf("cursor after G = %d, want 2", m.Sidebar.Cursor)
	}

	// 'g' goes to start.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = result.(ChatModel)

	if m.Sidebar.Cursor != 0 {
		t.Errorf("cursor after g = %d, want 0", m.Sidebar.Cursor)
	}
}

func TestChatModelNewConversation(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.Focus = FocusSidebar
	m.ChatView.Input.Blur()

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	updated := result.(ChatModel)

	if updated.ActiveConv == nil {
		t.Fatal("ActiveConv should be set after 'n'")
	}
	if updated.Focus != FocusChatArea {
		t.Errorf("focus = %d, want FocusChatArea after new conversation", updated.Focus)
	}
	if updated.ChatView.Title != "New conversation" {
		t.Errorf("title = %q, want %q", updated.ChatView.Title, "New conversation")
	}
	if updated.ChatView.ModelTag != "test-model" {
		t.Errorf("model tag = %q, want %q", updated.ChatView.ModelTag, "test-model")
	}
}

func TestChatModelOpenConversation(t *testing.T) {
	t.Parallel()

	m, store, _ := newTestChatModel()
	now := time.Now()
	store.conversations = []chat.Conversation{
		{
			ID:    "c1",
			Title: "Test Chat",
			Model: "claude-3",
			Messages: []chat.Message{
				{Role: chat.RoleUser, Content: "Hello", Timestamp: now},
				{Role: chat.RoleAssistant, Content: "Hi!", Timestamp: now.Add(time.Second)},
			},
			UpdatedAt: now,
		},
	}

	// Load list.
	result, _ := m.Update(MsgConvListUpdated{Conversations: store.conversations})
	m = result.(ChatModel)

	// Simulate loading a conversation.
	loadedConv := store.conversations[0]
	result, _ = m.Update(MsgConvLoaded{Conversation: &loadedConv})
	updated := result.(ChatModel)

	if updated.ActiveConv == nil {
		t.Fatal("ActiveConv should be set after loading")
	}
	if updated.ActiveConv.ID != "c1" {
		t.Errorf("ActiveConv.ID = %q, want %q", updated.ActiveConv.ID, "c1")
	}
	if len(updated.ChatView.Messages) != 2 {
		t.Errorf("ChatView.Messages = %d, want 2", len(updated.ChatView.Messages))
	}
	if updated.ChatView.Title != "Test Chat" {
		t.Errorf("ChatView.Title = %q, want %q", updated.ChatView.Title, "Test Chat")
	}
	if updated.Focus != FocusChatArea {
		t.Errorf("focus = %d, want FocusChatArea", updated.Focus)
	}
}

func TestChatModelDeleteConfirmation(t *testing.T) {
	t.Parallel()

	m, store, _ := newTestChatModel()
	now := time.Now()
	store.conversations = []chat.Conversation{
		{ID: "c1", Title: "Doomed", UpdatedAt: now},
	}

	// Load list.
	result, _ := m.Update(MsgConvListUpdated{Conversations: store.conversations})
	m = result.(ChatModel)
	m.Focus = FocusSidebar
	m.ChatView.Input.Blur()

	// Press 'd' to initiate delete.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = result.(ChatModel)

	if !m.Sidebar.IsConfirmingDelete() {
		t.Error("sidebar should be in delete confirmation mode after 'd'")
	}
	sel := m.Sidebar.SelectedConversation()
	if sel == nil || sel.ID != "c1" {
		t.Errorf("selected conversation should be c1, got %v", sel)
	}

	// Press 'n' to cancel.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = result.(ChatModel)

	if m.Sidebar.IsConfirmingDelete() {
		t.Error("sidebar should not be in delete confirmation mode after cancel")
	}
}

func TestChatModelDeleteConfirm(t *testing.T) {
	t.Parallel()

	m, store, _ := newTestChatModel()
	now := time.Now()
	store.conversations = []chat.Conversation{
		{ID: "c1", Title: "Doomed", UpdatedAt: now},
	}

	// Load list.
	result, _ := m.Update(MsgConvListUpdated{Conversations: store.conversations})
	m = result.(ChatModel)
	m.Focus = FocusSidebar
	m.ChatView.Input.Blur()

	// Press 'd' then 'y' to confirm.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = result.(ChatModel)

	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = result.(ChatModel)

	if m.Sidebar.IsConfirmingDelete() {
		t.Error("sidebar should not be in delete confirmation mode after confirm")
	}
	if cmd == nil {
		t.Fatal("expected delete command after confirmation")
	}
}

func TestChatModelSearchMode(t *testing.T) {
	t.Parallel()

	m, store, _ := newTestChatModel()
	now := time.Now()
	store.conversations = []chat.Conversation{
		{ID: "c1", Title: "Alpha chat", UpdatedAt: now},
		{ID: "c2", Title: "Beta discussion", UpdatedAt: now.Add(-time.Hour)},
		{ID: "c3", Title: "Gamma alpha topic", UpdatedAt: now.Add(-2 * time.Hour)},
	}

	// Load list.
	result, _ := m.Update(MsgConvListUpdated{Conversations: store.conversations})
	m = result.(ChatModel)
	m.Focus = FocusSidebar
	m.ChatView.Input.Blur()

	// Press '/' to enter search.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = result.(ChatModel)

	if !m.Sidebar.IsSearching() {
		t.Error("sidebar should be in search mode after '/'")
	}

	// Type "alpha".
	for _, c := range "alpha" {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{c}})
		m = result.(ChatModel)
	}

	if m.Sidebar.SearchQuery != "alpha" {
		t.Errorf("SearchQuery = %q, want %q", m.Sidebar.SearchQuery, "alpha")
	}

	visible := m.Sidebar.visibleCount()
	if visible != 2 {
		t.Errorf("visible count = %d, want 2 (Alpha chat + Gamma alpha topic)", visible)
	}

	// Press Escape to cancel search.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = result.(ChatModel)

	if m.Sidebar.IsSearching() {
		t.Error("sidebar should not be in search mode after esc")
	}
	if m.Sidebar.SearchQuery != "" {
		t.Errorf("SearchQuery after esc = %q, want empty", m.Sidebar.SearchQuery)
	}
}

func TestChatModelSearchBackspace(t *testing.T) {
	t.Parallel()

	m, store, _ := newTestChatModel()
	store.conversations = []chat.Conversation{
		{ID: "c1", Title: "Test", UpdatedAt: time.Now()},
	}

	result, _ := m.Update(MsgConvListUpdated{Conversations: store.conversations})
	m = result.(ChatModel)
	m.Focus = FocusSidebar
	m.ChatView.Input.Blur()

	// Enter search and type "ab".
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = result.(ChatModel)
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = result.(ChatModel)
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = result.(ChatModel)

	if m.Sidebar.SearchQuery != "ab" {
		t.Fatalf("SearchQuery = %q, want %q", m.Sidebar.SearchQuery, "ab")
	}

	// Backspace.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = result.(ChatModel)

	if m.Sidebar.SearchQuery != "a" {
		t.Errorf("SearchQuery after backspace = %q, want %q", m.Sidebar.SearchQuery, "a")
	}
}

func TestChatModelConvListUpdated(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	now := time.Now()

	convs := []chat.Conversation{
		{ID: "c1", Title: "Chat 1", UpdatedAt: now},
		{ID: "c2", Title: "Chat 2", UpdatedAt: now.Add(-time.Hour)},
	}

	result, _ := m.Update(MsgConvListUpdated{Conversations: convs})
	updated := result.(ChatModel)

	if len(updated.Sidebar.Conversations) != 2 {
		t.Errorf("conversations = %d, want 2", len(updated.Sidebar.Conversations))
	}
}

func TestChatModelConvListError(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()

	result, _ := m.Update(MsgConvListUpdated{
		Err: chat.ErrNotFound,
	})
	updated := result.(ChatModel)

	// Should not crash; sidebar stays empty.
	if len(updated.Sidebar.Conversations) != 0 {
		t.Errorf("conversations = %d, want 0", len(updated.Sidebar.Conversations))
	}
}

func TestChatModelChatResponse(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.ActiveConv = &chat.Conversation{ID: "c1", Title: "Test"}

	result, _ := m.Update(MsgChatResponse{
		ConversationID: "c1",
		Content:        "AI response here",
	})
	updated := result.(ChatModel)

	if len(updated.ActiveConv.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(updated.ActiveConv.Messages))
	}
	if updated.ActiveConv.Messages[0].Role != chat.RoleAssistant {
		t.Errorf("role = %q, want %q", updated.ActiveConv.Messages[0].Role, chat.RoleAssistant)
	}
	if updated.ActiveConv.Messages[0].Content != "AI response here" {
		t.Errorf("content = %q, want %q", updated.ActiveConv.Messages[0].Content, "AI response here")
	}
	if updated.ChatView.Loading {
		t.Error("loading should be false after response")
	}
}

func TestChatModelChatDoneError(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.ActiveConv = &chat.Conversation{ID: "c1"}

	errMsg := MsgChatDone{
		ConversationID: "c1",
		Err:            chat.ErrNotFound,
	}
	result, _ := m.Update(errMsg)
	updated := result.(ChatModel)

	if updated.ChatView.Loading {
		t.Error("loading should be false after error")
	}
	// Should have added a system error message.
	if len(updated.ActiveConv.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(updated.ActiveConv.Messages))
	}
	if updated.ActiveConv.Messages[0].Role != chat.RoleSystem {
		t.Errorf("error message role = %q, want %q", updated.ActiveConv.Messages[0].Role, chat.RoleSystem)
	}
	if !strings.Contains(updated.ActiveConv.Messages[0].Content, "Error") {
		t.Errorf("error message content = %q, want containing 'Error'", updated.ActiveConv.Messages[0].Content)
	}
}

func TestChatModelChatDoneSuccess(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.ActiveConv = &chat.Conversation{ID: "c1", Title: "Test"}

	result, cmd := m.Update(MsgChatDone{ConversationID: "c1"})
	_ = result.(ChatModel)

	// Should return a save command.
	if cmd == nil {
		t.Fatal("expected save command on successful completion")
	}
}

func TestChatModelConvDeleted(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.ActiveConv = &chat.Conversation{ID: "c1", Title: "Doomed"}

	result, cmd := m.Update(MsgConvDeleted{ConversationID: "c1"})
	updated := result.(ChatModel)

	// Active conv should be cleared since it was the deleted one.
	if updated.ActiveConv != nil {
		t.Error("ActiveConv should be nil after deleting the active conversation")
	}
	if len(updated.ChatView.Messages) != 0 {
		t.Errorf("messages = %d, want 0", len(updated.ChatView.Messages))
	}
	// Should return a refresh command.
	if cmd == nil {
		t.Fatal("expected list refresh command after deletion")
	}
}

func TestChatModelConvDeletedDifferentConv(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.ActiveConv = &chat.Conversation{ID: "c1", Title: "Active"}

	result, _ := m.Update(MsgConvDeleted{ConversationID: "c2"})
	updated := result.(ChatModel)

	// Active conv should NOT be cleared.
	if updated.ActiveConv == nil {
		t.Error("ActiveConv should still be set when a different conv is deleted")
	}
	if updated.ActiveConv.ID != "c1" {
		t.Errorf("ActiveConv.ID = %q, want %q", updated.ActiveConv.ID, "c1")
	}
}

func TestChatModelView(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.Width = 100
	m.Height = 30
	m.recalcLayout()

	view := m.View()

	if view == "" {
		t.Fatal("View should not be empty")
	}
	// Should contain sidebar header.
	if !strings.Contains(view, "Conversations") {
		t.Error("view should contain sidebar header 'Conversations'")
	}
}

func TestChatModelViewTooSmall(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.Width = 20
	m.Height = 5
	m.recalcLayout()

	view := m.View()

	if !strings.Contains(view, "too small") {
		t.Errorf("small terminal view should show 'too small', got %q", view)
	}
}

func TestChatModelViewNoSidebar(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.Width = 50 // Below sidebarMinTermWidth
	m.Height = 30
	m.recalcLayout()

	view := m.View()

	if view == "" {
		t.Fatal("View should not be empty")
	}
	// Sidebar should be hidden at narrow width.
	if strings.Contains(view, "Conversations") {
		t.Error("sidebar should be hidden at narrow terminal width")
	}
}

func TestChatModelDeleteConfirmView(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.Sidebar.Conversations = []chat.Conversation{
		{ID: "c1", Title: "Test", UpdatedAt: time.Now()},
	}
	m.Sidebar.clampCursor()
	m.recalcLayout()
	m.Focus = FocusSidebar
	m.Sidebar.RequestDelete()

	view := m.View()

	if !strings.Contains(view, "Delete") {
		t.Error("delete confirm view should show 'Delete'")
	}
}

func TestChatSidebarSelectedConversation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sidebar ChatSidebar
		wantID  string
		wantNil bool
	}{
		{
			name:    "empty list returns nil",
			sidebar: ChatSidebar{},
			wantNil: true,
		},
		{
			name: "returns selected conversation",
			sidebar: ChatSidebar{
				Conversations: []chat.Conversation{
					{ID: "c1", Title: "First"},
					{ID: "c2", Title: "Second"},
				},
				Cursor: 1,
			},
			wantID: "c2",
		},
		{
			name: "cursor out of bounds returns nil",
			sidebar: ChatSidebar{
				Conversations: []chat.Conversation{
					{ID: "c1", Title: "First"},
				},
				Cursor: 5,
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			conv := tt.sidebar.SelectedConversation()
			if tt.wantNil {
				if conv != nil {
					t.Errorf("expected nil, got conversation %q", conv.ID)
				}
				return
			}
			if conv == nil {
				t.Fatal("expected conversation, got nil")
			}
			if conv.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", conv.ID, tt.wantID)
			}
		})
	}
}

func TestChatSidebarClampCursor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		convCount  int
		cursor     int
		wantCursor int
	}{
		{"empty list", 0, 5, 0},
		{"cursor too high", 3, 10, 2},
		{"cursor negative", 3, -1, 0},
		{"cursor valid", 3, 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &ChatSidebar{
				Conversations: make([]chat.Conversation, tt.convCount),
				Cursor:        tt.cursor,
			}
			s.clampCursor()
			if s.Cursor != tt.wantCursor {
				t.Errorf("cursor = %d, want %d", s.Cursor, tt.wantCursor)
			}
		})
	}
}

func TestChatSidebarUpdateFilter(t *testing.T) {
	t.Parallel()

	s := &ChatSidebar{
		Conversations: []chat.Conversation{
			{ID: "c1", Title: "Go programming"},
			{ID: "c2", Title: "Rust traits"},
			{ID: "c3", Title: "Go testing"},
		},
	}

	// No filter — all visible.
	if s.visibleCount() != 3 {
		t.Fatalf("unfiltered count = %d, want 3", s.visibleCount())
	}

	// Filter for "go".
	s.SearchQuery = "go"
	s.updateFilter()

	if s.visibleCount() != 2 {
		t.Errorf("filtered count = %d, want 2", s.visibleCount())
	}

	visible := s.visibleList()
	if len(visible) != 2 {
		t.Fatalf("visible list = %d, want 2", len(visible))
	}
	if visible[0].ID != "c1" {
		t.Errorf("first visible = %q, want %q", visible[0].ID, "c1")
	}
	if visible[1].ID != "c3" {
		t.Errorf("second visible = %q, want %q", visible[1].ID, "c3")
	}

	// Clear filter.
	s.SearchQuery = ""
	s.updateFilter()

	if s.visibleCount() != 3 {
		t.Errorf("cleared count = %d, want 3", s.visibleCount())
	}
}

func TestChatModelFooter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		focus     ChatFocus
		inputMode ChatInputMode
		setup     func(m *ChatModel)
		contains  []string
	}{
		{
			name:      "sidebar normal",
			focus:     FocusSidebar,
			inputMode: ChatModeNormal,
			contains:  []string{"h/l", "j/k", "navigate", "enter", "open", "n", "new", "q", "quit", "{/}", "model"},
		},
		{
			name:      "chat area compose",
			focus:     FocusChatArea,
			inputMode: ChatModeCompose,
			contains:  []string{"enter", "send", "esc", "normal", "{/}", "model"},
		},
		{
			name:      "chat area normal",
			focus:     FocusChatArea,
			inputMode: ChatModeNormal,
			contains:  []string{"i", "compose", "j/k", "scroll", "g/G", "q", "quit", "{/}", "model"},
		},
		{
			name:      "search mode",
			focus:     FocusSidebar,
			inputMode: ChatModeNormal,
			setup:     func(m *ChatModel) { m.Sidebar.EnterSearch() },
			contains:  []string{"esc", "cancel", "enter", "select"},
		},
		{
			name:      "delete confirm",
			focus:     FocusSidebar,
			inputMode: ChatModeNormal,
			setup: func(m *ChatModel) {
				m.Sidebar.Conversations = []chat.Conversation{
					{ID: "c1", Title: "Test"},
				}
				m.Sidebar.RequestDelete()
			},
			contains: []string{"y", "confirm", "n", "cancel"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m, _, _ := newTestChatModel()
			m.Focus = tt.focus
			m.InputMode = tt.inputMode
			if tt.setup != nil {
				tt.setup(&m)
			}

			footer := m.renderFooter()
			for _, want := range tt.contains {
				if !strings.Contains(footer, want) {
					t.Errorf("footer should contain %q, got %q", want, footer)
				}
			}
		})
	}
}

func TestChatModelConvSaved(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()

	result, cmd := m.Update(MsgConvSaved{ConversationID: "c1"})
	_ = result.(ChatModel)

	// Should return a list refresh command.
	if cmd == nil {
		t.Fatal("expected list refresh command after save")
	}
}

func TestChatFocusConstants(t *testing.T) {
	t.Parallel()

	if FocusSidebar == FocusChatArea {
		t.Error("FocusSidebar and FocusChatArea should be distinct values")
	}
}

func TestChatModelSidebarModes(t *testing.T) {
	t.Parallel()

	modes := []ChatSidebarMode{SidebarNormal, SidebarSearch, SidebarConfirmDelete}
	seen := make(map[ChatSidebarMode]bool)
	for _, m := range modes {
		if seen[m] {
			t.Errorf("duplicate ChatSidebarMode value: %d", m)
		}
		seen[m] = true
	}
}

func TestChatModelTitleEdit(t *testing.T) {
	t.Parallel()

	m, store, _ := newTestChatModel()
	now := time.Now()
	store.conversations = []chat.Conversation{
		{ID: "c1", Title: "Original Title", UpdatedAt: now},
	}

	// Load conversations.
	result, _ := m.Update(MsgConvListUpdated{Conversations: store.conversations})
	m = result.(ChatModel)
	m.Focus = FocusSidebar
	m.ChatView.Input.Blur()

	// Press 't' to start title editing.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = result.(ChatModel)

	if !m.Sidebar.TitleEditing {
		t.Fatal("expected TitleEditing to be true after 't'")
	}
	if m.Sidebar.TitleEdit != "Original Title" {
		t.Errorf("TitleEdit = %q, want %q", m.Sidebar.TitleEdit, "Original Title")
	}

	// Type some characters.
	for _, c := range "New" {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{c}})
		m = result.(ChatModel)
	}
	if !strings.Contains(m.Sidebar.TitleEdit, "New") {
		t.Errorf("TitleEdit = %q, want to contain 'New'", m.Sidebar.TitleEdit)
	}

	// Backspace removes a character.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = result.(ChatModel)

	runes := []rune(m.Sidebar.TitleEdit)
	if len(runes) > 0 && runes[len(runes)-1] == 'w' {
		t.Errorf("backspace should have removed last character")
	}

	// Escape cancels.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = result.(ChatModel)

	if m.Sidebar.TitleEditing {
		t.Fatal("expected TitleEditing to be false after Esc")
	}
	if m.Sidebar.TitleEdit != "" {
		t.Errorf("TitleEdit = %q, want empty after cancel", m.Sidebar.TitleEdit)
	}
}

func TestChatModelTitleEditConfirm(t *testing.T) {
	t.Parallel()

	m, store, _ := newTestChatModel()
	now := time.Now()
	store.conversations = []chat.Conversation{
		{ID: "c1", Title: "Original Title", UpdatedAt: now},
	}

	result, _ := m.Update(MsgConvListUpdated{Conversations: store.conversations})
	m = result.(ChatModel)
	m.Focus = FocusSidebar
	m.ChatView.Input.Blur()

	// Start title edit.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = result.(ChatModel)

	// Clear and type new title.
	m.Sidebar.TitleEdit = "Renamed"

	// Press Enter to confirm.
	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = result.(ChatModel)

	if m.Sidebar.TitleEditing {
		t.Fatal("expected TitleEditing to be false after Enter")
	}
	if cmd == nil {
		t.Fatal("expected rename command after confirming title edit")
	}
}

func TestChatModelTitleEditEmptyString(t *testing.T) {
	t.Parallel()

	m, store, _ := newTestChatModel()
	now := time.Now()
	store.conversations = []chat.Conversation{
		{ID: "c1", Title: "Title", UpdatedAt: now},
	}

	result, _ := m.Update(MsgConvListUpdated{Conversations: store.conversations})
	m = result.(ChatModel)
	m.Focus = FocusSidebar
	m.ChatView.Input.Blur()

	// Start title edit then clear to empty.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = result.(ChatModel)
	m.Sidebar.TitleEdit = "   " // whitespace only

	// Enter with empty title should not produce a rename command.
	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = result.(ChatModel)

	if cmd != nil {
		t.Fatal("expected no command for empty title")
	}
}

func TestChatModelSidebarGScrollOffset(t *testing.T) {
	t.Parallel()

	m, store, _ := newTestChatModel()
	now := time.Now()
	convs := make([]chat.Conversation, 15)
	for i := range convs {
		convs[i] = chat.Conversation{
			ID:        fmt.Sprintf("c%d", i),
			Title:     fmt.Sprintf("Conv %d", i),
			UpdatedAt: now.Add(-time.Duration(i) * time.Hour),
		}
	}
	store.conversations = convs

	result, _ := m.Update(MsgConvListUpdated{Conversations: convs})
	m = result.(ChatModel)
	m.Focus = FocusSidebar
	m.ChatView.Input.Blur()

	// Press 'G' to jump to bottom.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = result.(ChatModel)

	if m.Sidebar.Cursor != 14 {
		t.Errorf("cursor after G = %d, want 14", m.Sidebar.Cursor)
	}

	// The scroll offset should have adjusted so the cursor is visible.
	maxItems := m.Sidebar.maxVisibleItems()
	if m.Sidebar.Offset+maxItems <= m.Sidebar.Cursor {
		t.Errorf("offset %d + maxItems %d should include cursor %d",
			m.Sidebar.Offset, maxItems, m.Sidebar.Cursor)
	}
}

func TestChatModelSearchNavigation(t *testing.T) {
	t.Parallel()

	m, store, _ := newTestChatModel()
	now := time.Now()
	store.conversations = []chat.Conversation{
		{ID: "c1", Title: "Alpha chat", UpdatedAt: now},
		{ID: "c2", Title: "Beta discussion", UpdatedAt: now.Add(-time.Hour)},
		{ID: "c3", Title: "Alpha topic", UpdatedAt: now.Add(-2 * time.Hour)},
	}

	result, _ := m.Update(MsgConvListUpdated{Conversations: store.conversations})
	m = result.(ChatModel)
	m.Focus = FocusSidebar
	m.ChatView.Input.Blur()

	// Enter search and type "alpha".
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = result.(ChatModel)
	for _, c := range "alpha" {
		result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{c}})
		m = result.(ChatModel)
	}

	if m.Sidebar.visibleCount() != 2 {
		t.Fatalf("visible count = %d, want 2", m.Sidebar.visibleCount())
	}

	// Navigate down with arrow key in search mode.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = result.(ChatModel)

	if m.Sidebar.Cursor != 1 {
		t.Errorf("cursor after down in search = %d, want 1", m.Sidebar.Cursor)
	}

	// Navigate back up.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = result.(ChatModel)

	if m.Sidebar.Cursor != 0 {
		t.Errorf("cursor after up in search = %d, want 0", m.Sidebar.Cursor)
	}
}

func TestChatModelCycleModelIndicator(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()

	if m.ChatView.ModelCount < 2 {
		t.Fatalf("expected at least 2 models, got %d", m.ChatView.ModelCount)
	}

	initialIndex := m.ChatView.ModelIndex

	// Cycle forward with '}'.
	m.Focus = FocusSidebar // ensure not in compose mode
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'}'}})
	m = result.(ChatModel)

	if m.ChatView.ModelIndex == initialIndex {
		t.Error("expected ModelIndex to change after cycling")
	}
	if m.ChatView.ModelCount != len(m.Models) {
		t.Errorf("ModelCount = %d, want %d", m.ChatView.ModelCount, len(m.Models))
	}

	// Cycle backward with '{'.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'{'}})
	m = result.(ChatModel)

	if m.ChatView.ModelIndex != initialIndex {
		t.Errorf("expected ModelIndex to return to %d after cycling back, got %d",
			initialIndex, m.ChatView.ModelIndex)
	}
}

// ---------------------------------------------------------------------------
// Streaming tests
// ---------------------------------------------------------------------------

func TestChatModelChunkAppendsToAssistantMessage(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.ActiveConv = &chat.Conversation{ID: "c1"}
	m.streaming = true
	m.ChatView.Loading = true

	// First chunk — creates assistant message, transitions to streaming.
	result, cmd := m.Update(MsgChatChunk{ConversationID: "c1", Content: "Hello"})
	m = result.(ChatModel)

	if m.ChatView.Loading {
		t.Error("Loading should be false after first chunk")
	}
	if !m.ChatView.Streaming {
		t.Error("Streaming should be true after first chunk")
	}
	if len(m.ActiveConv.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(m.ActiveConv.Messages))
	}
	if m.ActiveConv.Messages[0].Role != chat.RoleAssistant {
		t.Errorf("role = %q, want assistant", m.ActiveConv.Messages[0].Role)
	}
	if m.ActiveConv.Messages[0].Content != "Hello" {
		t.Errorf("content = %q, want %q", m.ActiveConv.Messages[0].Content, "Hello")
	}
	if cmd == nil {
		t.Fatal("expected readNextChunk command")
	}

	// Second chunk — appends to existing assistant message.
	result, cmd = m.Update(MsgChatChunk{ConversationID: "c1", Content: " World"})
	m = result.(ChatModel)

	if len(m.ActiveConv.Messages) != 1 {
		t.Fatalf("messages = %d, want 1 (appended, not new)", len(m.ActiveConv.Messages))
	}
	if m.ActiveConv.Messages[0].Content != "Hello World" {
		t.Errorf("content = %q, want %q", m.ActiveConv.Messages[0].Content, "Hello World")
	}
	if cmd == nil {
		t.Fatal("expected readNextChunk command after second chunk")
	}
}

func TestChatModelChunkIgnoresWrongConversation(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.ActiveConv = &chat.Conversation{ID: "c2"}
	m.streaming = true

	// Chunk for c1 should be discarded when active is c2.
	result, cmd := m.Update(MsgChatChunk{ConversationID: "c1", Content: "stale"})
	m = result.(ChatModel)

	if len(m.ActiveConv.Messages) != 0 {
		t.Errorf("messages = %d, want 0 (chunk for wrong conv should be discarded)", len(m.ActiveConv.Messages))
	}
	if cmd != nil {
		t.Error("expected nil cmd for discarded chunk")
	}
}

func TestChatModelDoneIgnoresWrongConversation(t *testing.T) {
	t.Parallel()

	m, store, _ := newTestChatModel()
	m.ActiveConv = &chat.Conversation{ID: "c2"}
	m.streaming = true

	result, cmd := m.Update(MsgChatDone{ConversationID: "c1"})
	m = result.(ChatModel)

	// Streaming should NOT be reset since the message is for a different conv.
	if !m.streaming {
		t.Error("streaming should remain true for mismatched conv ID")
	}
	if cmd != nil {
		t.Error("expected nil cmd for discarded done")
	}
	if len(store.saved) != 0 {
		t.Error("should not save for discarded done")
	}
}

func TestChatModelErrorIgnoresWrongConversation(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.ActiveConv = &chat.Conversation{ID: "c2"}
	m.streaming = true

	result, _ := m.Update(MsgChatError{ConversationID: "c1", Err: fmt.Errorf("stale error")})
	m = result.(ChatModel)

	// Streaming should NOT be reset since the message is for a different conv.
	if !m.streaming {
		t.Error("streaming should remain true for mismatched conv ID")
	}
	// No system error message should be added.
	if len(m.ActiveConv.Messages) != 0 {
		t.Errorf("messages = %d, want 0 (error for wrong conv should be discarded)", len(m.ActiveConv.Messages))
	}
}

func TestChatModelDoneSuccessSaves(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.ActiveConv = &chat.Conversation{ID: "c1"}
	m.streaming = true
	m.ChatView.Streaming = true

	result, cmd := m.Update(MsgChatDone{ConversationID: "c1"})
	m = result.(ChatModel)

	if m.streaming {
		t.Error("streaming should be false after done")
	}
	if m.ChatView.Streaming {
		t.Error("ChatView.Streaming should be false after done")
	}
	if m.ChatView.Loading {
		t.Error("ChatView.Loading should be false after done")
	}
	if cmd == nil {
		t.Fatal("expected save command on successful completion")
	}
}

func TestChatModelErrorCancellationPreservesPartial(t *testing.T) {
	t.Parallel()

	m, store, _ := newTestChatModel()
	m.ActiveConv = &chat.Conversation{
		ID: "c1",
		Messages: []chat.Message{
			{Role: chat.RoleUser, Content: "Hello"},
			{Role: chat.RoleAssistant, Content: "Partial response"},
		},
	}
	m.ChatView.Messages = make([]chat.Message, len(m.ActiveConv.Messages))
	copy(m.ChatView.Messages, m.ActiveConv.Messages)
	m.streaming = true
	m.ChatView.Streaming = true

	result, cmd := m.Update(MsgChatError{ConversationID: "c1", Err: context.Canceled})
	m = result.(ChatModel)

	// Streaming state should be reset.
	if m.streaming {
		t.Error("streaming should be false after cancellation error")
	}
	if m.ChatView.Streaming {
		t.Error("ChatView.Streaming should be false after cancellation")
	}

	// Partial response should have "[canceled]" suffix.
	lastMsg := m.ActiveConv.Messages[len(m.ActiveConv.Messages)-1]
	if !strings.Contains(lastMsg.Content, "[canceled]") {
		t.Errorf("content = %q, want to contain '[canceled]'", lastMsg.Content)
	}
	if lastMsg.Content != "Partial response [canceled]" {
		t.Errorf("content = %q, want %q", lastMsg.Content, "Partial response [canceled]")
	}

	// Should issue a save command.
	if cmd == nil {
		t.Fatal("expected save command for partial conversation")
	}
	_ = store // store used to verify save
}

func TestChatModelErrorNonCancellationShowsSystemMessage(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.ActiveConv = &chat.Conversation{ID: "c1"}
	m.streaming = true

	result, cmd := m.Update(MsgChatError{ConversationID: "c1", Err: fmt.Errorf("network timeout")})
	m = result.(ChatModel)

	if m.streaming {
		t.Error("streaming should be false after error")
	}

	// Should have added a system error message.
	if len(m.ActiveConv.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(m.ActiveConv.Messages))
	}
	errMsg := m.ActiveConv.Messages[0]
	if errMsg.Role != chat.RoleSystem {
		t.Errorf("role = %q, want system", errMsg.Role)
	}
	if !strings.Contains(errMsg.Content, "network timeout") {
		t.Errorf("content = %q, want to contain 'network timeout'", errMsg.Content)
	}

	// No save command for errors.
	if cmd != nil {
		t.Error("expected nil cmd after non-cancellation error")
	}
}

func TestChatModelSendMessageTriggersStreaming(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.ChatView.Input.SetValue("Hello AI")

	result, cmd := m.sendMessage()
	m = result.(ChatModel)

	if !m.streaming {
		t.Error("streaming should be true after sendMessage")
	}
	if !m.ChatView.Loading {
		t.Error("Loading should be true after sendMessage")
	}
	if cmd == nil {
		t.Fatal("expected readNextChunk command from sendMessage")
	}
	// User message should be appended.
	if len(m.ActiveConv.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(m.ActiveConv.Messages))
	}
	if m.ActiveConv.Messages[0].Content != "Hello AI" {
		t.Errorf("content = %q, want %q", m.ActiveConv.Messages[0].Content, "Hello AI")
	}
}

func TestChatModelSendMessageEmptyIgnored(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.ChatView.Input.SetValue("   ") // whitespace only

	result, cmd := m.sendMessage()
	m = result.(ChatModel)

	if m.streaming {
		t.Error("streaming should be false for empty input")
	}
	if cmd != nil {
		t.Error("expected nil cmd for empty input")
	}
}

func TestChatModelEnterDisabledDuringStreaming(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.Focus = FocusChatArea
	m.InputMode = ChatModeCompose
	m.ChatView.Streaming = true
	m.ChatView.Input.SetValue("should not send")

	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = result.(ChatModel)

	// The input should NOT have been consumed.
	if m.ChatView.InputValue() != "should not send" {
		t.Errorf("input should remain unchanged during streaming, got %q", m.ChatView.InputValue())
	}
	// Should not start a new sendMessage (no streaming cmd).
	if cmd != nil {
		t.Error("expected nil cmd when enter is pressed during streaming")
	}
}

func TestChatModelEnterDisabledDuringLoading(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.Focus = FocusChatArea
	m.InputMode = ChatModeCompose
	m.ChatView.Loading = true
	m.ChatView.Input.SetValue("should not send")

	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = result.(ChatModel)

	if m.ChatView.InputValue() != "should not send" {
		t.Errorf("input should remain unchanged during loading, got %q", m.ChatView.InputValue())
	}
	if cmd != nil {
		t.Error("expected nil cmd when enter is pressed during loading")
	}
}

func TestChatModelCtrlCDuringStreamingCancels(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.streaming = true
	m.ChatView.Streaming = true

	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_ = result.(ChatModel)

	// Should NOT quit — just cancel.
	if cmd != nil {
		t.Error("expected nil cmd (no tea.Quit) when Ctrl+C during streaming")
	}
}

func TestChatModelCtrlCDuringLoadingCancels(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.ChatView.Loading = true

	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_ = result.(ChatModel)

	if cmd != nil {
		t.Error("expected nil cmd (no tea.Quit) when Ctrl+C during loading")
	}
}

func TestChatModelCtrlCIdleQuits(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	// Should produce a quit command.
	if cmd == nil {
		t.Fatal("expected tea.Quit command when idle")
	}
}

func TestChatModelNewConversationCancelsStreaming(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.ActiveConv = &chat.Conversation{ID: "c1"}
	m.streaming = true
	m.ChatView.Streaming = true
	m.ChatView.Loading = false
	m.Focus = FocusSidebar
	m.ChatView.Input.Blur()

	// Press 'n' to create new conversation.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = result.(ChatModel)

	// Streaming state should be cleaned up.
	if m.streaming {
		t.Error("streaming should be false after new conversation")
	}
	if m.ChatView.Streaming {
		t.Error("ChatView.Streaming should be false after new conversation")
	}
	// New conversation should be active.
	if m.ActiveConv == nil {
		t.Fatal("ActiveConv should be set")
	}
	if m.ActiveConv.ID == "c1" {
		t.Error("ActiveConv should be a new conversation, not the old one")
	}
}

func TestChatViewAppendStreamChunk(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 24)

	// First chunk creates a new assistant message.
	cv.AppendStreamChunk("Hello")

	if len(cv.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(cv.Messages))
	}
	if cv.Messages[0].Role != chat.RoleAssistant {
		t.Errorf("role = %q, want assistant", cv.Messages[0].Role)
	}
	if cv.Messages[0].Content != "Hello" {
		t.Errorf("content = %q, want %q", cv.Messages[0].Content, "Hello")
	}

	// Second chunk appends to existing assistant message.
	cv.AppendStreamChunk(" World")

	if len(cv.Messages) != 1 {
		t.Fatalf("messages = %d, want 1 (should append, not add new)", len(cv.Messages))
	}
	if cv.Messages[0].Content != "Hello World" {
		t.Errorf("content = %q, want %q", cv.Messages[0].Content, "Hello World")
	}
}

func TestChatViewAppendStreamChunkAfterUserMessage(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 24)

	cv.AddMessage(chat.Message{
		Role:      chat.RoleUser,
		Content:   "Question?",
		Timestamp: time.Now(),
	})

	// First stream chunk creates a NEW assistant message (doesn't append to user msg).
	cv.AppendStreamChunk("Answer")

	if len(cv.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(cv.Messages))
	}
	if cv.Messages[1].Role != chat.RoleAssistant {
		t.Errorf("role = %q, want assistant", cv.Messages[1].Role)
	}
	if cv.Messages[1].Content != "Answer" {
		t.Errorf("content = %q, want %q", cv.Messages[1].Content, "Answer")
	}
}

func TestChatViewStreamingIndicator(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 24)
	cv.Streaming = true

	view := cv.View()

	if !strings.Contains(view, "streaming") {
		t.Error("expected 'streaming' indicator when Streaming is true")
	}
	// Should show cancel hint, not "enter: send".
	if strings.Contains(view, "enter: send") {
		t.Error("should not show 'enter: send' during streaming")
	}
	if !strings.Contains(view, "ctrl+c") {
		t.Error("expected 'ctrl+c' cancel hint during streaming")
	}
}

func TestChatViewLoadingIndicator(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 24)
	cv.SetLoading(true)

	view := cv.View()

	if !strings.Contains(view, "thinking") {
		t.Error("expected 'thinking' indicator when Loading is true")
	}
	if !strings.Contains(view, "ctrl+c") {
		t.Error("expected 'ctrl+c' cancel hint during loading")
	}
	if strings.Contains(view, "enter: send") {
		t.Error("should not show 'enter: send' during loading")
	}
}

func TestChatViewIdleHint(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 24)

	view := cv.View()

	if !strings.Contains(view, "enter: send") {
		t.Error("expected 'enter: send' hint when idle")
	}
}

func TestChatModelResetStreamingState(t *testing.T) {
	t.Parallel()

	m, _, _ := newTestChatModel()
	m.streaming = true
	m.ChatView.Loading = true
	m.ChatView.Streaming = true
	m.streamChunks = make(<-chan string)
	m.streamErrs = make(<-chan error)

	m.resetStreamingState()

	if m.streaming {
		t.Error("streaming should be false")
	}
	if m.ChatView.Loading {
		t.Error("Loading should be false")
	}
	if m.ChatView.Streaming {
		t.Error("Streaming should be false")
	}
	if m.streamChunks != nil {
		t.Error("streamChunks should be nil")
	}
	if m.streamErrs != nil {
		t.Error("streamErrs should be nil")
	}
	// Context should be renewed (non-nil).
	if m.ctx == nil {
		t.Error("ctx should be renewed, not nil")
	}
	if m.cancel == nil {
		t.Error("cancel should be renewed, not nil")
	}
}

func TestStartPhaseChat(t *testing.T) {
	t.Parallel()

	store := &mockStore{}
	provider := &mockProvider{response: "ok"}
	m := NewChatModel(store, provider, "claude-3")

	pc := chat.PhaseContext{
		PhaseID:          "fix-tests",
		PhaseSpec:        "## Problem\nTests are failing.",
		Cycle:            2,
		MaxCycles:        5,
		LastSummary:      "Added missing assertions",
		DiffStat:         "+10 -3 across 2 files",
		ReviewerFindings: "Need more edge cases",
	}

	m.StartPhaseChat(pc)

	// Verify conversation was created with phase linkage.
	if m.ActiveConv == nil {
		t.Fatal("ActiveConv should not be nil")
	}
	if m.ActiveConv.PhaseID != "fix-tests" {
		t.Errorf("PhaseID = %q, want %q", m.ActiveConv.PhaseID, "fix-tests")
	}

	// Verify system message was seeded.
	if len(m.ActiveConv.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m.ActiveConv.Messages))
	}
	sysMsg := m.ActiveConv.Messages[0]
	if sysMsg.Role != chat.RoleSystem {
		t.Errorf("Message[0].Role = %q, want %q", sysMsg.Role, chat.RoleSystem)
	}
	if !strings.Contains(sysMsg.Content, "fix-tests") {
		t.Error("system message should contain phase ID")
	}
	if !strings.Contains(sysMsg.Content, "Tests are failing") {
		t.Error("system message should contain phase spec")
	}
	if !strings.Contains(sysMsg.Content, "Added missing assertions") {
		t.Error("system message should contain last summary")
	}

	// Verify chat view state.
	if m.ChatView.PhaseBadge != "fix-tests" {
		t.Errorf("PhaseBadge = %q, want %q", m.ChatView.PhaseBadge, "fix-tests")
	}
	if !strings.Contains(m.ChatView.Title, "fix-tests") {
		t.Errorf("Title = %q, want containing 'fix-tests'", m.ChatView.Title)
	}
	if m.PhaseContext == nil {
		t.Fatal("PhaseContext should not be nil")
	}
}

func TestRefreshPhaseContext(t *testing.T) {
	t.Parallel()

	store := &mockStore{}
	provider := &mockProvider{response: "ok"}
	m := NewChatModel(store, provider, "claude-3")

	// Start a phase chat first.
	pc := chat.PhaseContext{
		PhaseID: "task-1",
		Cycle:   1,
	}
	m.StartPhaseChat(pc)

	initialMsgCount := len(m.ActiveConv.Messages)

	// Refresh with updated context.
	updated := chat.PhaseContext{
		PhaseID:     "task-1",
		Cycle:       3,
		MaxCycles:   5,
		LastSummary: "Refactored module structure",
	}
	m.RefreshPhaseContext(updated)

	// Should have one more message.
	if len(m.ActiveConv.Messages) != initialMsgCount+1 {
		t.Fatalf("expected %d messages, got %d", initialMsgCount+1, len(m.ActiveConv.Messages))
	}

	lastMsg := m.ActiveConv.Messages[len(m.ActiveConv.Messages)-1]
	if lastMsg.Role != chat.RoleSystem {
		t.Errorf("last message Role = %q, want %q", lastMsg.Role, chat.RoleSystem)
	}
	if !strings.Contains(lastMsg.Content, "Context Refresh") {
		t.Error("refresh message should contain 'Context Refresh'")
	}
	if !strings.Contains(lastMsg.Content, "Refactored module structure") {
		t.Error("refresh message should contain updated summary")
	}
	if m.PhaseContext.Cycle != 3 {
		t.Errorf("PhaseContext.Cycle = %d, want 3", m.PhaseContext.Cycle)
	}
}

func TestStartPhaseChat_ClearsOnNewConversation(t *testing.T) {
	t.Parallel()

	store := &mockStore{}
	m := NewChatModel(store, nil, "claude-3")

	// Start a phase chat.
	pc := chat.PhaseContext{PhaseID: "phase-1"}
	m.StartPhaseChat(pc)

	if m.ChatView.PhaseBadge != "phase-1" {
		t.Fatal("phase badge should be set")
	}

	// Start a new regular conversation.
	model, _ := m.startNewConversation()
	if cm, ok := model.(ChatModel); ok {
		m = cm
	}

	// Phase context should be cleared.
	if m.PhaseContext != nil {
		t.Error("PhaseContext should be nil after new conversation")
	}
	if m.ChatView.PhaseBadge != "" {
		t.Errorf("PhaseBadge = %q, want empty", m.ChatView.PhaseBadge)
	}
}
