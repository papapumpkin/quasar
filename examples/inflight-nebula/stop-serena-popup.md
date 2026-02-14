+++
id = "stop-serena-popup"
title = "Suppress Serena MCP UI popup during agent invocations"
type = "task"
priority = 1
depends_on = []
+++

## Problem

Every time quasar spawns a new Claude Code agent via `internal/claude/claude.go:Invoke`, the Serena MCP server triggers a UI popup. This interrupts developers watching nebula execution.

## Solution

Pass `--strict-mcp-config` flag or set `CLAUDE_CODE_DISABLE_MCP_POPUPS=1` environment variable in the `Invoke` method to prevent MCP servers from prompting during headless agent runs.

## Files to Modify

- `internal/claude/claude.go` — Add flag/env to `buildArgs()` or the env block in `Invoke()`

## Acceptance Criteria

- [ ] Running `quasar run "any task"` does NOT trigger Serena UI popups
- [ ] MCP servers still function for tool use, just no interactive popups
- [ ] Existing tests in `internal/claude/claude_test.go` still pass
- [ ] Add test verifying the suppression flag/env is present in args/env
