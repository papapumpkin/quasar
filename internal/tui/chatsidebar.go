package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/papapumpkin/quasar/internal/chat"
)

// Sidebar layout constraints.
const (
	sidebarMinWidth = 20
	sidebarMaxWidth = 35
	sidebarWidthPct = 30 // percentage of terminal width
)

// ChatSidebarMode tracks the sidebar's input mode for standalone rendering.
type ChatSidebarMode int

const (
	// SidebarNormal is the default navigation mode.
	SidebarNormal ChatSidebarMode = iota
	// SidebarSearch is active when the user is typing a filter query.
	SidebarSearch
	// SidebarConfirmDelete prompts the user to confirm deletion.
	SidebarConfirmDelete
)

// Sidebar-specific styles.
var (
	styleSidebarHeader = lipgloss.NewStyle().
				Foreground(colorWhite).
				Bold(true)

	styleSidebarCount = lipgloss.NewStyle().
				Foreground(colorMuted)

	styleSidebarItemNormal = lipgloss.NewStyle().
				Foreground(colorMutedLight)

	styleSidebarTimestamp = lipgloss.NewStyle().
				Foreground(colorMuted)

	styleSidebarModel = lipgloss.NewStyle().
				Foreground(colorNebula)

	styleSidebarHint = lipgloss.NewStyle().
				Foreground(colorMuted).
				Italic(true)

	styleSidebarSearchPrompt = lipgloss.NewStyle().
					Foreground(colorBlueshift)

	styleSidebarDeletePrompt = lipgloss.NewStyle().
					Foreground(colorDanger).
					Bold(true)
)

// ChatSidebar holds the state for the conversation list panel. It supports
// vim-style j/k navigation, real-time search filtering via /, delete
// confirmation via d, and collapsible visibility.
type ChatSidebar struct {
	Conversations []chat.Conversation
	Cursor        int
	Offset        int    // scroll offset for long lists
	SearchQuery   string // active search filter (empty = no filter)

	Width     int
	Height    int
	Collapsed bool

	mode     ChatSidebarMode
	filtered []int // indices into Conversations matching the search
}

// NewChatSidebar creates a ChatSidebar with default settings.
func NewChatSidebar() ChatSidebar {
	return ChatSidebar{
		mode: SidebarNormal,
	}
}

// SidebarCalcWidth calculates the sidebar panel width from the terminal width.
// The result is clamped between sidebarMinWidth and sidebarMaxWidth.
func SidebarCalcWidth(termWidth int) int {
	w := termWidth * sidebarWidthPct / 100
	if w < sidebarMinWidth {
		w = sidebarMinWidth
	}
	if w > sidebarMaxWidth {
		w = sidebarMaxWidth
	}
	return w
}

// SetSize updates the sidebar dimensions.
func (s *ChatSidebar) SetSize(width, height int) {
	s.Width = width
	s.Height = height
}

// ToggleCollapsed toggles sidebar visibility.
func (s *ChatSidebar) ToggleCollapsed() {
	s.Collapsed = !s.Collapsed
}

// Mode returns the current sidebar input mode.
func (s *ChatSidebar) Mode() ChatSidebarMode {
	return s.mode
}

// --- Selection ---

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

// --- Navigation ---

// MoveUp moves the cursor up by one, clamping at the top.
func (s *ChatSidebar) MoveUp() {
	if s.Cursor > 0 {
		s.Cursor--
	}
}

// MoveDown moves the cursor down by one, clamping at the bottom.
func (s *ChatSidebar) MoveDown() {
	maxIdx := s.visibleCount() - 1
	if maxIdx < 0 {
		return
	}
	if s.Cursor < maxIdx {
		s.Cursor++
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

// --- Search ---

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

// EnterSearch enters search mode, clearing any previous query.
func (s *ChatSidebar) EnterSearch() {
	s.mode = SidebarSearch
	s.SearchQuery = ""
	s.filtered = nil
	s.Cursor = 0
}

// ExitSearch exits search mode, restoring the full conversation list.
func (s *ChatSidebar) ExitSearch() {
	s.mode = SidebarNormal
	s.SearchQuery = ""
	s.filtered = nil
	s.Cursor = 0
}

// IsSearching returns whether the sidebar is in search mode.
func (s *ChatSidebar) IsSearching() bool {
	return s.mode == SidebarSearch
}

// SearchInput appends a rune to the search query and refilters the list.
func (s *ChatSidebar) SearchInput(r rune) {
	s.SearchQuery += string(r)
	s.updateFilter()
}

// SearchBackspace removes the last rune from the search query and refilters.
func (s *ChatSidebar) SearchBackspace() {
	if s.SearchQuery == "" {
		return
	}
	runes := []rune(s.SearchQuery)
	s.SearchQuery = string(runes[:len(runes)-1])
	s.updateFilter()
}

// --- Delete confirmation ---

// RequestDelete enters delete confirmation mode for the selected item.
// No-op if no conversation is selected.
func (s *ChatSidebar) RequestDelete() {
	if s.SelectedConversation() == nil {
		return
	}
	s.mode = SidebarConfirmDelete
}

// ConfirmDelete confirms the pending deletion and returns the conversation
// ID to delete. The caller is responsible for actual deletion (e.g., via
// the store). Returns an empty string if not in delete confirmation mode
// or no conversation is selected.
func (s *ChatSidebar) ConfirmDelete() string {
	if s.mode != SidebarConfirmDelete {
		return ""
	}
	sel := s.SelectedConversation()
	if sel == nil {
		s.mode = SidebarNormal
		return ""
	}
	id := sel.ID
	s.mode = SidebarNormal
	return id
}

// CancelDelete exits delete confirmation without deleting.
func (s *ChatSidebar) CancelDelete() {
	s.mode = SidebarNormal
}

// IsConfirmingDelete returns whether the sidebar is showing a delete prompt.
func (s *ChatSidebar) IsConfirmingDelete() bool {
	return s.mode == SidebarConfirmDelete
}

// --- Rendering ---

// View renders the sidebar panel. Returns an empty string when collapsed
// or when dimensions have not been set. This is the self-contained
// rendering method; ChatModel.renderSidebar provides an alternative
// integrated rendering path.
func (s ChatSidebar) View() string {
	if s.Collapsed || s.Width == 0 || s.Height == 0 {
		return ""
	}

	var lines []string

	// Header line.
	lines = append(lines, s.renderHeader())

	// Separator.
	lines = append(lines, styleSidebarCount.Render(strings.Repeat("─", s.Width)))

	// List content.
	// Reserve 4 lines: header(1) + sep(1) + footer-sep/prompt(1) + footer-hint(1).
	listHeight := s.Height - 4
	if listHeight < 1 {
		listHeight = 1
	}
	listLines := s.renderList(listHeight)
	lines = append(lines, listLines...)

	// Pad to fill available height before footer.
	targetBeforeFooter := s.Height - 2
	for len(lines) < targetBeforeFooter {
		lines = append(lines, "")
	}

	// Footer (2 lines).
	lines = append(lines, s.renderFooter()...)

	// Trim to exact height.
	if len(lines) > s.Height {
		lines = lines[:s.Height]
	}

	return strings.Join(lines, "\n")
}

// renderHeader renders the sidebar header line.
func (s ChatSidebar) renderHeader() string {
	if s.mode == SidebarSearch {
		query := s.SearchQuery + "_"
		maxLen := s.Width - 1 // "/" prefix
		if maxLen < 1 {
			maxLen = 1
		}
		query = TruncateWithEllipsis(query, maxLen)
		return styleSidebarSearchPrompt.Render("/" + query)
	}
	count := len(s.Conversations)
	header := styleSidebarHeader.Render("Conversations")
	countStr := styleSidebarCount.Render(fmt.Sprintf(" (%d)", count))
	return header + countStr
}

// renderList renders the visible conversation items within the given height.
func (s ChatSidebar) renderList(height int) []string {
	visible := s.visibleList()
	if len(visible) == 0 {
		if s.mode == SidebarSearch {
			return []string{styleSidebarHint.Render("  No matches")}
		}
		return []string{styleSidebarHint.Render("  No conversations")}
	}

	// Each item takes 2 lines (title + detail).
	const itemLines = 2
	maxItems := height / itemLines
	if maxItems < 1 {
		maxItems = 1
	}

	// Scroll offset to keep cursor in view.
	offset := 0
	if s.Cursor >= maxItems {
		offset = s.Cursor - maxItems + 1
	}
	end := offset + maxItems
	if end > len(visible) {
		end = len(visible)
	}

	var lines []string
	for vi := offset; vi < end; vi++ {
		conv := visible[vi]
		selected := vi == s.Cursor
		lines = append(lines, s.renderConvItem(conv, selected)...)
	}
	return lines
}

// renderConvItem renders a single conversation item as two lines:
// line 1: selection indicator + title
// line 2: timestamp + optional model badge (indented)
func (s ChatSidebar) renderConvItem(conv *chat.Conversation, selected bool) []string {
	// Line 1: indicator + title.
	indicator := "  "
	if selected {
		indicator = styleSelectionIndicator.Render(selectionIndicator) + " "
	}

	titleWidth := s.Width - 2 // indicator width
	if titleWidth < 4 {
		titleWidth = 4
	}
	title := TruncateWithEllipsis(conv.AutoTitle(), titleWidth)

	var styledTitle string
	if selected {
		styledTitle = styleRowSelected.Render(title)
	} else {
		styledTitle = styleSidebarItemNormal.Render(title)
	}

	line1 := indicator + styledTitle
	if selected {
		line1 = padToWidth(line1, s.Width, colorSelectionBg)
	}

	// Line 2: timestamp + model badge.
	ts := formatSidebarTimestamp(conv.UpdatedAt)
	var detailParts []string
	detailParts = append(detailParts, styleSidebarTimestamp.Render(ts))
	if conv.Model != "" {
		maxModelLen := s.Width - len(ts) - 5 // indent(3) + gap(2)
		if maxModelLen > 0 {
			detailParts = append(detailParts, styleSidebarModel.Render(
				TruncateWithEllipsis(conv.Model, maxModelLen),
			))
		}
	}
	detail := "   " + strings.Join(detailParts, "  ")
	if selected {
		detail = padToWidth(detail, s.Width, colorSelectionBg)
	}

	return []string{line1, detail}
}

// renderFooter renders the two footer lines (separator + hint, or delete prompt).
func (s ChatSidebar) renderFooter() []string {
	if s.mode == SidebarConfirmDelete {
		sel := s.SelectedConversation()
		if sel != nil {
			maxTitle := s.Width - 11 // ' Delete ""?' overhead
			if maxTitle < 4 {
				maxTitle = 4
			}
			title := TruncateWithEllipsis(sel.AutoTitle(), maxTitle)
			return []string{
				styleSidebarDeletePrompt.Render(fmt.Sprintf(" Delete %q?", title)),
				styleSidebarHint.Render("  y:yes  n:no"),
			}
		}
		return []string{
			styleSidebarHint.Render(" (nothing selected)"),
			styleSidebarHint.Render("  esc:cancel"),
		}
	}

	sep := styleSidebarCount.Render(strings.Repeat("─", s.Width))
	var hint string
	if s.mode == SidebarSearch {
		hint = styleSidebarHint.Render(" esc:cancel")
	} else {
		hint = styleSidebarHint.Render(" n:new d:del /:srch")
	}
	return []string{sep, hint}
}

// formatSidebarTimestamp renders a compact timestamp for the sidebar.
// Same-day times show "15:04"; older timestamps show "Jan 02".
func formatSidebarTimestamp(t time.Time) string {
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04")
	}
	return t.Format("Jan 02")
}
