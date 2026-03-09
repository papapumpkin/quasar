+++
id = "token-count-live"
title = "Ensure token count updates live in status bar"
type = "bug"
priority = 2
depends_on = []
+++

## Problem

The token count in the status bar bottom bar (`tokens 0`) doesn't update during execution. In `model.go`:
- Line 309: `m.StatusBar.TotalTokens += msg.Tokens` — only fires on `MsgAgentDone` (loop mode)
- Line 389/395: Same pattern for `MsgPhaseAgentDone` (nebula mode)

The `Tokens` field on these messages comes from the loop's `EventAgentDone`, but it's unclear whether the `Tokens` value is actually populated. The `InvocationResult` from `claude.Invoker` doesn't have a `Tokens` field — it has `CostUSD` and `DurationMs` but no token count.

## Solution

Trace the token count pipeline and fix wherever it breaks.

### Approach

1. **Trace the data flow**: Follow `Tokens` from source to display:
   - `claude.Invoker.Invoke()` returns `InvocationResult` — check `CLIResponse` JSON for token fields
   - `loop.go` emits events — check if `EventAgentDone` carries tokens
   - Bridge/bus converts to `MsgPhaseAgentDone` — check if `Tokens` field is populated
   - `model.go` adds to `StatusBar.TotalTokens`

2. **Check Claude CLI JSON output**: The `CLIResponse` struct may not include token usage. Claude CLI's JSON output format likely includes `input_tokens` and `output_tokens` or similar. Add these fields to `CLIResponse` if missing, and populate `InvocationResult` with a new `Tokens` field (or compute from `InputTokens + OutputTokens`).

3. **Propagate through the chain**: Ensure the token count flows: `CLIResponse → InvocationResult → loop event → bus event → TUI message → StatusBar.TotalTokens`.

4. **Update bottom bar rendering**: The `BottomBar()` method already renders `FormatTokens(s.TotalTokens)` — it just needs non-zero input. No rendering changes needed if the data flow is fixed.

## Files

- `internal/claude/claude.go` — check `CLIResponse` for token fields, populate `InvocationResult`
- `internal/agent/agent.go` — add `Tokens` (or `InputTokens`/`OutputTokens`) to `InvocationResult` if missing
- `internal/loop/loop.go` — ensure token count is emitted in agent done events
- `internal/tui/bridge.go` / `internal/tui/bus_bridge.go` — ensure token count propagates to TUI messages

## Acceptance Criteria

- [ ] Token count in the bottom status bar increments after each agent completion
- [ ] Token count reflects actual usage from Claude CLI responses
- [ ] Works in both loop mode and nebula mode
- [ ] `go test ./internal/claude/...` passes
- [ ] `go test ./internal/tui/...` passes
- [ ] `go build ./...` and `go vet ./...` pass
