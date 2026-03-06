+++
id = "streaming"
title = "Wire streaming AI responses into the chat view"
type = "feature"
priority = 2
depends_on = ["chat-model"]
+++

## Problem

Shore spawns async inference tasks and updates the UI as tokens arrive. We need
the same: when the user sends a message, kick off inference in a goroutine and
stream response chunks back to the BubbleTea model via messages.

## Solution

Use a BubbleTea command pattern to run inference asynchronously and deliver
chunks as messages.

**Flow:**
1. User presses Enter with non-empty input
2. `ChatModel.Update` appends user message to conversation
3. Returns a `tea.Cmd` that calls `Provider.ChatStream()` in a goroutine
4. Goroutine reads from the response channel, sending `MsgChatChunk` for each token
5. `MsgChatChunk` appends to the in-progress assistant message in the chat view
6. `MsgChatDone` fires when stream ends — save conversation to store

**Loading state:**
- While streaming, show a spinner next to the assistant's in-progress message
- Disable input submission (but allow scrolling/sidebar navigation)
- On error, show error inline as a system message

**Cancellation:**
- Ctrl+C during inference cancels the context
- Partial response preserved with "[cancelled]" suffix

This mirrors Shore's `spawn_inference_task()` pattern and `InferenceEvent`
completion handling, adapted to BubbleTea's Cmd/Msg paradigm.

## Files

- `internal/tui/chatmodel.go` — streaming command and message handling (extend)
- `internal/tui/chatmsg.go` — `MsgChatChunk`, `MsgChatDone`, `MsgChatError` types

## Acceptance Criteria

- [ ] Sending a message triggers async inference via tea.Cmd
- [ ] Response tokens stream into chat view in real-time
- [ ] Spinner shown during inference
- [ ] Conversation auto-saved on completion
- [ ] Ctrl+C cancels in-flight inference
- [ ] Errors display inline as system messages
