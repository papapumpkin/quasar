package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/papapumpkin/quasar/internal/dialogue"
)

// Dialogue overlay styles — blue-bordered interactive session.
var (
	styleDialogueOverlay = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBlueshift).
				Padding(1, 2)

	styleDialogueHeader = lipgloss.NewStyle().
				Foreground(colorBlueshift).
				Bold(true)

	styleDialogueContext = lipgloss.NewStyle().
				Foreground(colorMutedLight)

	styleDialogueAgentMsg = lipgloss.NewStyle().
				Foreground(colorAccent)

	styleDialogueHumanMsg = lipgloss.NewStyle().
				Foreground(colorBlueshift).
				Italic(true)

	styleDialogueSystemMsg = lipgloss.NewStyle().
				Foreground(colorMuted).
				Italic(true)

	styleDialogueHint = lipgloss.NewStyle().
				Foreground(colorMuted).
				Italic(true)

	styleDialogueTimestamp = lipgloss.NewStyle().
				Foreground(colorMuted)
)

// DialogueMode controls which part of the overlay receives input.
type DialogueMode int

const (
	// DialogueModeCompose focuses the textinput for typing.
	DialogueModeCompose DialogueMode = iota
	// DialogueModeScrollContext scrolls the context panel.
	DialogueModeScrollContext
	// DialogueModeScrollThread scrolls the message thread.
	DialogueModeScrollThread
)

// DialogueAction is the result of handling a key event in the overlay.
type DialogueAction int

const (
	// DialogueNone means no model-level action is needed.
	DialogueNone DialogueAction = iota
	// DialogueClosed means the user closed the dialogue.
	DialogueClosed
	// DialogueSent means the user sent a message (no model action needed).
	DialogueSent
)

// DialogueOverlay renders an interactive dialogue session as a centered
// overlay. It shows a context panel (scrollable), a message thread
// (scrollable), and a text input for composing responses.
type DialogueOverlay struct {
	Session       *dialogue.MemSession
	Messages      []dialogue.Message
	Input         textinput.Model
	Mode          DialogueMode
	ContextScroll int
	ThreadScroll  int
}

// NewDialogueOverlay creates a dialogue overlay from a MsgDialogueOpen.
func NewDialogueOverlay(sess *dialogue.MemSession) *DialogueOverlay {
	ti := textinput.New()
	ti.Prompt = "you> "
	ti.Placeholder = "type a message (enter to send, ctrl+d to close)"
	ti.CharLimit = 512
	ti.Width = 60
	ti.Focus()

	return &DialogueOverlay{
		Session: sess,
		Input:   ti,
	}
}

// SendHumanMessage sends the current input to the agent via the session
// channel and records the message locally.
func (d *DialogueOverlay) SendHumanMessage() string {
	val := strings.TrimSpace(d.Input.Value())
	if val == "" {
		return ""
	}
	msg := dialogue.Message{
		Role:    dialogue.RoleHuman,
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
func (d *DialogueOverlay) AddAgentMessage(msg dialogue.Message) {
	d.Messages = append(d.Messages, msg)
}

// HandleKey processes a key event and returns the resulting action plus any
// Bubbletea command (e.g. from updating the textinput).
func (d *DialogueOverlay) HandleKey(msg tea.KeyMsg, keys KeyMap) (DialogueAction, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back), msg.String() == "ctrl+d":
		d.Session.Close()
		return DialogueClosed, nil

	case msg.String() == "tab":
		d.ToggleMode()
		return DialogueNone, nil

	case key.Matches(msg, keys.Enter):
		d.SendHumanMessage()
		return DialogueSent, nil

	default:
		return d.handleModeKey(msg, keys)
	}
}

// handleModeKey delegates to the appropriate scroll or compose handler.
func (d *DialogueOverlay) handleModeKey(msg tea.KeyMsg, keys KeyMap) (DialogueAction, tea.Cmd) {
	switch d.Mode {
	case DialogueModeScrollContext:
		d.scrollContext(msg, keys)
		return DialogueNone, nil
	case DialogueModeScrollThread:
		d.scrollThread(msg, keys)
		return DialogueNone, nil
	default:
		var cmd tea.Cmd
		d.Input, cmd = d.Input.Update(msg)
		return DialogueNone, cmd
	}
}

func (d *DialogueOverlay) scrollContext(msg tea.KeyMsg, keys KeyMap) {
	switch {
	case key.Matches(msg, keys.Up):
		if d.ContextScroll > 0 {
			d.ContextScroll--
		}
	case key.Matches(msg, keys.Down):
		d.ContextScroll++
	}
}

func (d *DialogueOverlay) scrollThread(msg tea.KeyMsg, keys KeyMap) {
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
func (d *DialogueOverlay) ToggleMode() {
	switch d.Mode {
	case DialogueModeCompose:
		d.Mode = DialogueModeScrollContext
		d.Input.Blur()
	case DialogueModeScrollContext:
		d.Mode = DialogueModeScrollThread
	case DialogueModeScrollThread:
		d.Mode = DialogueModeCompose
		d.Input.Focus()
	}
}

// View renders the dialogue overlay content.
func (d *DialogueOverlay) View(width, height int) string {
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
	header := styleDialogueHeader.Render(fmt.Sprintf("DIALOGUE%s", kindBadge))
	b.WriteString(header)
	b.WriteString("\n")

	// Title.
	if req.Title != "" {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorWhite).Render(req.Title))
		b.WriteString("\n")
	}

	if req.PhaseID != "" {
		b.WriteString(styleDialogueTimestamp.Render(fmt.Sprintf("phase: %s", req.PhaseID)))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Context panel — scrollable detailed info.
	if req.Context != "" {
		contextLines := strings.Split(req.Context, "\n")
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
		styled := styleDialogueContext.Width(contentWidth).Render(contextText)

		scrollIndicator := ""
		if len(contextLines) > maxContextLines {
			scrollIndicator = styleDialogueTimestamp.Render(
				fmt.Sprintf(" [%d-%d of %d lines]", start+1, end, len(contextLines)),
			)
		}

		b.WriteString(styleDialogueTimestamp.Render("── context ──"))
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
		b.WriteString(styleDialogueTimestamp.Render("── messages ──"))
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
			case dialogue.RoleAgent:
				line = styleDialogueAgentMsg.Render(
					fmt.Sprintf("  [%s] agent: %s", ts, msg.Content),
				)
			case dialogue.RoleHuman:
				line = styleDialogueHumanMsg.Render(
					fmt.Sprintf("  [%s] you: %s", ts, msg.Content),
				)
			case dialogue.RoleSystem:
				line = styleDialogueSystemMsg.Render(
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
	case DialogueModeCompose:
		modeHint = "enter: send  ctrl+d: close  tab: scroll"
	case DialogueModeScrollContext:
		modeHint = "↑/↓: scroll context  tab: next  esc: back"
	case DialogueModeScrollThread:
		modeHint = "↑/↓: scroll thread  tab: compose  esc: back"
	}
	b.WriteString(styleDialogueHint.Render(modeHint))

	// Wrap in styled overlay box.
	return styleDialogueOverlay.Width(overlayWidth).Render(b.String())
}
