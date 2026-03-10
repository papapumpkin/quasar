package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/papapumpkin/quasar/internal/dialog"
)

// Dialog overlay styles — blue-bordered interactive session.
var (
	styleDialogOverlay = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBlueshift).
				Padding(1, 2)

	styleDialogHeader = lipgloss.NewStyle().
				Foreground(colorBlueshift).
				Bold(true)

	styleDialogContext = lipgloss.NewStyle().
				Foreground(colorMutedLight)

	styleDialogAgentMsg = lipgloss.NewStyle().
				Foreground(colorAccent)

	styleDialogHumanMsg = lipgloss.NewStyle().
				Foreground(colorBlueshift).
				Italic(true)

	styleDialogSystemMsg = lipgloss.NewStyle().
				Foreground(colorMuted).
				Italic(true)

	styleDialogHint = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	styleDialogTimestamp = lipgloss.NewStyle().
				Foreground(colorMuted)
)

// DialogMode controls which part of the overlay receives input.
type DialogMode int

const (
	// DialogModeCompose focuses the textinput for typing.
	DialogModeCompose DialogMode = iota
	// DialogModeScrollContext scrolls the context panel.
	DialogModeScrollContext
	// DialogModeScrollThread scrolls the message thread.
	DialogModeScrollThread
)

// DialogAction is the result of handling a key event in the overlay.
type DialogAction int

const (
	// DialogNone means no model-level action is needed.
	DialogNone DialogAction = iota
	// DialogClosed means the user closed the dialog.
	DialogClosed
	// DialogSent means the user sent a message (no model action needed).
	DialogSent
)

// DialogOverlay renders an interactive dialog session as a centered
// overlay. It shows a context panel (scrollable), a message thread
// (scrollable), and a text input for composing responses.
type DialogOverlay struct {
	Session       *dialog.MemSession
	Messages      []dialog.Message
	Input         textinput.Model
	Mode          DialogMode
	ContextScroll int
	ThreadScroll  int
}

// NewDialogOverlay creates a dialog overlay from a MsgDialogOpen.
func NewDialogOverlay(sess *dialog.MemSession) *DialogOverlay {
	ti := textinput.New()
	ti.Prompt = "you> "
	ti.Placeholder = "type a message (enter to send, ctrl+d to close)"
	ti.CharLimit = 512
	ti.Width = 60
	ti.Focus()

	return &DialogOverlay{
		Session: sess,
		Input:   ti,
	}
}

// SendHumanMessage sends the current input to the agent via the session
// channel and records the message locally.
func (d *DialogOverlay) SendHumanMessage() string {
	val := strings.TrimSpace(d.Input.Value())
	if val == "" {
		return ""
	}
	msg := dialog.Message{
		Role:    dialog.RoleHuman,
		Content: val,
		Time:    time.Now(),
	}
	d.Messages = append(d.Messages, msg)
	d.Input.SetValue("")

	// Non-blocking send to the session's FromHuman channel.
	select {
	case d.Session.FromHuman() <- msg:
	default:
	}
	return val
}

// AddAgentMessage appends an agent message to the local thread.
func (d *DialogOverlay) AddAgentMessage(msg dialog.Message) {
	d.Messages = append(d.Messages, msg)
}

// HandleKey processes a key event and returns the resulting action plus any
// Bubbletea command (e.g. from updating the textinput).
func (d *DialogOverlay) HandleKey(msg tea.KeyMsg, keys KeyMap) (DialogAction, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back), msg.String() == "ctrl+d":
		d.Session.Close()
		return DialogClosed, nil

	case msg.String() == "tab":
		d.ToggleMode()
		return DialogNone, nil

	case key.Matches(msg, keys.Enter):
		d.SendHumanMessage()
		return DialogSent, nil

	default:
		return d.handleModeKey(msg, keys)
	}
}

// handleModeKey delegates to the appropriate scroll or compose handler.
func (d *DialogOverlay) handleModeKey(msg tea.KeyMsg, keys KeyMap) (DialogAction, tea.Cmd) {
	switch d.Mode {
	case DialogModeScrollContext:
		d.scrollContext(msg, keys)
		return DialogNone, nil
	case DialogModeScrollThread:
		d.scrollThread(msg, keys)
		return DialogNone, nil
	default:
		var cmd tea.Cmd
		d.Input, cmd = d.Input.Update(msg)
		return DialogNone, cmd
	}
}

func (d *DialogOverlay) scrollContext(msg tea.KeyMsg, keys KeyMap) {
	switch {
	case key.Matches(msg, keys.Up):
		if d.ContextScroll > 0 {
			d.ContextScroll--
		}
	case key.Matches(msg, keys.Down):
		d.ContextScroll++
	}
}

func (d *DialogOverlay) scrollThread(msg tea.KeyMsg, keys KeyMap) {
	switch {
	case key.Matches(msg, keys.Up):
		if d.ThreadScroll > 0 {
			d.ThreadScroll--
		}
	case key.Matches(msg, keys.Down):
		d.ThreadScroll++
	}
}

// ToggleMode cycles through compose → scroll-context → scroll-thread → compose.
func (d *DialogOverlay) ToggleMode() {
	switch d.Mode {
	case DialogModeCompose:
		d.Mode = DialogModeScrollContext
		d.Input.Blur()
	case DialogModeScrollContext:
		d.Mode = DialogModeScrollThread
	case DialogModeScrollThread:
		d.Mode = DialogModeCompose
		d.Input.Focus()
	}
}

// View renders the dialog overlay content.
func (d *DialogOverlay) View(width, height int) string {
	var b strings.Builder

	// Constrain overlay width.
	overlayWidth := 72
	if width > 0 && width < overlayWidth+4 {
		overlayWidth = width - 4
	}
	if overlayWidth < 40 {
		overlayWidth = 40
	}
	contentWidth := overlayWidth - 6 // account for border + padding

	d.Input.Width = contentWidth

	req := d.Session.Request()

	// Header.
	kindBadge := ""
	if req.Kind != "" {
		kindBadge = fmt.Sprintf(" [%s]", req.Kind)
	}
	header := styleDialogHeader.Render(fmt.Sprintf("DIALOG%s", kindBadge))
	b.WriteString(header)
	b.WriteString("\n")

	// Title.
	if req.Title != "" {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorWhite).Render(req.Title))
		b.WriteString("\n")
	}

	if req.PhaseID != "" {
		b.WriteString(styleDialogTimestamp.Render(fmt.Sprintf("phase: %s", req.PhaseID)))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Context panel — scrollable, markdown-rendered detailed info.
	if req.Context != "" {
		rendered := RenderMarkdown(req.Context, contentWidth)
		contextLines := strings.Split(rendered, "\n")
		maxContextLines := (height / 3)
		if maxContextLines < 5 {
			maxContextLines = 5
		}

		start := d.ContextScroll
		if start > len(contextLines) {
			start = len(contextLines)
		}
		end := start + maxContextLines
		if end > len(contextLines) {
			end = len(contextLines)
		}

		visible := contextLines[start:end]
		contextText := strings.Join(visible, "\n")
		styled := styleDialogContext.Width(contentWidth).Render(contextText)

		scrollIndicator := ""
		if len(contextLines) > maxContextLines {
			scrollIndicator = styleDialogTimestamp.Render(
				fmt.Sprintf(" [%d-%d of %d lines]", start+1, end, len(contextLines)),
			)
		}

		b.WriteString(styleDialogTimestamp.Render("── context ──"))
		b.WriteString(scrollIndicator)
		b.WriteString("\n")
		b.WriteString(styled)
		b.WriteString("\n\n")
	}

	// Options (quick-select).
	if len(req.Options) > 0 {
		for i, opt := range req.Options {
			label := fmt.Sprintf("  %c) %s", 'a'+i, opt)
			b.WriteString(lipgloss.NewStyle().Foreground(colorBlueshift).Render(label))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Message thread.
	if len(d.Messages) > 0 {
		b.WriteString(styleDialogTimestamp.Render("── messages ──"))
		b.WriteString("\n")

		maxMsgLines := (height / 4)
		if maxMsgLines < 3 {
			maxMsgLines = 3
		}

		// Render messages, showing the most recent ones.
		var msgLines []string
		for _, msg := range d.Messages {
			ts := msg.Time.Format("15:04:05")
			var line string
			switch msg.Role {
			case dialog.RoleAgent:
				line = styleDialogAgentMsg.Render(
					fmt.Sprintf("  [%s] agent: %s", ts, msg.Content),
				)
			case dialog.RoleHuman:
				line = styleDialogHumanMsg.Render(
					fmt.Sprintf("  [%s] you: %s", ts, msg.Content),
				)
			case dialog.RoleSystem:
				line = styleDialogSystemMsg.Render(
					fmt.Sprintf("  [%s] system: %s", ts, msg.Content),
				)
			}
			msgLines = append(msgLines, line)
		}

		// Show tail of messages if thread is long.
		start := len(msgLines) - maxMsgLines + d.ThreadScroll
		if start < 0 {
			start = 0
		}
		end := start + maxMsgLines
		if end > len(msgLines) {
			end = len(msgLines)
		}
		for _, line := range msgLines[start:end] {
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Input.
	b.WriteString(d.Input.View())
	b.WriteString("\n")

	// Mode indicator + hints.
	var modeHint string
	switch d.Mode {
	case DialogModeCompose:
		modeHint = "enter: send  ctrl+d: close  tab: scroll"
	case DialogModeScrollContext:
		modeHint = "↑/↓: scroll context  tab: next  esc: back"
	case DialogModeScrollThread:
		modeHint = "↑/↓: scroll thread  tab: compose  esc: back"
	}
	b.WriteString(styleDialogHint.Render(modeHint))

	// Wrap in styled overlay box.
	return styleDialogOverlay.Width(overlayWidth).Render(b.String())
}
