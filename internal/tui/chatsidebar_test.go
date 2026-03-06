package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/chat"
)

// --- Constructor tests ---

func TestNewChatSidebar(t *testing.T) {
	t.Parallel()

	cs := NewChatSidebar()

	if cs.Mode() != SidebarNormal {
		t.Fatalf("expected SidebarNormal mode, got %d", cs.Mode())
	}
	if cs.Cursor != 0 {
		t.Fatalf("expected cursor 0, got %d", cs.Cursor)
	}
	if len(cs.Conversations) != 0 {
		t.Fatalf("expected 0 conversations, got %d", len(cs.Conversations))
	}
	if cs.Collapsed {
		t.Fatal("expected not collapsed initially")
	}
}

// --- SidebarCalcWidth tests ---

func TestSidebarCalcWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		termWidth int
		want      int
	}{
		{"clamps to min for narrow terminal", 40, sidebarMinWidth},
		{"clamps to max for wide terminal", 200, sidebarMaxWidth},
		{"30 percent of 100", 100, 30},
		{"30 percent of 80", 80, 24},
		{"very narrow clamps to min", 10, sidebarMinWidth},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SidebarCalcWidth(tt.termWidth)
			if got != tt.want {
				t.Fatalf("SidebarCalcWidth(%d) = %d, want %d", tt.termWidth, got, tt.want)
			}
		})
	}
}

// --- Navigation tests ---

func TestChatSidebarMoveUpDown(t *testing.T) {
	t.Parallel()

	t.Run("move down increments cursor", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		cs.MoveDown()

		if cs.Cursor != 1 {
			t.Fatalf("expected cursor 1, got %d", cs.Cursor)
		}
	})

	t.Run("move down clamps at bottom", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(2)
		cs.MoveDown()
		cs.MoveDown()
		cs.MoveDown()

		if cs.Cursor != 1 {
			t.Fatalf("expected cursor 1 (clamped), got %d", cs.Cursor)
		}
	})

	t.Run("move up decrements cursor", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		cs.Cursor = 2
		cs.MoveUp()

		if cs.Cursor != 1 {
			t.Fatalf("expected cursor 1, got %d", cs.Cursor)
		}
	})

	t.Run("move up clamps at top", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		cs.MoveUp()

		if cs.Cursor != 0 {
			t.Fatalf("expected cursor 0 (clamped), got %d", cs.Cursor)
		}
	})

	t.Run("move down no-op on empty list", func(t *testing.T) {
		t.Parallel()
		cs := NewChatSidebar()
		cs.MoveDown()

		if cs.Cursor != 0 {
			t.Fatalf("expected cursor 0, got %d", cs.Cursor)
		}
	})
}

// --- ToggleCollapsed tests ---

func TestChatSidebarToggleCollapsed(t *testing.T) {
	t.Parallel()

	cs := NewChatSidebar()
	if cs.Collapsed {
		t.Fatal("expected not collapsed initially")
	}

	cs.ToggleCollapsed()
	if !cs.Collapsed {
		t.Fatal("expected collapsed after toggle")
	}

	cs.ToggleCollapsed()
	if cs.Collapsed {
		t.Fatal("expected not collapsed after double toggle")
	}
}

// --- Search mode tests ---

func TestChatSidebarSearchMode(t *testing.T) {
	t.Parallel()

	t.Run("enter search sets mode", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		cs.EnterSearch()

		if !cs.IsSearching() {
			t.Fatal("expected search mode")
		}
		if cs.SearchQuery != "" {
			t.Fatalf("expected empty query, got %q", cs.SearchQuery)
		}
	})

	t.Run("search input filters items", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3) // "Conv 0", "Conv 1", "Conv 2"
		cs.EnterSearch()
		for _, r := range "Conv 1" {
			cs.SearchInput(r)
		}

		if cs.SearchQuery != "Conv 1" {
			t.Fatalf("expected query 'Conv 1', got %q", cs.SearchQuery)
		}
		if cs.visibleCount() != 1 {
			t.Fatalf("expected 1 visible item, got %d", cs.visibleCount())
		}
	})

	t.Run("search is case insensitive", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		cs.EnterSearch()
		for _, r := range "conv" {
			cs.SearchInput(r)
		}

		if cs.visibleCount() != 3 {
			t.Fatalf("expected 3 visible items for 'conv', got %d", cs.visibleCount())
		}
	})

	t.Run("search backspace removes last rune", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		cs.EnterSearch()
		cs.SearchInput('a')
		cs.SearchInput('b')
		cs.SearchBackspace()

		if cs.SearchQuery != "a" {
			t.Fatalf("expected query 'a', got %q", cs.SearchQuery)
		}
	})

	t.Run("search backspace on empty is no-op", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		cs.EnterSearch()
		cs.SearchBackspace()

		if cs.SearchQuery != "" {
			t.Fatalf("expected empty query, got %q", cs.SearchQuery)
		}
	})

	t.Run("exit search restores full list", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		cs.EnterSearch()
		cs.SearchInput('z') // no matches
		cs.ExitSearch()

		if cs.IsSearching() {
			t.Fatal("expected normal mode after exit")
		}
		if cs.visibleCount() != 3 {
			t.Fatalf("expected 3 visible items after exit, got %d", cs.visibleCount())
		}
	})

	t.Run("search navigation within filtered results", func(t *testing.T) {
		t.Parallel()
		cs := NewChatSidebar()
		cs.Conversations = []chat.Conversation{
			{ID: "a", Title: "Conv 0"},
			{ID: "b", Title: "Conv 1"},
			{ID: "c", Title: "Conv 10"},
			{ID: "d", Title: "Conv 2"},
			{ID: "e", Title: "Conv 20"},
		}
		cs.EnterSearch()
		cs.SearchInput('2') // matches "Conv 2" and "Conv 20"

		if cs.visibleCount() != 2 {
			t.Fatalf("expected 2 visible items, got %d", cs.visibleCount())
		}

		cs.MoveDown()
		sel := cs.SelectedConversation()
		if sel == nil || sel.Title != "Conv 20" {
			title := ""
			if sel != nil {
				title = sel.Title
			}
			t.Fatalf("expected 'Conv 20' after MoveDown, got %q", title)
		}
	})
}

// --- Delete confirmation tests ---

func TestChatSidebarDeleteMode(t *testing.T) {
	t.Parallel()

	t.Run("request delete enters confirmation", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		cs.RequestDelete()

		if !cs.IsConfirmingDelete() {
			t.Fatal("expected delete confirmation mode")
		}
	})

	t.Run("request delete on empty is no-op", func(t *testing.T) {
		t.Parallel()
		cs := NewChatSidebar()
		cs.RequestDelete()

		if cs.IsConfirmingDelete() {
			t.Fatal("expected normal mode for empty list")
		}
	})

	t.Run("confirm delete returns conversation ID", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		cs.Cursor = 1
		cs.RequestDelete()
		id := cs.ConfirmDelete()

		if id != "id-1" {
			t.Fatalf("expected deleted ID 'id-1', got %q", id)
		}
		if cs.IsConfirmingDelete() {
			t.Fatal("expected normal mode after confirm")
		}
	})

	t.Run("cancel delete returns to normal", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		cs.RequestDelete()
		cs.CancelDelete()

		if cs.IsConfirmingDelete() {
			t.Fatal("expected normal mode after cancel")
		}
	})

	t.Run("confirm delete not in mode returns empty", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		id := cs.ConfirmDelete()

		if id != "" {
			t.Fatalf("expected empty string when not in delete mode, got %q", id)
		}
	})
}

// --- View rendering tests ---

func TestChatSidebarViewCollapsed(t *testing.T) {
	t.Parallel()

	cs := makeSidebarWithConvs(3)
	cs.SetSize(30, 20)
	cs.Collapsed = true

	view := cs.View()
	if view != "" {
		t.Fatalf("expected empty view when collapsed, got %q", view)
	}
}

func TestChatSidebarViewUnsized(t *testing.T) {
	t.Parallel()

	cs := makeSidebarWithConvs(3)
	view := cs.View()
	if view != "" {
		t.Fatalf("expected empty view when unsized, got %q", view)
	}
}

func TestChatSidebarViewRender(t *testing.T) {
	t.Parallel()

	t.Run("contains header with count", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		cs.SetSize(30, 20)

		view := cs.View()
		if !strings.Contains(view, "Conversations") {
			t.Fatal("expected 'Conversations' header")
		}
		if !strings.Contains(view, "(3)") {
			t.Fatal("expected count '(3)' in header")
		}
	})

	t.Run("contains conversation titles", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(2)
		cs.SetSize(30, 20)

		view := cs.View()
		if !strings.Contains(view, "Conv 0") {
			t.Fatal("expected 'Conv 0' in view")
		}
		if !strings.Contains(view, "Conv 1") {
			t.Fatal("expected 'Conv 1' in view")
		}
	})

	t.Run("contains selection indicator", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(2)
		cs.SetSize(30, 20)

		view := cs.View()
		if !strings.Contains(view, selectionIndicator) {
			t.Fatal("expected selection indicator in view")
		}
	})

	t.Run("contains footer hints", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(1)
		cs.SetSize(30, 20)

		view := cs.View()
		if !strings.Contains(view, "n:new") {
			t.Fatal("expected footer hint 'n:new'")
		}
		if !strings.Contains(view, "d:del") {
			t.Fatal("expected footer hint 'd:del'")
		}
	})

	t.Run("contains separator lines", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(1)
		cs.SetSize(30, 20)

		view := cs.View()
		if !strings.Contains(view, "─") {
			t.Fatal("expected separator line")
		}
	})

	t.Run("empty state message", func(t *testing.T) {
		t.Parallel()
		cs := NewChatSidebar()
		cs.SetSize(30, 20)

		view := cs.View()
		if !strings.Contains(view, "No conversations") {
			t.Fatal("expected 'No conversations' message")
		}
	})
}

func TestChatSidebarViewSearch(t *testing.T) {
	t.Parallel()

	t.Run("search header shows query", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		cs.SetSize(30, 20)
		cs.EnterSearch()
		cs.SearchInput('h')
		cs.SearchInput('i')

		view := cs.View()
		if !strings.Contains(view, "/hi") {
			t.Fatal("expected search prompt '/hi' in view")
		}
	})

	t.Run("search footer shows cancel hint", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		cs.SetSize(30, 20)
		cs.EnterSearch()

		view := cs.View()
		if !strings.Contains(view, "esc:cancel") {
			t.Fatal("expected 'esc:cancel' in search footer")
		}
	})

	t.Run("no matches message", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		cs.SetSize(30, 20)
		cs.EnterSearch()
		cs.SearchInput('z')
		cs.SearchInput('z')
		cs.SearchInput('z')

		view := cs.View()
		if !strings.Contains(view, "No matches") {
			t.Fatal("expected 'No matches' in view")
		}
	})
}

func TestChatSidebarViewDelete(t *testing.T) {
	t.Parallel()

	t.Run("delete confirmation shows prompt", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		cs.SetSize(30, 20)
		cs.RequestDelete()

		view := cs.View()
		if !strings.Contains(view, "Delete") {
			t.Fatal("expected 'Delete' in view")
		}
		if !strings.Contains(view, "y:yes") {
			t.Fatal("expected 'y:yes' in view")
		}
		if !strings.Contains(view, "n:no") {
			t.Fatal("expected 'n:no' in view")
		}
	})

	t.Run("delete prompt shows item title", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(3)
		cs.SetSize(30, 20)
		cs.Cursor = 1
		cs.RequestDelete()

		view := cs.View()
		if !strings.Contains(view, "Conv 1") {
			t.Fatal("expected 'Conv 1' in delete prompt")
		}
	})
}

func TestChatSidebarViewModelBadge(t *testing.T) {
	t.Parallel()

	cs := NewChatSidebar()
	cs.Conversations = []chat.Conversation{
		{
			ID:        "c1",
			Title:     "Test Chat",
			Model:     "claude-sonnet",
			UpdatedAt: time.Date(2025, 1, 15, 14, 30, 0, 0, time.UTC),
		},
	}
	cs.SetSize(35, 20)

	view := cs.View()
	if !strings.Contains(view, "claude-sonnet") {
		t.Fatal("expected model badge 'claude-sonnet' in view")
	}
}

// --- Timestamp formatting tests ---

func TestFormatSidebarTimestamp(t *testing.T) {
	t.Parallel()

	t.Run("old date shows month and day", func(t *testing.T) {
		t.Parallel()
		// Use a date in a different year to guarantee it's not "today".
		ts := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
		result := formatSidebarTimestamp(ts)
		if result != "Jan 15" {
			t.Fatalf("expected 'Jan 15', got %q", result)
		}
	})

	t.Run("very old date shows month and day", func(t *testing.T) {
		t.Parallel()
		ts := time.Date(2020, 12, 25, 8, 0, 0, 0, time.UTC)
		result := formatSidebarTimestamp(ts)
		if result != "Dec 25" {
			t.Fatalf("expected 'Dec 25', got %q", result)
		}
	})
}

// --- SetSize tests ---

func TestChatSidebarSetSize(t *testing.T) {
	t.Parallel()

	cs := NewChatSidebar()
	cs.SetSize(30, 20)

	if cs.Width != 30 {
		t.Fatalf("expected width 30, got %d", cs.Width)
	}
	if cs.Height != 20 {
		t.Fatalf("expected height 20, got %d", cs.Height)
	}
}

// --- Scrolling tests ---

func TestChatSidebarScrolling(t *testing.T) {
	t.Parallel()

	t.Run("items scroll when cursor exceeds visible area", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(10)
		cs.SetSize(30, 10) // small height: list area = 10-4 = 6 lines, 3 items visible

		// Move cursor to item 4 (beyond visible area).
		for i := 0; i < 4; i++ {
			cs.MoveDown()
		}

		// View should still render without error and show the cursor item.
		view := cs.View()
		if !strings.Contains(view, "Conv 4") {
			t.Fatal("expected scrolled view to contain 'Conv 4'")
		}
	})

	t.Run("first item visible when cursor at zero", func(t *testing.T) {
		t.Parallel()
		cs := makeSidebarWithConvs(10)
		cs.SetSize(30, 10)

		view := cs.View()
		if !strings.Contains(view, "Conv 0") {
			t.Fatal("expected 'Conv 0' visible at cursor 0")
		}
	})
}

// --- Mode tests ---

func TestChatSidebarModeConstants(t *testing.T) {
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

// --- helpers ---

func makeTestConversations(n int) []chat.Conversation {
	now := time.Date(2025, 3, 6, 14, 0, 0, 0, time.UTC)
	convs := make([]chat.Conversation, n)
	for i := range convs {
		convs[i] = chat.Conversation{
			ID:        fmt.Sprintf("id-%d", i),
			Title:     fmt.Sprintf("Conv %d", i),
			Model:     "claude",
			UpdatedAt: now.Add(-time.Duration(i) * time.Hour),
		}
	}
	return convs
}

func makeSidebarWithConvs(n int) ChatSidebar {
	cs := NewChatSidebar()
	cs.Conversations = makeTestConversations(n)
	return cs
}
