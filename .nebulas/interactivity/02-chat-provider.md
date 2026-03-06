+++
id = "chat-provider"
title = "Conversational provider interface wrapping agent.Invoker"
type = "feature"
priority = 2
depends_on = ["chat-store"]
+++

## Problem

Shore has a `ProviderClient` trait with a `run()` method that takes conversation
history and returns a generation result. We need an equivalent that wraps Quasar's
existing `agent.Invoker` for conversational (multi-turn) use.

## Solution

Create a `Provider` interface in `internal/chat` that adapts `agent.Invoker` for
multi-turn chat. Shore's provider takes the full conversation history and returns
a response — we do the same.

**Interface:**
```go
type Provider interface {
    Chat(ctx context.Context, messages []Message, model string) (string, error)
    ChatStream(ctx context.Context, messages []Message, model string) (<-chan string, <-chan error)
}
```

**Claude provider implementation:**
- Wraps `agent.Invoker` (Claude CLI via `claude -p`)
- Serializes conversation history into the prompt format Claude expects
- `Chat()` — synchronous, returns full response
- `ChatStream()` — returns channels for token-by-token streaming
- Model parameter selects which model to use (passed through to invoker)

This parallels Shore's `openai_provider.rs` which implements `ProviderClient::run()`
by calling the OpenAI-compatible API with conversation history.

## Files

- `internal/chat/provider.go` — `Provider` interface + `ClaudeProvider` implementation
- `internal/chat/provider_test.go` — tests with mock invoker

## Acceptance Criteria

- [ ] `Provider` interface defined with `Chat` and `ChatStream` methods
- [ ] `ClaudeProvider` wraps `agent.Invoker` for multi-turn conversation
- [ ] Conversation history serialized correctly for Claude
- [ ] `ChatStream` returns a channel that delivers response chunks
- [ ] Tests verify prompt construction and response handling
