package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/chat"
)

func TestNewChatView(t *testing.T) {
	t.Parallel()

	cv := NewChatView()

	if !cv.Input.Focused() {
		t.Fatal("textinput should be focused after creation")
	}
	if cv.Loading {
		t.Fatal("loading should be false initially")
	}
	if len(cv.Messages) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(cv.Messages))
	}
	if !cv.autoScroll {
		t.Fatal("auto-scroll should be enabled initially")
	}
}

func TestChatViewSetSize(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 24)

	if cv.width != 80 {
		t.Fatalf("expected width 80, got %d", cv.width)
	}
	if cv.height != 24 {
		t.Fatalf("expected height 24, got %d", cv.height)
	}
	if !cv.ready {
		t.Fatal("viewport should be ready after SetSize")
	}

	vpHeight := cv.viewportHeight()
	expectedVP := 24 - titleBarHeight - inputAreaHeight
	if vpHeight != expectedVP {
		t.Fatalf("expected viewport height %d, got %d", expectedVP, vpHeight)
	}
}

func TestChatViewAddMessage(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 24)

	msg := chat.Message{
		Role:      chat.RoleUser,
		Content:   "Hello, world!",
		Timestamp: time.Date(2026, 3, 6, 10, 30, 0, 0, time.UTC),
	}
	cv.AddMessage(msg)

	if len(cv.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(cv.Messages))
	}
	if cv.Messages[0].Content != "Hello, world!" {
		t.Fatalf("expected content 'Hello, world!', got %q", cv.Messages[0].Content)
	}
}

func TestChatViewRenderEmptyState(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 24)

	view := cv.View()

	if !strings.Contains(view, "Start a conversation") {
		t.Fatal("empty state should show placeholder text")
	}
}

func TestChatViewEmptyStateKeybindingHints(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 30)

	view := cv.View()

	hints := []string{"Quick reference", "compose", "switch model", "sidebar", "search", "rename", "quit"}
	for _, hint := range hints {
		if !strings.Contains(view, hint) {
			t.Errorf("empty state should contain hint %q", hint)
		}
	}
}

func TestChatViewTitleBarModelPosition(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.Title = "Test Chat"
	cv.ModelTag = "claude-sonnet-4"
	cv.ModelIndex = 0
	cv.ModelCount = 3
	cv.SetSize(80, 24)

	view := cv.View()
	if !strings.Contains(view, "1/3") {
		t.Fatal("expected model position indicator '1/3' in title bar")
	}
}

func TestChatViewTitleBarSingleModel(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.Title = "Test Chat"
	cv.ModelTag = "claude-sonnet-4"
	cv.ModelIndex = 0
	cv.ModelCount = 1
	cv.SetSize(80, 24)

	view := cv.View()
	if strings.Contains(view, "1/1") {
		t.Fatal("single model should not show position indicator")
	}
	if !strings.Contains(view, "claude-sonnet-4") {
		t.Fatal("expected model name in title bar")
	}
}

func TestChatViewRenderMessages(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 6, 10, 30, 0, 0, time.UTC)
	tests := []struct {
		name     string
		role     chat.Role
		content  string
		contains string
	}{
		{
			name:     "user message",
			role:     chat.RoleUser,
			content:  "Hello",
			contains: "you>",
		},
		{
			name:     "assistant message",
			role:     chat.RoleAssistant,
			content:  "Hi there",
			contains: "assistant>",
		},
		{
			name:     "system message",
			role:     chat.RoleSystem,
			content:  "System info",
			contains: "system>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cv := NewChatView()
			cv.SetSize(80, 24)

			cv.AddMessage(chat.Message{
				Role:      tt.role,
				Content:   tt.content,
				Timestamp: now,
			})

			view := cv.View()
			if !strings.Contains(view, tt.contains) {
				t.Fatalf("expected view to contain %q for role %s", tt.contains, tt.role)
			}
			if !strings.Contains(view, tt.content) {
				t.Fatalf("expected view to contain message content %q", tt.content)
			}
		})
	}
}

func TestChatViewRenderTimestamp(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 24)

	ts := time.Date(2026, 3, 6, 14, 25, 33, 0, time.UTC)
	cv.AddMessage(chat.Message{
		Role:      chat.RoleUser,
		Content:   "test",
		Timestamp: ts,
	})

	view := cv.View()
	if !strings.Contains(view, "14:25:33") {
		t.Fatal("expected timestamp [14:25:33] in rendered view")
	}
}

func TestChatViewTitleBar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		title    string
		model    string
		contains []string
	}{
		{
			name:     "default title",
			title:    "",
			model:    "",
			contains: []string{"New conversation"},
		},
		{
			name:     "custom title",
			title:    "My Chat",
			model:    "",
			contains: []string{"My Chat"},
		},
		{
			name:     "title with model",
			title:    "My Chat",
			model:    "claude-sonnet",
			contains: []string{"My Chat", "claude-sonnet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cv := NewChatView()
			cv.Title = tt.title
			cv.ModelTag = tt.model
			cv.SetSize(80, 24)

			view := cv.View()
			for _, want := range tt.contains {
				if !strings.Contains(view, want) {
					t.Fatalf("expected view to contain %q", want)
				}
			}
		})
	}
}

func TestChatViewLoading(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 24)

	cv.SetLoading(true)

	if !cv.Loading {
		t.Fatal("loading should be true")
	}

	view := cv.View()
	if !strings.Contains(view, "thinking") {
		t.Fatal("loading state should show 'thinking' indicator")
	}
}

func TestChatViewScrolling(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 10) // small viewport to force scrolling

	// Add enough messages to overflow the viewport.
	now := time.Now()
	for i := 0; i < 20; i++ {
		cv.AddMessage(chat.Message{
			Role:      chat.RoleUser,
			Content:   strings.Repeat("Line content. ", 5),
			Timestamp: now.Add(time.Duration(i) * time.Minute),
		})
	}

	if !cv.autoScroll {
		t.Fatal("auto-scroll should be enabled after adding messages")
	}

	// Scroll up should disable auto-scroll.
	cv.ScrollUp()
	if cv.autoScroll {
		t.Fatal("auto-scroll should be disabled after scrolling up")
	}

	// Scroll to bottom should re-enable auto-scroll.
	cv.ScrollToBottom()
	if !cv.autoScroll {
		t.Fatal("auto-scroll should be re-enabled after scrolling to bottom")
	}

	// Scroll to top should disable auto-scroll.
	cv.ScrollToTop()
	if cv.autoScroll {
		t.Fatal("auto-scroll should be disabled after scrolling to top")
	}
}

func TestChatViewInputValue(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 24)

	cv.Input.SetValue("  hello  ")
	val := cv.InputValue()
	if val != "hello" {
		t.Fatalf("expected trimmed input 'hello', got %q", val)
	}

	cv.ClearInput()
	if cv.Input.Value() != "" {
		t.Fatalf("expected empty input after clear, got %q", cv.Input.Value())
	}
}

func TestChatViewViewEmpty(t *testing.T) {
	t.Parallel()

	cv := NewChatView()

	// No SetSize called — should return empty string.
	view := cv.View()
	if view != "" {
		t.Fatalf("expected empty view before SetSize, got %q", view)
	}
}

func TestRenderMarkdownBold(t *testing.T) {
	t.Parallel()

	result := renderMarkdown("This is **bold** text", styleChatAssistant, 80)
	if !strings.Contains(result, "bold") {
		t.Fatal("expected bold text to be present in output")
	}
	// The word "bold" should be rendered (not the ** delimiters in raw form).
	if strings.Contains(result, "**bold**") {
		t.Fatal("raw ** delimiters should not appear in rendered output")
	}
}

func TestRenderMarkdownInlineCode(t *testing.T) {
	t.Parallel()

	result := renderMarkdown("Use `fmt.Println` here", styleChatAssistant, 80)
	if !strings.Contains(result, "fmt.Println") {
		t.Fatal("expected inline code content in output")
	}
}

func TestRenderMarkdownCodeBlock(t *testing.T) {
	t.Parallel()

	input := "Before\n```go\nfunc main() {}\n```\nAfter"
	result := renderMarkdown(input, styleChatAssistant, 80)

	if !strings.Contains(result, "go") {
		t.Fatal("expected language hint in code block opener")
	}
	if !strings.Contains(result, "func main()") {
		t.Fatal("expected code block content in output")
	}
	if !strings.Contains(result, "Before") {
		t.Fatal("expected text before code block")
	}
	if !strings.Contains(result, "After") {
		t.Fatal("expected text after code block")
	}
}

func TestRenderMarkdownEmptyCodeBlock(t *testing.T) {
	t.Parallel()

	input := "```\nsome code\n```"
	result := renderMarkdown(input, styleChatAssistant, 80)

	if !strings.Contains(result, "┌──") {
		t.Fatal("expected code block fence opener")
	}
	if !strings.Contains(result, "└──") {
		t.Fatal("expected code block fence closer")
	}
	if !strings.Contains(result, "some code") {
		t.Fatal("expected code block content")
	}
}

func TestFindClosing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		start int
		delim string
		want  int
	}{
		{"found", "hello** world", 0, "**", 5},
		{"not found", "hello world", 0, "**", -1},
		{"at start", "** rest", 0, "**", 0},
		{"empty search", "", 0, "**", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := findClosing([]rune(tt.input), tt.start, tt.delim)
			if got != tt.want {
				t.Fatalf("findClosing(%q, %d, %q) = %d, want %d",
					tt.input, tt.start, tt.delim, got, tt.want)
			}
		})
	}
}

func TestFindClosingRune(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		start int
		delim rune
		want  int
	}{
		{"found", "hello`world", 0, '`', 5},
		{"not found", "hello world", 0, '`', -1},
		{"at start", "`rest", 0, '`', 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := findClosingRune([]rune(tt.input), tt.start, tt.delim)
			if got != tt.want {
				t.Fatalf("findClosingRune(%q, %d, %q) = %d, want %d",
					tt.input, tt.start, tt.delim, got, tt.want)
			}
		})
	}
}

func TestChatViewMultipleMessages(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 24)

	now := time.Date(2026, 3, 6, 10, 0, 0, 0, time.UTC)

	cv.AddMessage(chat.Message{
		Role:      chat.RoleUser,
		Content:   "What is Go?",
		Timestamp: now,
	})
	cv.AddMessage(chat.Message{
		Role:      chat.RoleAssistant,
		Content:   "Go is a programming language.",
		Timestamp: now.Add(time.Second * 5),
	})

	if len(cv.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(cv.Messages))
	}

	view := cv.View()
	if !strings.Contains(view, "you>") {
		t.Fatal("expected user role label")
	}
	if !strings.Contains(view, "assistant>") {
		t.Fatal("expected assistant role label")
	}
	if !strings.Contains(view, "What is Go?") {
		t.Fatal("expected user message content")
	}
	if !strings.Contains(view, "Go is a programming language.") {
		t.Fatal("expected assistant message content")
	}
}

func TestChatViewInputArea(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 24)

	view := cv.View()

	// Should contain the input hint.
	if !strings.Contains(view, "enter: send") {
		t.Fatal("expected input hint 'enter: send' in view")
	}

	// Should contain the separator.
	if !strings.Contains(view, "─") {
		t.Fatal("expected separator line in input area")
	}
}

func TestChatViewLoadingWithMessages(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 24)

	now := time.Now()
	cv.AddMessage(chat.Message{
		Role:      chat.RoleUser,
		Content:   "Hello",
		Timestamp: now,
	})
	cv.SetLoading(true)

	view := cv.View()
	if !strings.Contains(view, "Hello") {
		t.Fatal("expected user message in view")
	}
	if !strings.Contains(view, "thinking") {
		t.Fatal("expected loading indicator in view")
	}

	// Hint line should also mention thinking.
	if !strings.Contains(view, "thinking") {
		t.Fatal("expected thinking indicator in hint area")
	}
}

func TestChatViewPageScroll(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 10) // small to force scrolling

	now := time.Now()
	for i := 0; i < 30; i++ {
		cv.AddMessage(chat.Message{
			Role:      chat.RoleUser,
			Content:   strings.Repeat("Lots of text. ", 3),
			Timestamp: now.Add(time.Duration(i) * time.Minute),
		})
	}

	// Page up should disable auto-scroll.
	cv.ScrollPageUp()
	if cv.autoScroll {
		t.Fatal("auto-scroll should be disabled after page up")
	}

	// Page down repeatedly should eventually re-enable auto-scroll
	// when reaching the bottom.
	for i := 0; i < 100; i++ {
		cv.ScrollPageDown()
	}
	if !cv.autoScroll {
		t.Fatal("auto-scroll should be re-enabled after scrolling to bottom via page down")
	}
}

func TestChatViewSetSizeResize(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 24)

	if !cv.ready {
		t.Fatal("viewport should be ready")
	}

	// Resize.
	cv.SetSize(120, 40)

	if cv.width != 120 {
		t.Fatalf("expected width 120, got %d", cv.width)
	}
	if cv.height != 40 {
		t.Fatalf("expected height 40, got %d", cv.height)
	}
}

// --- Edge case tests ---

func TestRenderMarkdownUnclosedCodeFence(t *testing.T) {
	t.Parallel()

	// An unclosed code fence should render remaining lines as code block
	// content without crashing.
	input := "before\n```go\nfunc main() {}\nstill in block"
	result := renderMarkdown(input, styleChatAssistant, 80)

	if !strings.Contains(result, "before") {
		t.Fatal("expected text before code fence")
	}
	if !strings.Contains(result, "┌─") {
		t.Fatal("expected code block opener")
	}
	if !strings.Contains(result, "func main()") {
		t.Fatal("expected code block content")
	}
	if !strings.Contains(result, "still in block") {
		t.Fatal("expected trailing content inside unclosed code block")
	}
	// Should NOT contain a closing fence.
	if strings.Contains(result, "└──") {
		t.Fatal("unclosed code fence should not produce a closing fence")
	}
}

func TestChatViewEmptyMessageContent(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 24)

	cv.AddMessage(chat.Message{
		Role:      chat.RoleAssistant,
		Content:   "",
		Timestamp: time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC),
	})

	if len(cv.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(cv.Messages))
	}

	// Should render without panic, showing the role label even with empty body.
	view := cv.View()
	if !strings.Contains(view, "assistant>") {
		t.Fatal("expected assistant role label for empty message")
	}
}

func TestChatViewWhitespaceOnlyMessage(t *testing.T) {
	t.Parallel()

	cv := NewChatView()
	cv.SetSize(80, 24)

	cv.AddMessage(chat.Message{
		Role:      chat.RoleUser,
		Content:   "   \n\t\n  ",
		Timestamp: time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC),
	})

	// Should render without panic.
	view := cv.View()
	if !strings.Contains(view, "you>") {
		t.Fatal("expected user role label for whitespace-only message")
	}
}

func TestRenderMarkdownUnclosedBold(t *testing.T) {
	t.Parallel()

	// Unclosed ** should render as literal text (falls through to plain).
	result := renderMarkdown("This has **no closing bold", styleChatAssistant, 80)

	// The text including the ** should appear (rendered as plain text).
	if !strings.Contains(result, "**no closing bold") {
		t.Fatal("unclosed bold delimiters should be rendered as literal text")
	}
}

func TestRenderMarkdownUnclosedBacktick(t *testing.T) {
	t.Parallel()

	// Unclosed ` should render as literal text.
	result := renderMarkdown("Use `no closing backtick", styleChatAssistant, 80)

	if !strings.Contains(result, "`no closing backtick") {
		t.Fatal("unclosed backtick should be rendered as literal text")
	}
}

func TestRenderMarkdownLongWordExceedingWidth(t *testing.T) {
	t.Parallel()

	// A single word longer than the wrap width should be hard-broken
	// by wrapText rather than causing an infinite loop or panic.
	longWord := strings.Repeat("x", 100)
	result := renderMarkdown(longWord, styleChatAssistant, 40)

	// The full content should be present (possibly across multiple lines).
	if !strings.Contains(result, strings.Repeat("x", 40)) {
		t.Fatal("expected long word content in output")
	}
	// The result should contain a line break since the word exceeds width.
	if !strings.Contains(result, "\n") {
		t.Fatal("expected line break for word exceeding wrap width")
	}
}

func TestRenderInlineMarkdownBatchesPlainText(t *testing.T) {
	t.Parallel()

	// Verify that plain text between markdown spans is styled efficiently
	// (batched, not per-character). We test this by checking that the output
	// for a plain string produces a single styled run rather than per-char runs.
	plain := "hello world"
	result := renderInlineMarkdown(plain, styleChatAssistant)

	// The plain text should appear as-is within the output.
	if !strings.Contains(result, "hello world") {
		t.Fatal("expected plain text content in a single styled run")
	}

	// A per-character approach would produce "h" "e" "l" "l" "o" each
	// separately styled — far more bytes. A batched approach wraps
	// the entire string in one styled call. We verify by checking that
	// the byte count is reasonable (not bloated by per-char ANSI codes).
	perCharOverhead := len(styleChatAssistant.Render("x")) * len(plain)
	batchedSize := len(styleChatAssistant.Render(plain))
	if len(result) > batchedSize+10 {
		t.Fatalf("output appears per-character styled: len=%d, expected ~%d (per-char would be ~%d)",
			len(result), batchedSize, perCharOverhead)
	}
}

func TestRenderInlineMarkdownMixedSpans(t *testing.T) {
	t.Parallel()

	// Verify mixed inline formatting: plain + bold + plain + code + plain.
	input := "start **bold** middle `code` end"
	result := renderInlineMarkdown(input, styleChatAssistant)

	if !strings.Contains(result, "start") {
		t.Fatal("expected 'start' in output")
	}
	if !strings.Contains(result, "bold") {
		t.Fatal("expected 'bold' in output")
	}
	if !strings.Contains(result, "middle") {
		t.Fatal("expected 'middle' in output")
	}
	if !strings.Contains(result, "code") {
		t.Fatal("expected 'code' in output")
	}
	if !strings.Contains(result, "end") {
		t.Fatal("expected 'end' in output")
	}
}

func TestRenderMarkdownWrappingBeforeStyling(t *testing.T) {
	t.Parallel()

	// Verify that wrapping operates on plain text width, not ANSI-inflated width.
	// A 60-char plain text line with a 40-char wrap width should wrap at ~40 chars.
	// If wrapping happened after styling, ANSI codes would inflate the byte count
	// and cause premature wrapping.
	input := "Here is some text with **bold** and `code` mixed throughout the line"
	result := renderMarkdown(input, styleChatAssistant, 40)

	// Content should be present and wrapped.
	if !strings.Contains(result, "Here is") {
		t.Fatal("expected start of wrapped content")
	}
	if !strings.Contains(result, "bold") {
		t.Fatal("expected bold content after wrapping")
	}
	if !strings.Contains(result, "code") {
		t.Fatal("expected code content after wrapping")
	}
	// Should contain a line break due to wrapping.
	if !strings.Contains(result, "\n") {
		t.Fatal("expected line break from wrapping")
	}
}
