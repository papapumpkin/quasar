+++
id = "stable-prefix"
title = "Ensure system prompt is a stable, cache-friendly prefix"
type = "task"
priority = 1
depends_on = ["prompt-audit"]
labels = ["quasar", "cache", "cost-optimization"]
scope = ["internal/agent/prompt.go", "internal/loop/prompts.go"]
+++

## Problem

The system prompt built by `agent.BuildSystemPrompt()` is already structured correctly for caching: `[ProjectContext] -> [basePrompt] -> [FabricProtocol]`. All three segments are stable within a phase. However, the current code in `Loop.composeContextPrefix()` mixes stable project context into the **user prompt** alongside volatile fabric state, which means:

1. Project context is duplicated — once in the system prompt (via `BuildSystemPrompt`) and once in the user prompt (via `composeContextPrefix`).
2. The fabric snapshot is composed alongside project context via `ContextBudget.Compose()`, but the snapshot changes every cycle (new claims, pulses, completed phases), polluting the stable prefix budget.

The fix is straightforward: project context belongs exclusively in the system prompt (already there), and fabric snapshot belongs exclusively in the user prompt (volatile section).

### Current flow (problematic)

```
runCoderPhase():
  prompt = l.buildCoderPrompt(state)          // volatile: task + findings
  prompt = relayBlock + "\n" + prompt          // volatile: hail relays
  prompt = l.composeContextPrefix(ctx, prompt) // MIXES stable (project ctx) + volatile (fabric snap) into user prompt
  agent  = l.coderAgent(budget)                // system prompt already has project ctx via BuildSystemPrompt
  result = l.Invoker.Invoke(ctx, agent, prompt, l.WorkDir)
```

Project context appears in both the system prompt AND the user prompt. The system prompt copy enables caching; the user prompt copy wastes tokens.

## Solution

### 1. Refactor `composeContextPrefix` to be volatile-only

Rename `composeContextPrefix` to `composeVolatilePrefix` and remove project context from it. It should only inject volatile content (fabric snapshot) into the user prompt:

```go
// composeVolatilePrefix prepends volatile context (fabric snapshot) to the task
// prompt. Stable content (project context) is handled by BuildSystemPrompt and
// must NOT be duplicated here.
func (l *Loop) composeVolatilePrefix(ctx context.Context, taskPrompt string) string {
    if !l.FabricEnabled || l.Fabric == nil {
        return taskPrompt
    }

    snap := l.buildFabricSnapshot(ctx)
    var b strings.Builder
    b.WriteString("## Current Fabric State\n\n")
    b.WriteString(fabric.RenderSnapshot(snap))
    b.WriteString("\n\n---\n\n")
    b.WriteString(taskPrompt)
    return b.String()
}
```

This removes the `ContextBudget.Compose()` call that mixed stable and volatile content. The project context is now exclusively in the system prompt via `BuildSystemPrompt`.

### 2. Update `runCoderPhase` and `runReviewerPhase`

In `runCoderPhase` (loop.go line ~432-437), change:

```go
// Before (mixes stable + volatile in user prompt):
prompt := l.buildCoderPrompt(state)
relayBlock, relayIDs := l.pendingHailRelay()
if relayBlock != "" {
    prompt = relayBlock + "\n" + prompt
}
prompt = l.composeContextPrefix(ctx, prompt)

// After (only volatile content in user prompt):
prompt := l.buildCoderPrompt(state)
relayBlock, relayIDs := l.pendingHailRelay()
if relayBlock != "" {
    prompt = relayBlock + "\n" + prompt
}
prompt = l.composeVolatilePrefix(ctx, prompt)
```

Apply the same change in `runReviewerPhase` if it uses `composeContextPrefix`.

### 3. Verify `BuildSystemPrompt` is the sole owner of stable content

Confirm that `coderAgent()` and `reviewerAgent()` (loop.go lines ~379-418) pass `ProjectContext` to `BuildSystemPrompt` via `PromptOpts`. They already do:

```go
func (l *Loop) coderAgent(budget float64) agent.Agent {
    sysPrompt := agent.BuildSystemPrompt(l.CoderPrompt, agent.PromptOpts{
        FabricEnabled:  l.FabricEnabled,
        TaskID:         l.TaskID,
        ProjectContext: l.ProjectContext,  // stable prefix
    })
    // ...
}
```

This is correct. No changes needed to agent construction.

### 4. Handle backward compatibility

When `MaxContextTokens` is 0 and `ProjectContext` is empty, the current `composeContextPrefix` falls back to `PrependFabricContext()`. The new `composeVolatilePrefix` should preserve this fallback:

```go
func (l *Loop) composeVolatilePrefix(ctx context.Context, taskPrompt string) string {
    if !l.FabricEnabled || l.Fabric == nil {
        return taskPrompt
    }
    snap := l.buildFabricSnapshot(ctx)
    return PrependFabricContext(taskPrompt, snap)
}
```

This keeps `PrependFabricContext` for backward compat while removing the `ContextBudget.Compose()` path that mixed stable content into the user prompt.

## Files

- `internal/loop/prompts.go` — rename `composeContextPrefix` to `composeVolatilePrefix`, remove project context injection from user prompt path
- `internal/loop/loop.go` — update `runCoderPhase` (and `runReviewerPhase` if applicable) to call `composeVolatilePrefix`
- `internal/loop/prompts_test.go` — add/update tests verifying that `composeVolatilePrefix` does NOT include project context, only fabric state

## Acceptance Criteria

- [ ] `composeContextPrefix` renamed to `composeVolatilePrefix` (or equivalent refactor)
- [ ] Project context is NOT present in the user prompt (only in system prompt via `BuildSystemPrompt`)
- [ ] Fabric snapshot IS present in the user prompt when `FabricEnabled` is true
- [ ] Hail relay blocks remain in the user prompt (volatile)
- [ ] `coderAgent()` and `reviewerAgent()` continue passing `ProjectContext` to `BuildSystemPrompt`
- [ ] Backward compatibility preserved: when `FabricEnabled` is false and `ProjectContext` is empty, user prompt is unchanged
- [ ] Test confirms system prompt is identical between cycle 1 and cycle 2 of the same phase (same `ProjectContext`, same `basePrompt`)
- [ ] `go test ./internal/loop/...` passes
- [ ] `go test ./internal/agent/...` passes
- [ ] `go vet ./...` clean
