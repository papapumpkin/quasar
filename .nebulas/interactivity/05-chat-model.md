+++
id = "chat-model"
title = "Root chat mode model composing sidebar and chat view"
type = "feature"
priority = 2
depends_on = ["chat-view", "sidebar", "chat-provider"]
+++

## Problem

We need a root BubbleTea model that composes the sidebar and chat view into a
cohesive chat mode, managing focus, state transitions, and message routing.
This mirrors Shore's `App` struct which holds `current_chat`, `chat_history`,
`chat_history_index`, and `AppState` enum.

## Solution

Create a `ChatModel` in `internal/tui` that acts as the root model for chat mode.

**State (mirroring Shore's App):**
```go
type ChatModel struct {
    Sidebar     ChatSidebar
    ChatView    ChatView
    Store       chat.Store
    Provider    chat.Provider
    Focus       ChatFocus        // Sidebar or ChatArea
    State       ChatState        // Normal, Search, DeleteConfirm
    ActiveConv  *chat.Conversation
    ConvList    []chat.Conversation
    Width, Height int
}
```

**Focus management:**
- Tab or h/l switches focus between sidebar and chat area
- When sidebar focused: j/k navigate conversations, Enter opens
- When chat area focused: j/k scroll messages, compose input active
- Mirrors Shore's implicit focus model where sidebar and chat coexist

**State transitions (mirroring Shore's AppState):**
- `Normal` — default, sidebar + chat side by side
- `Search` — sidebar search mode active
- `DeleteConfirm` — overlay asking to confirm deletion

**Message routing:**
- `MsgChatResponse` — streaming AI response chunk → append to active conversation
- `MsgChatDone` — inference complete → save conversation
- `MsgConvLoaded` — conversation loaded from store → populate chat view
- `MsgConvListUpdated` — store changed → refresh sidebar

**Layout:**
- Horizontal split: sidebar (left) | chat view (right)
- Responsive: sidebar collapses on narrow terminals
- Follows existing TUI patterns (lipgloss.JoinHorizontal)

## Files

- `internal/tui/chatmodel.go` — `ChatModel` struct, Init, Update, View
- `internal/tui/chatmsg.go` — chat-specific message types

## Acceptance Criteria

- [ ] `ChatModel` composes sidebar + chat view with horizontal split
- [ ] Focus switches between sidebar and chat area
- [ ] Opening a conversation loads it into the chat view
- [ ] New conversation creates entry and focuses chat input
- [ ] State transitions for search and delete confirmation
- [ ] Responsive layout adapts to terminal width
