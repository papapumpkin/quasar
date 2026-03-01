+++
id = "cross-invocation"
title = "Guarantee byte-identical system prompts across all invocations within a phase"
type = "task"
priority = 1
depends_on = ["stable-prefix"]
labels = ["quasar", "cache", "cost-optimization"]
scope = ["internal/loop/loop.go", "internal/agent/prompt.go"]
+++

## Problem

Even after phase 02 separates stable and volatile content, the system prompt is still rebuilt from scratch on every invocation. In the current code, `coderAgent()` and `reviewerAgent()` call `agent.BuildSystemPrompt()` each time they are invoked — once per cycle per role. Within a single phase:

- Cycle 1: `coderAgent()` calls `BuildSystemPrompt(l.CoderPrompt, opts)` -> builds system prompt
- Cycle 1: `reviewerAgent()` calls `BuildSystemPrompt(l.ReviewPrompt, opts)` -> builds system prompt
- Cycle 2: `coderAgent()` calls `BuildSystemPrompt(l.CoderPrompt, opts)` -> rebuilds system prompt
- Cycle 2: `reviewerAgent()` calls `BuildSystemPrompt(l.ReviewPrompt, opts)` -> rebuilds system prompt

While the inputs are the same (same `l.CoderPrompt`, same `l.ProjectContext`, same `l.FabricEnabled`), the system prompt is rebuilt N times. This is not a correctness problem — `strings.Builder` produces identical output for identical inputs — but it is wasteful and fragile. Any future change that accidentally introduces non-determinism (e.g., a timestamp, a map iteration) would silently break cache hits.

Additionally, the `--system-prompt` flag value is what the Claude CLI uses to compute the cache key. For cross-role cache hits (coder and reviewer sharing a cached prefix), the system prompts need to share a common prefix. Currently, coder and reviewer have DIFFERENT base prompts (`l.CoderPrompt` vs `l.ReviewPrompt`), so their system prompts diverge after the `ProjectContext` segment. This means cross-role caching only works for the `ProjectContext` prefix — which is already the case, but should be explicitly guaranteed.

## Solution

### 1. Pre-compute system prompts at loop initialization

Move system prompt construction from `coderAgent()`/`reviewerAgent()` (called per cycle) to `initCycleState()` or `runLoop()` (called once per phase). Store the pre-computed prompts on the `Loop` struct:

```go
// In Loop struct, add:
type Loop struct {
    // ... existing fields ...

    // cachedCoderSystemPrompt is the pre-computed system prompt for the coder
    // agent, built once at phase start. It contains only stable content
    // (ProjectContext + CoderPrompt + FabricProtocol) and must remain
    // byte-identical across all cycles for prompt cache hits.
    cachedCoderSystemPrompt string

    // cachedReviewerSystemPrompt is the pre-computed system prompt for the
    // reviewer agent, built once at phase start.
    cachedReviewerSystemPrompt string
}
```

Build them once in `runLoop()` before the cycle loop begins:

```go
func (l *Loop) runLoop(ctx context.Context, beadID, taskDescription string) (*TaskResult, error) {
    // Pre-compute system prompts once for the entire phase.
    // These are stable: same ProjectContext + basePrompt + FabricProtocol.
    opts := agent.PromptOpts{
        FabricEnabled:  l.FabricEnabled,
        TaskID:         l.TaskID,
        ProjectContext: l.ProjectContext,
    }
    l.cachedCoderSystemPrompt = agent.BuildSystemPrompt(l.CoderPrompt, opts)
    l.cachedReviewerSystemPrompt = agent.BuildSystemPrompt(l.ReviewPrompt, opts)

    perAgentBudget := l.perAgentBudget()
    state := l.initCycleState(ctx, beadID, taskDescription)
    // ...
}
```

### 2. Simplify `coderAgent()` and `reviewerAgent()`

Replace the per-call `BuildSystemPrompt` with the cached value:

```go
func (l *Loop) coderAgent(budget float64) agent.Agent {
    return agent.Agent{
        Role:         agent.RoleCoder,
        SystemPrompt: l.cachedCoderSystemPrompt,
        Model:        l.Model,
        MaxBudgetUSD: budget,
        AllowedTools: []string{
            "Read", "Edit", "Write", "Glob", "Grep",
            "Bash(go *)", "Bash(git diff *)", "Bash(git status)", "Bash(git log *)",
        },
        MCP: l.MCP,
    }
}

func (l *Loop) reviewerAgent(budget float64) agent.Agent {
    return agent.Agent{
        Role:         agent.RoleReviewer,
        SystemPrompt: l.cachedReviewerSystemPrompt,
        Model:        l.Model,
        MaxBudgetUSD: budget,
        AllowedTools: []string{
            "Read", "Glob", "Grep",
            "Bash(go vet *)", "Bash(git diff *)", "Bash(git log *)",
        },
        MCP: l.MCP,
    }
}
```

### 3. Add a cross-invocation identity assertion

Add a debug assertion (active when `Verbose` is true or in tests) that verifies the system prompt passed to `Invoker.Invoke()` is identical to the cached version. This catches any future code that accidentally modifies the system prompt between cycles:

```go
// assertSystemPromptStable is a debug check that verifies the system prompt
// has not been mutated since it was cached at phase start. This guards against
// accidental cache-busting changes introduced in future code.
func assertSystemPromptStable(cached, actual string) {
    if cached != actual {
        panic(fmt.Sprintf("BUG: system prompt mutated between cycles (cached len=%d, actual len=%d)", len(cached), len(actual)))
    }
}
```

### 4. Cross-phase caching via shared ProjectContext prefix

For nebula runs with multiple phases, all phases receive the same `ProjectContext` from `snapshot.Scanner.Scan()` (it is deterministic for a given repo state). Since `BuildSystemPrompt` places `ProjectContext` first, the system prompts for different phases share a common prefix:

```
Phase A (coder): [ProjectContext] + [CoderPrompt_A] + [FabricProtocol]
Phase B (coder): [ProjectContext] + [CoderPrompt_B] + [FabricProtocol]
                  ^^^^^^^^^^^^^^^^
                  shared prefix → cache hit for this segment
```

The nebula orchestrator already passes the same `ProjectContext` to all phase loops. Document this guarantee with a comment in the nebula apply code and add a test that verifies two different `Loop` instances with the same `ProjectContext` produce system prompts that share a common prefix.

## Files

- `internal/loop/loop.go` — add `cachedCoderSystemPrompt`/`cachedReviewerSystemPrompt` fields to `Loop`, pre-compute in `runLoop()`, update `coderAgent()`/`reviewerAgent()` to use cached values
- `internal/loop/loop_test.go` — test that system prompt is identical across cycles 1 and 2; test that two loops with same `ProjectContext` share a system prompt prefix
- `internal/agent/prompt_test.go` — test that `BuildSystemPrompt` is deterministic (same inputs -> same output byte-for-byte)

## Acceptance Criteria

- [ ] System prompts for coder and reviewer are built once per phase, not once per cycle
- [ ] `coderAgent()` and `reviewerAgent()` use the pre-computed cached system prompt
- [ ] Test proves: given the same `Loop` configuration, `coderAgent()` returns the same `SystemPrompt` string on cycle 1 and cycle 5
- [ ] Test proves: `BuildSystemPrompt(base, opts)` is deterministic (called twice with same args -> identical output)
- [ ] Test proves: two `Loop` instances with the same `ProjectContext` but different `CoderPrompt` values produce system prompts that share a `ProjectContext`-length common prefix
- [ ] No functional regression: `go test ./internal/loop/...` passes
- [ ] `go vet ./...` clean
