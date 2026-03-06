package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/papapumpkin/quasar/internal/chat"
)

// Chat view styles — role-differentiated message rendering.
var (
	// styleChatUser styles user messages in blueshift cyan.
	styleChatUser = lipgloss.NewStyle().
			Foreground(colorBlueshift)

	// styleChatAssistant styles assistant messages in accent orange.
	styleChatAssistant = lipgloss.NewStyle().
				Foreground(colorAccent)

	// styleChatSystem styles system messages in muted italic.
	styleChatSystem = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	// styleChatTimestamp styles timestamps in muted gray.
	styleChatTimestamp = lipgloss.NewStyle().
				Foreground(colorMuted)

	// styleChatRoleLabel styles role labels (you>, assistant>) with bold emphasis.
	styleChatRoleLabel = lipgloss.NewStyle().
				Bold(true)

	// styleChatCode styles inline code spans.
	styleChatCode = lipgloss.NewStyle().
			Foreground(colorStarYellow)

	// styleChatCodeBlock styles fenced code block content.
	styleChatCodeBlock = lipgloss.NewStyle().
				Foreground(colorMutedLight)

	// styleChatBold styles bold text.
	styleChatBold = lipgloss.NewStyle().
			Bold(true)

	// styleChatTitle styles the conversation title bar.
	styleChatTitle = lipgloss.NewStyle().
			Foreground(colorWhite).
			Bold(true)

	// styleChatModel styles the model name in the title bar.
	styleChatModel = lipgloss.NewStyle().
			Foreground(colorNebula)

	// styleChatTitleBar renders the title bar background.
	styleChatTitleBar = lipgloss.NewStyle().
				Background(colorSurfaceBright).
				Padding(0, 1)

	// styleChatInputBorder renders a separator above the input area.
	styleChatInputBorder = lipgloss.NewStyle().
				Foreground(colorMuted)

	// styleChatEmpty styles the empty-state placeholder.
	styleChatEmpty = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)
)

// titleBarHeight is the number of lines consumed by the title bar.
const titleBarHeight = 1

// inputAreaHeight is the number of lines consumed by the input area
// (separator + input + hint line).
const inputAreaHeight = 3

// ChatView renders a scrollable conversation thread with a text input area.
// It displays messages with role-based styling, basic markdown formatting,
// and a loading spinner during AI inference.
type ChatView struct {
	Title    string // conversation title
	ModelTag string // active model name shown in title bar

	Messages []chat.Message
	Input    textinput.Model
	Spinner  spinner.Model
	Loading  bool // true while waiting for AI response

	viewport   viewport.Model
	width      int
	height     int
	totalLines int  // rendered content line count
	autoScroll bool // auto-scroll to bottom on new messages
	ready      bool // whether viewport has been sized
}

// NewChatView creates a ChatView with default settings.
func NewChatView() ChatView {
	ti := textinput.New()
	ti.Prompt = "you> "
	ti.Placeholder = "type a message…"
	ti.CharLimit = 4096
	ti.Width = 60
	ti.Focus()

	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = lipgloss.NewStyle().Foreground(colorBlue)

	return ChatView{
		Input:      ti,
		Spinner:    s,
		autoScroll: true,
	}
}

// SetSize updates the view dimensions and re-renders content.
func (cv *ChatView) SetSize(width, height int) {
	cv.width = width
	cv.height = height

	vpHeight := cv.viewportHeight()
	if vpHeight < 1 {
		vpHeight = 1
	}

	if !cv.ready {
		cv.viewport = viewport.New(width, vpHeight)
		cv.ready = true
	} else {
		cv.viewport.Width = width
		cv.viewport.Height = vpHeight
	}

	cv.Input.Width = width - lipgloss.Width(cv.Input.Prompt) - 2
	if cv.Input.Width < 20 {
		cv.Input.Width = 20
	}

	cv.refreshContent()
}

// viewportHeight returns the available height for the message viewport,
// accounting for the title bar and input area.
func (cv *ChatView) viewportHeight() int {
	return cv.height - titleBarHeight - inputAreaHeight
}

// AddMessage appends a message and refreshes the viewport. If auto-scroll
// is active (the user has not scrolled up), the viewport scrolls to bottom.
func (cv *ChatView) AddMessage(msg chat.Message) {
	cv.Messages = append(cv.Messages, msg)
	cv.refreshContent()
	if cv.autoScroll {
		cv.viewport.GotoBottom()
	}
}

// SetLoading sets the loading state and refreshes the viewport content
// to show or hide the spinner indicator.
func (cv *ChatView) SetLoading(loading bool) {
	cv.Loading = loading
	cv.refreshContent()
	if cv.autoScroll {
		cv.viewport.GotoBottom()
	}
}

// ScrollUp moves the viewport up by one line and disables auto-scroll.
func (cv *ChatView) ScrollUp() {
	if !cv.ready {
		return
	}
	cv.viewport.LineUp(1)
	cv.autoScroll = false
}

// ScrollDown moves the viewport down by one line. Re-enables auto-scroll
// if the viewport reaches the bottom.
func (cv *ChatView) ScrollDown() {
	if !cv.ready {
		return
	}
	cv.viewport.LineDown(1)
	cv.updateAutoScroll()
}

// ScrollPageUp moves the viewport up by one page.
func (cv *ChatView) ScrollPageUp() {
	if !cv.ready {
		return
	}
	cv.viewport.HalfViewUp()
	cv.autoScroll = false
}

// ScrollPageDown moves the viewport down by one page. Re-enables
// auto-scroll if the viewport reaches the bottom.
func (cv *ChatView) ScrollPageDown() {
	if !cv.ready {
		return
	}
	cv.viewport.HalfViewDown()
	cv.updateAutoScroll()
}

// ScrollToTop moves the viewport to the top and disables auto-scroll.
func (cv *ChatView) ScrollToTop() {
	if !cv.ready {
		return
	}
	cv.viewport.GotoTop()
	cv.autoScroll = false
}

// ScrollToBottom moves the viewport to the bottom and re-enables auto-scroll.
func (cv *ChatView) ScrollToBottom() {
	if !cv.ready {
		return
	}
	cv.viewport.GotoBottom()
	cv.autoScroll = true
}

// updateAutoScroll re-enables auto-scroll if the viewport has reached the bottom.
func (cv *ChatView) updateAutoScroll() {
	if !cv.ready || cv.viewport.Height <= 0 {
		return
	}
	maxOffset := cv.totalLines - cv.viewport.Height
	if maxOffset <= 0 || cv.viewport.YOffset >= maxOffset {
		cv.autoScroll = true
	}
}

// InputValue returns the current text in the input field.
func (cv *ChatView) InputValue() string {
	return strings.TrimSpace(cv.Input.Value())
}

// ClearInput resets the input field to empty.
func (cv *ChatView) ClearInput() {
	cv.Input.SetValue("")
}

// View renders the complete chat view: title bar, message viewport, and input area.
func (cv ChatView) View() string {
	if cv.width == 0 || cv.height == 0 {
		return ""
	}

	sections := make([]string, 0, 3)

	// Title bar.
	sections = append(sections, cv.renderTitleBar())

	// Message viewport.
	if cv.ready {
		sections = append(sections, cv.viewport.View())
	}

	// Input area.
	sections = append(sections, cv.renderInputArea())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderTitleBar renders the conversation title and model name.
func (cv ChatView) renderTitleBar() string {
	title := cv.Title
	if title == "" {
		title = "New conversation"
	}

	var parts []string
	parts = append(parts, styleChatTitle.Render(title))

	if cv.ModelTag != "" {
		parts = append(parts, styleChatModel.Render(fmt.Sprintf(" [%s]", cv.ModelTag)))
	}

	content := strings.Join(parts, "")
	return styleChatTitleBar.Width(cv.width).Render(content)
}

// renderInputArea renders the separator, text input, and hint line.
func (cv ChatView) renderInputArea() string {
	var b strings.Builder

	// Separator line.
	sep := strings.Repeat("─", cv.width)
	b.WriteString(styleChatInputBorder.Render(sep))
	b.WriteString("\n")

	// Text input.
	b.WriteString(cv.Input.View())
	b.WriteString("\n")

	// Hint line.
	hint := "enter: send"
	if cv.Loading {
		hint += "  " + cv.Spinner.View() + " thinking…"
	}
	b.WriteString(styleChatTimestamp.Render(hint))

	return b.String()
}

// refreshContent re-renders all messages into the viewport.
func (cv *ChatView) refreshContent() {
	if !cv.ready {
		return
	}
	content := cv.renderMessages()
	cv.totalLines = strings.Count(content, "\n") + 1
	cv.viewport.SetContent(content)
}

// renderMessages formats all messages into a single string for the viewport.
func (cv ChatView) renderMessages() string {
	if len(cv.Messages) == 0 && !cv.Loading {
		return styleChatEmpty.Render("  Start a conversation by typing a message below.")
	}

	contentWidth := cv.width - 2 // small margin
	if contentWidth < 20 {
		contentWidth = 20
	}

	var sb strings.Builder
	for i, msg := range cv.Messages {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(cv.renderMessage(msg, contentWidth))
	}

	// Loading indicator at the bottom.
	if cv.Loading {
		if len(cv.Messages) > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(cv.renderLoadingIndicator())
	}

	return sb.String()
}

// renderMessage formats a single message with role label, timestamp, and
// basic markdown rendering.
func (cv ChatView) renderMessage(msg chat.Message, width int) string {
	var b strings.Builder

	// Timestamp.
	ts := styleChatTimestamp.Render(msg.Timestamp.Format("[15:04:05]"))

	// Role label and content style.
	var label string
	var contentStyle lipgloss.Style
	switch msg.Role {
	case chat.RoleUser:
		label = styleChatRoleLabel.Foreground(colorBlueshift).Render("you>")
		contentStyle = styleChatUser
	case chat.RoleAssistant:
		label = styleChatRoleLabel.Foreground(colorAccent).Render("assistant>")
		contentStyle = styleChatAssistant
	case chat.RoleSystem:
		label = styleChatRoleLabel.Foreground(colorMuted).Render("system>")
		contentStyle = styleChatSystem
	default:
		label = styleChatRoleLabel.Render("?>")
		contentStyle = styleChatSystem
	}

	// Header line: [timestamp] role>
	b.WriteString(fmt.Sprintf("%s %s", ts, label))
	b.WriteString("\n")

	// Render message body with basic markdown.
	rendered := renderMarkdown(msg.Content, contentStyle, width)
	b.WriteString(rendered)

	return b.String()
}

// renderLoadingIndicator renders a spinner line while waiting for AI response.
func (cv ChatView) renderLoadingIndicator() string {
	label := styleChatRoleLabel.Foreground(colorAccent).Render("assistant>")
	return fmt.Sprintf("%s %s %s",
		styleChatTimestamp.Render("          "), // blank timestamp area
		label,
		cv.Spinner.View()+" "+styleChatAssistant.Render("thinking…"),
	)
}

// renderMarkdown applies basic markdown formatting to message content.
// Supported:
//   - **bold** → bold text
//   - `inline code` → code-styled text
//   - ```fenced code blocks``` → code block styling
//
// Content is word-wrapped to the given width.
func renderMarkdown(content string, baseStyle lipgloss.Style, width int) string {
	lines := strings.Split(content, "\n")
	var sb strings.Builder

	inCodeBlock := false
	for i, line := range lines {
		if i > 0 {
			sb.WriteString("\n")
		}

		trimmed := strings.TrimSpace(line)

		// Toggle fenced code blocks.
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			if inCodeBlock {
				// Opening fence — render language hint if present.
				lang := strings.TrimPrefix(trimmed, "```")
				if lang != "" {
					sb.WriteString(styleChatCodeBlock.Render("  ┌─ " + lang))
				} else {
					sb.WriteString(styleChatCodeBlock.Render("  ┌──"))
				}
			} else {
				// Closing fence.
				sb.WriteString(styleChatCodeBlock.Render("  └──"))
			}
			continue
		}

		if inCodeBlock {
			sb.WriteString(styleChatCodeBlock.Render("  │ " + line))
			continue
		}

		// Apply inline formatting and word-wrap.
		formatted := renderInlineMarkdown(line, baseStyle)
		wrapped := wrapText(formatted, width)
		sb.WriteString(wrapped)
	}

	return sb.String()
}

// renderInlineMarkdown processes inline markdown within a single line.
// It handles **bold** and `inline code` spans.
func renderInlineMarkdown(line string, baseStyle lipgloss.Style) string {
	var sb strings.Builder
	i := 0
	runes := []rune(line)
	n := len(runes)

	for i < n {
		// Bold: **text**
		if i+1 < n && runes[i] == '*' && runes[i+1] == '*' {
			end := findClosing(runes, i+2, "**")
			if end >= 0 {
				inner := string(runes[i+2 : end])
				sb.WriteString(styleChatBold.Render(inner))
				i = end + 2
				continue
			}
		}

		// Inline code: `text`
		if runes[i] == '`' {
			end := findClosingRune(runes, i+1, '`')
			if end >= 0 {
				inner := string(runes[i+1 : end])
				sb.WriteString(styleChatCode.Render(inner))
				i = end + 1
				continue
			}
		}

		// Regular character — apply base style.
		sb.WriteString(baseStyle.Render(string(runes[i])))
		i++
	}

	return sb.String()
}

// findClosing searches for a two-character closing delimiter in runes starting
// at position start. Returns the index of the first character of the delimiter,
// or -1 if not found.
func findClosing(runes []rune, start int, delim string) int {
	dr := []rune(delim)
	if len(dr) != 2 {
		return -1
	}
	for i := start; i+1 < len(runes); i++ {
		if runes[i] == dr[0] && runes[i+1] == dr[1] {
			return i
		}
	}
	return -1
}

// findClosingRune searches for a single-character closing delimiter.
// Returns the index of the delimiter, or -1 if not found.
func findClosingRune(runes []rune, start int, delim rune) int {
	for i := start; i < len(runes); i++ {
		if runes[i] == delim {
			return i
		}
	}
	return -1
}
