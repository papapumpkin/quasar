+++
id = "prompt-cache-audit-and-fix"
title = "Verify the prompt cache actually hits — pass --exclude-dynamic-system-prompt-sections to claude, capture cache token telemetry, surface hit rate in the TUI"
type = "task"
priority = 2
scope = [
    "internal/claude/claude.go",
    "internal/claude/claude_test.go",
    "internal/agent/prompt_layout.go",
    "internal/telemetry/cache_metrics.go",
    "internal/telemetry/cache_metrics_test.go",
    "internal/tui/cache_panel.go",
]
+++

## Problem

The two-zone prompt layout (`internal/agent/prompt_layout.go`) is correct: stable content forms a contiguous prefix in the system prompt, volatile content goes in the user prompt. The `prompt-cache-aware` skill nudges the coder away from re-reads. The `CacheOptimization` config flag is plumbed through every adapter.

But three plumbing gaps mean the cache may not be hitting at the rate the architecture promises:

1. **`buildArgs` in `internal/claude/claude.go` does not pass `--exclude-dynamic-system-prompt-sections`.** The claude CLI's `--help` says this flag "Improves cross-user prompt-cache reuse" by lifting dynamic content (timestamps, env) out of the system prompt. Without it, the system prompt prefix can change byte-by-byte across invocations and the cache key invalidates.
2. **No telemetry captures `cache_creation_input_tokens` vs `cache_read_input_tokens`** from the claude CLI's JSON response. We cannot tell if the cache is working.
3. **The `--cache-optimization` Cobra flag drives `agent.PromptLayout` behavior but never reaches the claude subprocess argv.** A user toggling it off has no effect on the actual subprocess.

The fix is small but unblocks everything downstream — once we can measure hit rate, every later phase has a quantitative signal.

## Solution

### Pass the right flags

`internal/claude/claude.go`, in `buildArgs`:

```go
func buildArgs(a agent.Agent, prompt string) []string {
    args := []string{
        "-p", prompt,
        "--output-format", "json",
    }

    if a.SystemPrompt != "" {
        args = append(args, "--system-prompt", a.SystemPrompt)
    }

    // NEW: keep dynamic content out of the system prompt so the cache
    // prefix is byte-stable across invocations.
    if a.CacheOptimization {
        args = append(args, "--exclude-dynamic-system-prompt-sections")
    }

    // ... rest unchanged
}
```

The corresponding `CacheOptimization bool` field gets added to `agent.Agent` and threaded from the existing config. `cmd/run.go`'s `--cache-optimization` flag already reaches `cfg.CacheOptimization`; we just need to propagate it to the `Agent` struct on each invocation.

### Capture cache token telemetry

The claude CLI returns JSON with a `usage` block. Capture the cache fields:

```go
type ClaudeUsage struct {
    InputTokens             int     `json:"input_tokens"`
    OutputTokens            int     `json:"output_tokens"`
    CacheCreationInputTokens int    `json:"cache_creation_input_tokens"` // NEW
    CacheReadInputTokens    int     `json:"cache_read_input_tokens"`     // NEW
}
```

`internal/telemetry/cache_metrics.go` records per-invocation:

```go
type CacheMetric struct {
    InvocationID   string
    NebulaID       string
    PhaseID        string
    CycleN         int
    InputTokens    int
    CacheCreate    int
    CacheRead      int
    CacheHitRatio  float64 // CacheRead / (CacheRead + InputTokens), 0..1
    Timestamp      time.Time
}

func (m *CacheMetricStore) Record(ctx context.Context, metric CacheMetric) error
func (m *CacheMetricStore) HitRateByPhase(ctx context.Context, nebulaID string) (map[string]float64, error)
func (m *CacheMetricStore) HitRateByCycle(ctx context.Context, nebulaID, phaseID string) ([]float64, error)
```

Stored in `.quasar/telemetry/cache_metrics.jsonl` (append-only, ≤100B per line, daily rotation).

### Surface in the TUI

`internal/tui/cache_panel.go` adds a small footer panel to the fleet detail view:

```
Cache: read 18.2k / created 1.4k / fresh 2.1k  →  hit 84% (last 5 cycles)
```

Three numbers from the last N invocations of the current phase. The hit-rate is the killer metric — if it's < 50% the architecture isn't paying off and we've got a regression to find.

### Validation: measure a known-good cycle

A small CLI command `quasar cache report --nebula <id>` walks the JSONL log and prints per-phase, per-cycle hit rate plus a global average. The acceptance criterion below requires it to print non-zero `cache_read_input_tokens` on the SECOND invocation of the same phase — the first invocation populates the cache, the second should reuse it.

### What this does NOT do

- Does not add caching infrastructure where none exists. The two-zone layout is already correct.
- Does not modify the volatile suffix.
- Does not change the prompt-cache-aware skill (the coder-side behavior is fine as-is).

## Files

- `internal/claude/claude.go` (modify) — pass `--exclude-dynamic-system-prompt-sections` when `CacheOptimization=true`
- `internal/claude/claude_test.go` (modify) — assert the new arg is present when enabled, absent when disabled
- `internal/agent/agent.go` (modify) — add `CacheOptimization bool` field
- `internal/agent/prompt_layout.go` (modify if needed) — wire the new field through if not already
- `internal/telemetry/cache_metrics.go` (new) — CacheMetric struct, CacheMetricStore, JSONL writer
- `internal/telemetry/cache_metrics_test.go` (new)
- `internal/tui/cache_panel.go` (new) — small read-only panel
- `cmd/cache.go` (new) — `quasar cache report --nebula <id>` walker

## Acceptance Criteria

- [ ] `claude.buildArgs(agent, prompt)` with `agent.CacheOptimization=true` includes `--exclude-dynamic-system-prompt-sections` in its returned argv
- [ ] Same call with `CacheOptimization=false` does NOT include the flag
- [ ] `claude.Invoker.Invoke` parses `cache_creation_input_tokens` and `cache_read_input_tokens` from the response JSON
- [ ] `CacheMetricStore.Record` appends a JSONL line with all required fields
- [ ] `quasar cache report --nebula <id>` walks the log and prints per-phase hit rate
- [ ] In an end-to-end test that fires two consecutive invocations of the same phase, the SECOND invocation's `cache_read_input_tokens` is > 0
- [ ] In the same test, the global cache hit ratio computed from the metric is > 0
- [ ] TUI cache panel renders the three numbers + hit% on the fleet detail view
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
