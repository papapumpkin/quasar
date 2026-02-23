+++
id = "agent-context-prefix"
title = "Add context prefix support to agent invocation"
type = "feature"
priority = 1
depends_on = ["context-scanner"]
+++

## Problem

The agent system (`internal/agent/agent.go` and `internal/claude/claude.go`) has no mechanism to prepend a shared context block to system prompts. We need to add this so the project snapshot can be injected into every agent call.

The key requirement for prompt caching: the **system prompt prefix must be identical** across all invocations. If we prepend the snapshot to `Agent.SystemPrompt`, then all coders and reviewers in a nebula run share the same cached prefix.

## Solution

### 1. Add `ContextPrefix` to `Agent`

In `internal/agent/agent.go`, add a `ContextPrefix string` field to the `Agent` struct. This is the project snapshot that gets prepended to the system prompt.

### 2. Prepend in `buildArgs`

In `internal/claude/claude.go`, modify `buildArgs` to combine `ContextPrefix + SystemPrompt` when both are present:

```go
systemPrompt := a.SystemPrompt
if a.ContextPrefix != "" {
    systemPrompt = a.ContextPrefix + "\n\n" + systemPrompt
}
if systemPrompt != "" {
    args = append(args, "--system-prompt", systemPrompt)
}
```

This ensures:
- The context prefix is the **first** part of the system prompt (maximizes cache hit window)
- All agents (coder, reviewer, architect) sharing the same `ContextPrefix` value hit the same cache
- When `ContextPrefix` is empty, behavior is unchanged

### 3. Wire through `Loop`

Add a `ContextPrefix string` field to `loop.Loop`. In `coderAgent()` and `reviewerAgent()`, pass `l.ContextPrefix` to the `Agent.ContextPrefix` field.

## Files

- `internal/agent/agent.go` — add `ContextPrefix` field to `Agent` struct
- `internal/claude/claude.go` — modify `buildArgs` to prepend context
- `internal/loop/loop.go` — add `ContextPrefix` field, wire to agents

## Acceptance Criteria

- [ ] `Agent.ContextPrefix` field exists and is documented
- [ ] `buildArgs` prepends `ContextPrefix` to system prompt when non-empty
- [ ] When `ContextPrefix` is empty, `buildArgs` output is identical to before (no regression)
- [ ] `Loop.ContextPrefix` is propagated to both coder and reviewer agents
- [ ] Existing tests still pass
