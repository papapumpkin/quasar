+++
id = "cli-command"
title = "Add `quasar chat` CLI command"
type = "feature"
priority = 2
depends_on = ["chat-model"]
+++

## Problem

The chat mode needs a CLI entry point. Shore launches directly from `main.rs`;
Quasar uses Cobra commands, so we add `quasar chat`.

## Solution

Add a `chat` Cobra command in `cmd/chat.go`.

**Behavior:**
- `quasar chat` — opens the chat TUI with sidebar + last conversation
- `quasar chat --new` — opens with a fresh conversation
- `quasar chat --model <name>` — override default model

**Wiring:**
1. Initialize `chat.FileStore` with `~/.quasar/chats/` directory
2. Initialize `chat.ClaudeProvider` with the configured `agent.Invoker`
3. Create `tui.ChatModel` with store + provider
4. Create `tea.NewProgram` with the chat model (alternate screen buffer)
5. Run the program, block until quit

**Integration with existing TUI:**
- Chat mode is a separate entry point from nebula/loop modes
- Reuses existing lipgloss styles, key maps, and program infrastructure
- Could later be accessible from the home/landing page via a menu option

## Files

- `cmd/chat.go` — Cobra command definition and wiring

## Acceptance Criteria

- [ ] `quasar chat` launches the chat TUI
- [ ] `--new` flag starts a fresh conversation
- [ ] `--model` flag overrides the default model
- [ ] Proper teardown on quit (save state, restore terminal)
- [ ] Error handling for missing config / API keys
