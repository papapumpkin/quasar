+++
id = "chat-store"
title = "Chat persistence types and JSON file store"
type = "feature"
priority = 1
depends_on = []
+++

## Problem

We need a persistence layer for chat conversations that doesn't introduce SQLite.
Shore uses SQLite with full-text search, but we need a simpler approach that fits
Quasar's existing patterns.

## Solution

Create `internal/chat` package with core types and a JSON file-based store.

**Types:**
- `Conversation` — metadata (ID, title, created/updated timestamps, model name)
- `Message` — role (user/assistant/system), content, timestamp, model ID
- `Store` interface — consumed where persistence is needed

**JSON file store:**
- Each conversation is a single JSON file: `~/.quasar/chats/<id>.json`
- File contains the `Conversation` struct with embedded `[]Message`
- `List()` scans directory, reads only metadata (or a lightweight index file)
- `Save(conv)` writes full conversation to disk
- `Load(id)` reads a single conversation
- `Delete(id)` removes the file
- Auto-generate titles from first user message (truncated) if no title set

This mirrors Shore's `database.rs` CRUD operations (create_chat, get_recent_chats,
get_chat_messages, update_chat_title, delete_chat) but with JSON files instead of SQLite.

## Files

- `internal/chat/types.go` — `Conversation`, `Message`, `Role` types
- `internal/chat/store.go` — `Store` interface + `FileStore` implementation
- `internal/chat/store_test.go` — table-driven tests using temp directories

## Acceptance Criteria

- [ ] `Conversation` and `Message` types defined with JSON tags
- [ ] `Store` interface with List, Load, Save, Delete methods
- [ ] `FileStore` reads/writes `~/.quasar/chats/*.json`
- [ ] Auto-title generation from first user message
- [ ] Tests cover create, load, list, delete, and edge cases (empty dir, missing file)
