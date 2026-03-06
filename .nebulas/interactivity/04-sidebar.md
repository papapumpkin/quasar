+++
id = "sidebar"
title = "Left sidebar with conversation list and search"
type = "feature"
priority = 2
depends_on = ["chat-store"]
+++

## Problem

Shore's defining UX feature is the left sidebar that lists all saved conversations,
allowing users to switch between them with vim keys and search/filter. We need an
equivalent BubbleTea component.

## Solution

Create a `ChatSidebar` component in `internal/tui` that renders a selectable
list of conversations in a left panel.

**Layout:**
- Fixed width (~30% of terminal, min 20 cols, max 35 cols)
- Header: "Conversations" label + count
- List: conversation titles sorted by last-updated (newest first)
- Each item shows: title (or "New Chat"), timestamp, model badge
- Selected item highlighted with bold + background color
- Footer: key hints (n: new, d: delete, /: search)

**Navigation (vim-style, mirroring Shore's z/q keys):**
- j/k — move selection up/down
- Enter — open selected conversation
- n — create new conversation
- d — delete with confirmation
- / — enter search mode (filters list by title substring)

**Search mode:**
- Text input appears at top of sidebar
- List filters in real-time as user types
- Esc exits search, restoring full list
- Mirrors Shore's `SearchMode` AppState

**Collapsible:**
- Toggle sidebar visibility with a key (e.g., `ctrl+b` or `[`)
- When collapsed, full width goes to chat view

## Files

- `internal/tui/chatsidebar.go` — `ChatSidebar` struct, Update, View
- `internal/tui/chatsidebar_test.go` — navigation and filter tests

## Acceptance Criteria

- [ ] Sidebar renders conversation list sorted by recency
- [ ] j/k navigation with visual selection indicator
- [ ] Enter opens selected conversation in chat view
- [ ] n creates new conversation
- [ ] d deletes with confirmation prompt
- [ ] / enters search mode with real-time filtering
- [ ] Sidebar collapsible via toggle key
