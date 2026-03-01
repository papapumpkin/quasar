+++
id = "cache-telemetry"
title = "Add telemetry to track prompt cache hit potential per invocation"
type = "task"
priority = 2
depends_on = ["cross-invocation"]
labels = ["quasar", "cache", "cost-optimization"]
scope = ["internal/loop/hooks.go", "internal/telemetry/telemetry.go", "internal/loop/loop.go"]
+++

## Problem

After phases 01-03 restructure prompts for caching, we have no way to verify that cache hits are actually occurring or to detect regressions. The Claude CLI's JSON output (`CLIResponse` in `internal/claude/types.go`) reports `total_cost_usd` but does not break down cached vs uncached input tokens. We need our own telemetry to track:

1. Whether the system prompt is identical across invocations (our side — prefix stability)
2. The system prompt size (larger stable prefixes = more savings per cache hit)
3. The user prompt size (volatile content that is never cached)
4. Cost per invocation to detect anomalies that suggest cache misses

### Current telemetry

The `telemetry.Emitter` (in `internal/telemetry/telemetry.go`) writes JSONL events with kinds like `KindAgentStart`, `KindAgentDone`, `KindCycleStart`, `KindCycleDone`. The loop's `Event` type (in `internal/loop/hooks.go`) carries `Kind`, `Cycle`, `Agent`, `BeadID`, `Result` (`*agent.InvocationResult`), and `Message`.

The `InvocationResult` struct (in `internal/agent/agent.go`) has `ResultText`, `CostUSD`, `DurationMs`, `SessionID` — no prompt size or cache metrics.

## Solution

### 1. Extend `InvocationResult` with prompt metrics

Add fields to `agent.InvocationResult` to capture prompt dimensions:

```go
type InvocationResult struct {
    ResultText       string
    CostUSD          float64
    DurationMs       int64
    SessionID        string
    SystemPromptLen  int    // Length of the system prompt in bytes
    UserPromptLen    int    // Length of the user prompt in bytes
    SystemPromptHash string // SHA-256 hex digest of the system prompt (for cache identity tracking)
}
```

Populate these in `claude.Invoker.Invoke()` before the subprocess call:

```go
func (inv *Invoker) Invoke(ctx context.Context, a agent.Agent, prompt string, workDir string) (agent.InvocationResult, error) {
    args := buildArgs(a, prompt)
    // ... existing subprocess logic ...

    return agent.InvocationResult{
        ResultText:       resp.Result,
        CostUSD:          resp.TotalCostUSD,
        DurationMs:       resp.DurationMs,
        SessionID:        resp.SessionID,
        SystemPromptLen:  len(a.SystemPrompt),
        UserPromptLen:    len(prompt),
        SystemPromptHash: sha256Hex(a.SystemPrompt),
    }, nil
}
```

Add a small helper `sha256Hex` in `internal/claude/claude.go`:

```go
func sha256Hex(s string) string {
    h := sha256.Sum256([]byte(s))
    return hex.EncodeToString(h[:])
}
```

### 2. Add a `KindCacheMetrics` telemetry event kind

In `internal/telemetry/telemetry.go`, add:

```go
const (
    // ... existing kinds ...
    KindCacheMetrics = "cache_metrics"
)
```

### 3. Emit cache metrics after each invocation

In `Loop.runCoderPhase()` and `Loop.runReviewerPhase()`, after the `Invoker.Invoke()` call returns, emit a cache metrics event via the hook system:

```go
// In runCoderPhase, after successful invoke:
l.emit(ctx, Event{
    Kind:   EventCacheMetrics,
    BeadID: state.TaskBeadID,
    Cycle:  state.Cycle,
    Agent:  "coder",
    Result: &result,
    Message: fmt.Sprintf("sys_prompt_hash=%s sys_prompt_len=%d user_prompt_len=%d cost=%.4f",
        result.SystemPromptHash, result.SystemPromptLen, result.UserPromptLen, result.CostUSD),
})
```

Add `EventCacheMetrics` to the `EventKind` enum in `internal/loop/hooks.go`:

```go
const (
    EventCycleStart EventKind = iota
    EventAgentDone
    EventReviewComplete
    EventTaskSuccess
    EventTaskFailed
    EventRefactored
    EventCacheMetrics // New: emitted after each agent invocation with prompt size/hash info
)
```

### 4. Log cache hit potential in UI

When `Verbose` is true, log a summary to stderr via `ui.Printer` after each invocation:

```go
if inv.Verbose {
    if state.Cycle > 1 && result.SystemPromptHash == state.prevSystemPromptHash {
        l.UI.Verbose("cache: system prompt STABLE (hash match, %d bytes cached)", result.SystemPromptLen)
    } else if state.Cycle > 1 {
        l.UI.Verbose("cache: system prompt CHANGED (cache miss, prev=%s curr=%s)",
            state.prevSystemPromptHash[:8], result.SystemPromptHash[:8])
    }
}
```

Add `prevSystemPromptHash` as a transient field on `CycleState`:

```go
type CycleState struct {
    // ... existing fields ...
    prevSystemPromptHash string // transient: tracks system prompt hash from previous invocation for cache hit detection
}
```

### 5. Aggregate cache metrics in TaskResult

Add a summary to `TaskResult` so the nebula orchestrator can report cache effectiveness:

```go
type TaskResult struct {
    // ... existing fields ...
    CacheHitCount  int // Number of invocations where system prompt hash matched previous
    CacheMissCount int // Number of invocations where system prompt hash changed
    TotalCachedBytes int64 // Sum of SystemPromptLen for cache-hit invocations
}
```

## Files

- `internal/agent/agent.go` — add `SystemPromptLen`, `UserPromptLen`, `SystemPromptHash` fields to `InvocationResult`
- `internal/claude/claude.go` — populate new fields in `Invoke()`, add `sha256Hex` helper
- `internal/telemetry/telemetry.go` — add `KindCacheMetrics` constant
- `internal/loop/hooks.go` — add `EventCacheMetrics` to `EventKind` enum
- `internal/loop/state.go` — add `prevSystemPromptHash` to `CycleState`, add cache counters to `TaskResult`
- `internal/loop/loop.go` — emit `EventCacheMetrics` in `runCoderPhase` and `runReviewerPhase`, update `prevSystemPromptHash` tracking
- `internal/claude/claude_test.go` — test that `sha256Hex` produces correct output, test that `Invoke` populates prompt metrics
- `internal/loop/loop_test.go` — test that cache hit/miss counts are correctly tracked across cycles

## Acceptance Criteria

- [ ] `InvocationResult` has `SystemPromptLen`, `UserPromptLen`, and `SystemPromptHash` fields
- [ ] `claude.Invoker.Invoke()` populates all three fields before returning
- [ ] `sha256Hex` helper produces correct SHA-256 hex digests (verified by test)
- [ ] `KindCacheMetrics` telemetry event kind exists
- [ ] `EventCacheMetrics` event is emitted after every coder and reviewer invocation
- [ ] `CycleState.prevSystemPromptHash` tracks the previous invocation's hash
- [ ] When verbose mode is on, cache hit/miss status is logged to stderr
- [ ] `TaskResult` includes `CacheHitCount`, `CacheMissCount`, and `TotalCachedBytes`
- [ ] Tests verify that two consecutive invocations with the same system prompt produce a cache hit count of 1
- [ ] `go test ./internal/loop/...` passes
- [ ] `go test ./internal/claude/...` passes
- [ ] `go test ./internal/agent/...` passes
- [ ] `go vet ./...` clean
