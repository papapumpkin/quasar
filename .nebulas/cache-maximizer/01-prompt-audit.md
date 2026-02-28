+++
id = "prompt-audit"
title = "Audit and classify all prompt content as stable or volatile"
type = "task"
priority = 1
labels = ["quasar", "cache", "cost-optimization"]
+++

## Problem

The current prompt assembly scatters stable and volatile content across both the system prompt and user prompt without a clear separation. To maximize Anthropic's prompt caching (90% discount on cached input tokens), we need every piece of content classified as either **stable** (identical across invocations within a phase) or **volatile** (changes per cycle or per invocation).

Currently:

- `agent.BuildSystemPrompt()` (in `internal/agent/prompt.go`) assembles: `[ProjectContext] -> [basePrompt] -> [FabricProtocol]`. ProjectContext and basePrompt are stable within a phase, FabricProtocol is stable across all invocations. This is already correct.

- `Loop.composeContextPrefix()` (in `internal/loop/prompts.go`) prepends fabric snapshot and project context to the **task prompt** (user prompt). The fabric snapshot (`fabric.Snapshot` with entanglements, claims, pulses, phase states) changes between cycles as other phases complete work. The project context is stable. These two are mixed together via `ContextBudget.Compose()`.

- `Loop.buildCoderPrompt()` builds cycle-specific content: task description, reviewer findings from previous cycles, refactor instructions. All volatile.

- `Loop.buildReviewerPrompt()` builds cycle-specific content: coder output, lint output, review instructions. All volatile.

- `PrependFabricContext()` in `internal/loop/prompts.go` puts `fabric.RenderSnapshot(snap)` before the task description. The snapshot is volatile (changes per cycle as claims/pulses update).

- Hail relay blocks are prepended to the coder prompt in `runCoderPhase()` — these are volatile and per-invocation.

## Solution

Create a classification document and add it as a Go source file (`internal/agent/prompt_layout.go`) with constants and documentation that codifies the stable/volatile boundary. This becomes the authoritative reference for all subsequent phases.

### Classification

**Stable (system prompt prefix — identical across all invocations in a phase):**
1. `ProjectContext` from `snapshot.Scanner.Scan()` — deterministic, repo-state-dependent only
2. `basePrompt` (`l.CoderPrompt` or `l.ReviewPrompt`) — set once at loop initialization
3. `FabricProtocol` constant — static instructions for fabric interaction

**Volatile (user prompt — changes per cycle):**
1. Task description and bead ID
2. Reviewer findings (`[]ReviewFinding`) from previous cycle
3. Coder output summary from previous cycle
4. Lint output from lint pass
5. Filter output from pre-reviewer checks
6. Fabric snapshot (`fabric.RenderSnapshot()`) — claims, pulses, phase states change per cycle
7. Hail relay blocks — resolved hails injected per invocation
8. Refactor instructions — injected mid-run when user edits task

**Cross-phase stable (identical across all phases in a nebula run):**
1. `ProjectContext` — same repo snapshot used for all phases

### Implementation

Create `internal/agent/prompt_layout.go`:

```go
package agent

// PromptZone classifies where content belongs in the prompt layout
// for maximum cache effectiveness.
type PromptZone int

const (
    // ZoneStablePrefix is content placed in the system prompt that remains
    // byte-identical across all invocations within a phase. This is the
    // primary cache target — Anthropic caches from the beginning of the
    // system prompt, so all stable content must form a contiguous prefix.
    //
    // Content: ProjectContext + basePrompt + FabricProtocol
    ZoneStablePrefix PromptZone = iota

    // ZoneVolatileSuffix is content placed in the user prompt (-p flag)
    // that changes between cycles. This includes task descriptions,
    // findings, lint output, fabric snapshots, and hail relays.
    ZoneVolatileSuffix
)
```

Add a `PromptManifest` struct that records what content was placed in each zone for a given invocation. This will be used by the telemetry phase (phase 04) to verify cache effectiveness:

```go
// PromptManifest records the content placement for a single agent invocation.
// It is used for telemetry and debugging prompt cache behavior.
type PromptManifest struct {
    SystemPromptHash string // SHA-256 of the full system prompt (stable prefix)
    UserPromptLen    int    // Length of the user prompt in bytes
    Zone             map[string]PromptZone // Maps content label to its zone
}
```

### Verification

Write a test `TestPromptZoneClassification` that:
1. Builds a system prompt via `BuildSystemPrompt()` with known inputs
2. Verifies ProjectContext appears before basePrompt
3. Verifies FabricProtocol appears after basePrompt
4. Verifies no volatile content (findings, lint output, fabric snapshot) is present in the system prompt
5. Builds a user prompt via `buildCoderPrompt()` and verifies volatile content is present

## Files

- `internal/agent/prompt_layout.go` — `PromptZone` type, `ZoneStablePrefix`/`ZoneVolatileSuffix` constants, `PromptManifest` struct
- `internal/agent/prompt_layout_test.go` — classification verification tests

## Acceptance Criteria

- [ ] `PromptZone` type with `ZoneStablePrefix` and `ZoneVolatileSuffix` constants defined
- [ ] `PromptManifest` struct defined with `SystemPromptHash`, `UserPromptLen`, and `Zone` fields
- [ ] GoDoc comments on all exported types explain the caching rationale
- [ ] Test verifies `BuildSystemPrompt()` output contains only stable content (ProjectContext, basePrompt, FabricProtocol)
- [ ] Test verifies volatile content (findings, lint output, fabric state) is absent from system prompt output
- [ ] `go test ./internal/agent/...` passes
- [ ] `go vet ./...` clean
