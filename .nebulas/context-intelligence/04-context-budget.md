+++
id = "context-budget"
title = "Token-budget-aware context composition"
type = "feature"
priority = 2
depends_on = ["fabric-auto-inject"]
scope = ["internal/snapshot/budget.go", "internal/loop/prompts.go"]
+++

## Problem

With three context layers (project snapshot, fabric state, prior work), the total injected context can grow large — especially in nebulas with many phases producing many entanglements. An 8-phase nebula with 20 entanglements, 15 file claims, and 5 discoveries could produce a fabric snapshot of 3-5K tokens. Combined with a project snapshot (~8K tokens) and prior work, we could easily inject 15K+ tokens per invocation.

This needs a budget system that composes all context layers within a configurable ceiling, prioritizing the most valuable information and truncating gracefully.

## Solution

### 1. Context budget type

Create `internal/snapshot/budget.go`:

```go
// ContextBudget manages token budget allocation across context layers.
type ContextBudget struct {
    MaxTokens int // Total token budget for all context (default 10000)
}

// Compose assembles context layers within the token budget.
// Layers are prioritized in order: project (most cacheable) > fabric (most actionable) > prior work.
func (cb *ContextBudget) Compose(project, fabric, priorWork string) string
```

### 2. Budget allocation strategy

Priority order (higher = allocated first):
1. **Project context** (40% of budget) — most stable, highest cache hit value
2. **Fabric state** (40% of budget) — most actionable for current execution
3. **Prior work** (20% of budget) — helpful but not critical

If a layer is under budget, its unused allocation rolls to the next layer. If a layer exceeds its allocation, truncate with a clear `[... truncated, N items omitted]` marker.

### 3. Token estimation

Use a simple heuristic: 1 token ≈ 4 characters. This is accurate enough for budget management without needing a real tokenizer. The `MaxTokens` default of 10000 maps to ~40K characters.

### 4. Truncation strategy

For structured content (fabric state), truncation is section-aware:
- Keep all section headers
- Truncate the longest section first (usually entanglements)
- Within entanglements, keep the first N and add `... and M more entanglements`
- Always preserve discoveries (they're actionable blockers)

For free-text content (CLAUDE.md, prior work), truncate at the end with `[truncated]`.

### 5. Wire into prompt construction

Update `buildCoderPrompt` to use the budget:

```go
budget := &snapshot.ContextBudget{MaxTokens: 10000}
composed := budget.Compose(l.ProjectContext, fabricState, priorWorkContext)
// composed is injected as a single prefix block
```

### 6. Configuration

Add `max_context_tokens` to `[execution]` in `nebula.toml`:

```toml
[execution]
max_context_tokens = 10000  # 0 = no context injection
```

And a `--max-context-tokens` flag on `quasar run` and `quasar nebula apply`.

## Files

- `internal/snapshot/budget.go` — `ContextBudget` type, `Compose` method, truncation logic
- `internal/snapshot/budget_test.go` — tests for budget allocation, truncation, rollover
- `internal/loop/prompts.go` — Use `ContextBudget.Compose` when building prompts
- `internal/nebula/types.go` — Add `MaxContextTokens` to `Execution` struct
- `internal/nebula/parse.go` — Parse `max_context_tokens` field
- `cmd/nebula_apply.go` — Wire `--max-context-tokens` flag
- `cmd/run.go` — Wire `--max-context-tokens` flag

## Acceptance Criteria

- [ ] `ContextBudget.Compose` produces output within the configured token budget
- [ ] Project context gets highest priority allocation (truncated last)
- [ ] Fabric state truncation preserves discoveries and section headers
- [ ] Unused budget from one layer rolls over to the next
- [ ] `max_context_tokens = 0` disables all context injection
- [ ] Default budget of 10000 tokens works without explicit configuration
- [ ] `nebula.toml` supports `max_context_tokens` in `[execution]`
- [ ] `--max-context-tokens` flag works on both `quasar run` and `quasar nebula apply`
- [ ] `go test ./internal/snapshot/...` passes
- [ ] `go vet ./...` clean
