+++
id = "tui-feedback"
title = "TUI feedback panel with push-to-talk and feedback history"
type = "feature"
priority = 2
depends_on = ["worker-integration", "transcriber-interface"]
scope = ["internal/tui/feedbackpanel.go", "internal/tui/feedbackpanel_test.go", "internal/tui/msg.go", "internal/tui/model.go"]
allow_scope_overlap = true
+++

## Problem

The feedback system (phases 4-6) operates at the library level. Users interacting through the BubbleTea TUI have no way to submit feedback or see what feedback has been processed. The TUI needs a feedback panel that:

1. Shows a scrollable history of feedback items and their resulting actions.
2. Accepts text input for typed feedback.
3. Supports push-to-talk for voice input (via the STT `Transcriber` from phase 1).
4. Integrates with the existing `AppModel` navigation and panel system.

## Solution

### New message types

```go
// In internal/tui/msg.go:

// MsgFeedbackSubmitted is sent when the user submits text or voice feedback.
type MsgFeedbackSubmitted struct {
    Content string
    Source  string // "text" or "voice"
}

// MsgFeedbackProcessed is sent when the advisor produces actions from feedback.
type MsgFeedbackProcessed struct {
    ItemID  string
    Actions []string // Human-readable action descriptions.
}

// MsgVoiceActive is sent when push-to-talk is engaged/disengaged.
type MsgVoiceActive struct {
    Active bool
}

// MsgTranscription is sent when the STT pipeline produces a transcription.
type MsgTranscription struct {
    Text  string
    Final bool
}
```

### FeedbackPanel component

```go
// internal/tui/feedbackpanel.go

package tui

import (
    "fmt"
    "strings"

    "github.com/charmbracelet/bubbles/textinput"
    "github.com/charmbracelet/bubbles/viewport"
    tea "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/lipgloss"
)

// FeedbackEntry is a single item in the feedback history display.
type FeedbackEntry struct {
    Source   string   // "text", "voice", "web"
    Content  string   // The user's feedback
    Actions  []string // Resulting actions (filled after processing)
    Pending  bool     // True while the advisor is processing
}

// FeedbackPanel displays feedback history and accepts input via text
// or push-to-talk voice.
type FeedbackPanel struct {
    entries    []FeedbackEntry
    input      textinput.Model
    viewport   viewport.Model
    voiceReady bool // True if STT Transcriber is available.
    voiceActive bool // True while push-to-talk is held.
    liveTranscript string // Partial transcription while speaking.
    width, height int
    focused    bool
}

// NewFeedbackPanel creates a panel ready for use.
func NewFeedbackPanel(voiceReady bool) FeedbackPanel {
    ti := textinput.New()
    ti.Placeholder = "Type feedback (or hold 'v' to speak)..."
    ti.CharLimit = 500

    return FeedbackPanel{
        input:      ti,
        voiceReady: voiceReady,
    }
}
```

### Update handler

```go
func (fp *FeedbackPanel) Update(msg tea.Msg) (FeedbackPanel, tea.Cmd) {
    var cmds []tea.Cmd

    switch msg := msg.(type) {

    case tea.KeyMsg:
        switch msg.String() {
        case "enter":
            if fp.input.Value() != "" {
                content := fp.input.Value()
                fp.input.Reset()
                fp.entries = append(fp.entries, FeedbackEntry{
                    Source:  "text",
                    Content: content,
                    Pending: true,
                })
                fp.scrollToBottom()
                return fp, func() tea.Msg {
                    return MsgFeedbackSubmitted{Content: content, Source: "text"}
                }
            }
        case "v":
            // Push-to-talk toggle (only when input is empty).
            if fp.voiceReady && fp.input.Value() == "" {
                fp.voiceActive = !fp.voiceActive
                return fp, func() tea.Msg {
                    return MsgVoiceActive{Active: fp.voiceActive}
                }
            }
        }

    case MsgTranscription:
        if msg.Final {
            fp.voiceActive = false
            fp.liveTranscript = ""
            fp.entries = append(fp.entries, FeedbackEntry{
                Source:  "voice",
                Content: msg.Text,
                Pending: true,
            })
            fp.scrollToBottom()
            return fp, func() tea.Msg {
                return MsgFeedbackSubmitted{Content: msg.Text, Source: "voice"}
            }
        }
        fp.liveTranscript = msg.Text

    case MsgFeedbackProcessed:
        // Mark the matching entry as processed.
        for i := range fp.entries {
            if fp.entries[i].Pending {
                fp.entries[i].Pending = false
                fp.entries[i].Actions = msg.Actions
                break
            }
        }
    }

    // Update text input.
    var cmd tea.Cmd
    fp.input, cmd = fp.input.Update(msg)
    cmds = append(cmds, cmd)

    return fp, tea.Batch(cmds...)
}
```

### View

```go
func (fp FeedbackPanel) View() string {
    var b strings.Builder

    // Header.
    header := lipgloss.NewStyle().Bold(true).Render("Feedback")
    b.WriteString(header + "\n")

    // History.
    for _, entry := range fp.entries {
        icon := ">"
        if entry.Source == "voice" {
            icon = "🎤" // Only emoji used in voice context.
        }
        line := fmt.Sprintf(" %s %s", icon, entry.Content)
        if entry.Pending {
            line += " (processing...)"
        }
        b.WriteString(line + "\n")
        for _, action := range entry.Actions {
            b.WriteString(fmt.Sprintf("   -> %s\n", action))
        }
    }

    // Live transcription.
    if fp.voiceActive && fp.liveTranscript != "" {
        b.WriteString(fmt.Sprintf(" [listening] %s\n", fp.liveTranscript))
    } else if fp.voiceActive {
        b.WriteString(" [listening...]\n")
    }

    // Input.
    b.WriteString("\n")
    b.WriteString(fp.input.View())

    // Voice hint.
    if fp.voiceReady {
        b.WriteString("  (hold 'v' for voice)")
    }

    return b.String()
}
```

### AppModel integration

Add the `FeedbackPanel` as a toggleable panel in `AppModel`:

```go
// In model.go AppModel struct:
FeedbackPanel FeedbackPanel
ShowFeedback  bool

// Toggle with 'f' key:
case "f":
    m.ShowFeedback = !m.ShowFeedback

// In the View method, render the feedback panel in the bottom
// section when ShowFeedback is true, similar to how DetailPanel
// is rendered in the right section.
```

Route feedback messages through `AppModel.Update`:

```go
case MsgFeedbackSubmitted:
    // Forward to the advisor agent (via a command).
    return m, m.processFeedback(msg)

case MsgFeedbackProcessed:
    m.FeedbackPanel, cmd = m.FeedbackPanel.Update(msg)
    return m, cmd

case MsgVoiceActive:
    // Start/stop the STT transcriber.
    return m, m.handleVoiceToggle(msg)

case MsgTranscription:
    m.FeedbackPanel, cmd = m.FeedbackPanel.Update(msg)
    return m, cmd
```

### STT integration for push-to-talk

When `MsgVoiceActive{Active: true}` is received, start the transcriber. When `Active: false`, stop it. Transcriptions arrive as `MsgTranscription` messages pumped from the `Transcriber.Transcriptions()` channel:

```go
func (m *AppModel) handleVoiceToggle(msg MsgVoiceActive) tea.Cmd {
    if m.transcriber == nil {
        return nil
    }
    if msg.Active {
        m.transcriber.Start(context.Background())
        return m.listenForTranscriptions()
    }
    m.transcriber.Stop()
    return nil
}

func (m *AppModel) listenForTranscriptions() tea.Cmd {
    return func() tea.Msg {
        t, ok := <-m.transcriber.Transcriptions()
        if !ok {
            return nil
        }
        return MsgTranscription{Text: t.Text, Final: t.Final}
    }
}
```

## Files

- `internal/tui/feedbackpanel.go` — `FeedbackPanel`, `FeedbackEntry`, `NewFeedbackPanel`, `Update`, `View`
- `internal/tui/feedbackpanel_test.go` — tests for: text input submission, voice toggle, transcription handling, entry rendering, pending state
- `internal/tui/msg.go` — add `MsgFeedbackSubmitted`, `MsgFeedbackProcessed`, `MsgVoiceActive`, `MsgTranscription`
- `internal/tui/model.go` — add `FeedbackPanel` and `ShowFeedback` fields, route messages, toggle with 'f' key, render in view

## Acceptance Criteria

- [ ] Feedback panel toggles with 'f' key in the TUI
- [ ] Text input submits feedback on Enter
- [ ] Push-to-talk starts STT on 'v' press (when voice is available)
- [ ] Live transcription shows partial text while speaking
- [ ] Final transcription submits as voice feedback
- [ ] Feedback history shows source icon, content, and resulting actions
- [ ] Pending entries show "processing..." indicator
- [ ] Processed entries show action descriptions
- [ ] Panel is hidden by default (does not interfere with existing TUI)
- [ ] Voice hint only shows when `Transcriber.Available()` returns true
- [ ] `go test ./internal/tui/...` passes
- [ ] `go vet ./...` passes
