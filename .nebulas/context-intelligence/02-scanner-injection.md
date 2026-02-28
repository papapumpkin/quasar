+++
id = "scanner-injection"
title = "Inject project snapshot into agent system prompts"
type = "feature"
priority = 1
depends_on = ["project-scanner"]
scope = ["internal/agent/prompt.go", "internal/agent/prompt_test.go", "internal/loop/loop.go"]
+++

## Problem

The project scanner from phase 1 produces a snapshot, but nothing wires it into agent invocations. The snapshot needs to be injected into the system prompt (not the user prompt) because Anthropic's prompt caching operates on system prompt prefixes — the earlier and more stable the content, the higher the cache hit rate.

Currently `agent.BuildSystemPrompt` only handles the fabric protocol. It needs to also handle the project context prefix.

## Solution

### 1. Extend PromptOpts

Add the project snapshot to the existing `PromptOpts` struct in `internal/agent/prompt.go`:

```go
type PromptOpts struct {
    FabricEnabled  bool
    TaskID         string
    ProjectContext string // Deterministic project snapshot (prepended to system prompt)
}
```

### 2. Update BuildSystemPrompt

Prepend the project context before the base prompt. The order matters for cache stability:

```
[ProjectContext]     ← stable across all invocations (highest cache value)
[Base system prompt] ← role-specific (coder vs reviewer)
[Fabric protocol]    ← conditional on FabricEnabled
```

```go
func BuildSystemPrompt(base string, opts PromptOpts) string {
    var b strings.Builder
    if opts.ProjectContext != "" {
        b.WriteString(opts.ProjectContext)
        b.WriteString("\n\n---\n\n")
    }
    b.WriteString(base)
    if opts.FabricEnabled {
        b.WriteString("\n\n")
        b.WriteString(FabricProtocol)
    }
    return b.String()
}
```

### 3. Wire into Loop

Add a `ProjectContext` field to `Loop`:

```go
type Loop struct {
    // ... existing fields
    ProjectContext string // Injected into agent system prompts for prompt caching
}
```

Both `coderAgent()` and `reviewerAgent()` pass it through `PromptOpts`. Currently only `coderAgent` calls `BuildSystemPrompt` — extend `reviewerAgent` to do the same so both roles benefit from cached context.

### 4. Wire into nebula apply

In `cmd/nebula_apply.go` (or `cmd/nebula_adapters.go`), scan once at nebula start and pass the snapshot through to all phase loops:

```go
scanner := &snapshot.Scanner{WorkDir: workDir}
projectCtx, err := scanner.Scan(ctx)
// ... pass projectCtx to each Loop via the adapter
```

The scan happens once, the result is reused for every phase and every cycle. This is where the cache savings compound.

## Files

- `internal/agent/prompt.go` — Add `ProjectContext` to `PromptOpts`, update `BuildSystemPrompt`
- `internal/agent/prompt_test.go` — Test system prompt ordering (context → base → protocol)
- `internal/loop/loop.go` — Add `ProjectContext` field, pass through `coderAgent()` and `reviewerAgent()`
- `internal/loop/loop_test.go` — Update `TestCoderAgent` and `TestReviewerAgent` for new field
- `cmd/nebula_apply.go` — Scan project context once at startup
- `cmd/nebula_adapters.go` — Pass `ProjectContext` through to phase loops

## Acceptance Criteria

- [ ] `BuildSystemPrompt` prepends project context before the base prompt when provided
- [ ] Project context appears before base prompt and fabric protocol in the system prompt
- [ ] Empty `ProjectContext` produces identical output to current behavior (backward compatible)
- [ ] Both `coderAgent()` and `reviewerAgent()` include project context in their system prompt
- [ ] `quasar nebula apply` scans the project once and reuses the snapshot across all phases
- [ ] `quasar run` also supports `--project-context` flag (opt-in for single-task mode)
- [ ] `go test ./internal/agent/...` and `go test ./internal/loop/...` pass
- [ ] `go vet ./...` clean
