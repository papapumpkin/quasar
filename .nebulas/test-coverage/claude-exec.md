+++
id = "claude-exec"
title = "Test claude invoker execution paths"
type = "task"
priority = 2
+++

## Goal

Increase `internal/claude` coverage from 40.0% to ~70% by testing `Invoke` and `Validate`.

## Current State

Tested at 100%:
- `buildEnv` — environment variable construction
- `buildArgs` — CLI argument construction

At 0%:
- `Invoke` — calls `exec.CommandContext` to run `claude -p`, parses JSON result
- `Validate` — calls `exec.LookPath` to check if `claude` binary exists
- `sessionAttr` — sets process group attributes (platform-specific, may be hard to test)

## Approach

1. Similar to beads-exec: examine how `Invoke` calls `exec.CommandContext`.
2. Use the `TestHelperProcess` pattern or extract a command runner interface.
3. Test `Invoke` with:
   - Successful invocation returning valid JSON with result, cost, duration, session_id
   - Claude binary returning non-zero exit code
   - Invalid JSON output
   - Context cancellation
4. Test `Validate` with:
   - Binary found on PATH
   - Binary not found

## Files

- `internal/claude/claude_test.go` — extend with exec tests
- `internal/claude/claude.go` — read, minimal modifications only if needed

## Scope

- `internal/claude/` only
