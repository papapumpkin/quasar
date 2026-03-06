package tui

import (
	"context"
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
		name     string
		focus    ChatFocus
		setup    func(m *ChatModel)
		contains []string
	}{
		{
			name:     "sidebar normal",
			focus:    FocusSidebar,
			contains: []string{"tab", "j/k", "navigate", "enter", "open", "n", "new chat", "q", "quit"},
		},
		{
			name:     "chat area normal",
			focus:    FocusChatArea,
			contains: []string{"tab", "enter", "send", "j/k", "scroll", "ctrl+c", "quit"},
		},
		{
			name:  "search mode",
			focus: FocusSidebar,
			setup: func(m *ChatModel) {
				m.Sidebar.EnterSearch()
			},
			contains: []string{"esc", "cancel", "enter", "select"},
		},
		{
			name:  "delete confirm",
			focus: FocusSidebar,
			setup: func(m *ChatModel) {
				m.Sidebar.Conversations = []chat.Conversation{{ID: "c1", Title: "Test"}}
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
