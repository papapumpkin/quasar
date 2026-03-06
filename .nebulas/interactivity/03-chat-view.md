+++
id = "chat-view"
title = "BubbleTea chat message view with markdown rendering"
type = "feature"
priority = 2
depends_on = ["chat-store"]
+++

## Problem

The main chat area needs to display a scrollable thread of messages with
markdown formatting, user/assistant role differentiation, and a text input
for composing messages. Shore renders this with Ratatui using color-coded
messages, word wrapping, and chunk-based pagination.

## Solution

Create a `ChatView` BubbleTea component in `internal/tui` that renders the
conversation thread and input area.

**Layout (vertical split):**
1. **Title bar** (1-2 lines) — conversation title + active model name
2. **Message area** (flex) — scrollable list of messages, newest at bottom
3. **Input area** (3-5 lines) — text input with prompt indicator

**Message rendering:**
- User messages: right-aligned or prefixed with `you>`, styled in blue/cyan
- Assistant messages: left-aligned, styled in accent color, with basic markdown
  (bold, code blocks, inline code) via lipgloss
- System messages: muted/italic
- Timestamps shown in muted style
- Loading spinner when waiting for AI response (reuse existing spinner pattern)

**Scrolling:**
- vim keys (j/k) scroll through messages
- Auto-scroll to bottom on new message
- Page up/down for fast navigation

Mirrors Shore's `render_chat_content()` approach: color-coded roles, word-wrapped
text, chunk pagination, and search highlighting.

## Files

- `internal/tui/chatview.go` — `ChatView` struct, Update, View methods
- `internal/tui/chatview_test.go` — render tests

## Acceptance Criteria

- [ ] `ChatView` renders message thread with role-based styling
- [ ] Text input at bottom with prompt indicator
- [ ] Scrollable message area with j/k navigation
- [ ] Auto-scroll to newest message on arrival
- [ ] Loading spinner during AI inference
- [ ] Basic markdown rendering (bold, code, code blocks)
