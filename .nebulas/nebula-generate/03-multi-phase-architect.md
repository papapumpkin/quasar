+++
id = "multi-phase-architect"
title = "Extend architect agent to generate complete multi-phase nebulas"
type = "feature"
priority = 1
depends_on = ["codebase-analyzer", "dependency-inference"]
scope = ["internal/nebula/architect.go", "internal/nebula/architect_test.go", "internal/nebula/generate.go", "internal/nebula/generate_test.go"]
allow_scope_overlap = true
+++

## Problem

The current `RunArchitect` function in `internal/nebula/architect.go` generates exactly one phase at a time — it sends a single prompt to the architect agent and parses back a single `PHASE_FILE:...END_PHASE_FILE` block. The `nebula generate` command needs to produce an entire nebula (5-15 phases) from a single user prompt, complete with a manifest, proper dependency ordering, and scope declarations.

Calling `RunArchitect` in a loop would be wasteful and lose cross-phase coherence — each invocation would lack context about what other phases were already generated. The architect needs to see the full decomposition plan at once to assign correct `depends_on` relationships, avoid redundant phases, and distribute scope ownership cleanly.

The existing `ArchitectRequest` has `Mode` (create/refactor) and `PhaseID` fields, but no concept of "generate an entire nebula from scratch." A new mode and supporting orchestration logic are needed.

## Solution

### 1. New Architect Mode

Add a new `ArchitectMode` constant and extend `ArchitectRequest`:

```go
const ArchitectModeGenerate ArchitectMode = "generate"
```

The `ArchitectRequest` struct already has `UserPrompt` and `Nebula` fields. For generate mode, `Nebula` will be a skeleton nebula with just the manifest (no phases yet), and `UserPrompt` is the user's natural-language description of what they want built.

### 2. Generate Orchestrator

Create `internal/nebula/generate.go` with the top-level orchestration function:

```go
// GenerateRequest holds the inputs for generating a complete nebula.
type GenerateRequest struct {
    UserPrompt   string             // What the user wants built
    NebulaName   string             // Name for the generated nebula (kebab-case)
    OutputDir    string             // Directory where the nebula will be written
    WorkDir      string             // Repository root for codebase analysis
    Analysis     *CodebaseAnalysis  // Pre-computed codebase analysis
    Model        string             // Model override (empty = default)
    MaxBudgetUSD float64            // Budget cap for generation itself
}

// GenerateResult holds the output of nebula generation.
type GenerateResult struct {
    Nebula      *Nebula          // The complete generated nebula
    Manifest    Manifest         // Generated manifest
    Phases      []PhaseSpec      // Generated phases with corrected dependencies
    InferResult *InferenceResult // Dependency inference report
    Errors      []string         // Non-fatal warnings
    CostUSD     float64          // Total cost of architect invocations
}

// Generate produces a complete nebula from a natural-language prompt.
// It invokes the architect agent to decompose the prompt into phases,
// runs dependency inference, and validates the result.
func Generate(ctx context.Context, invoker agent.Invoker, req GenerateRequest) (*GenerateResult, error)
```

Implementation flow:

1. Build a scaffold `Manifest` from `req` (name, description derived from prompt, sensible defaults for execution settings).
2. Build the multi-phase architect prompt using `req.Analysis.FormatForPrompt()` as codebase context, the scaffold manifest context, and the user prompt.
3. Invoke the architect agent with `invoker.Invoke(ctx, agent, prompt, req.WorkDir)`.
4. Parse the multi-phase output (multiple `PHASE_FILE:...END_PHASE_FILE` blocks).
5. Apply manifest defaults to each phase via `applyDefaults`.
6. Run `DependencyInferrer.InferDependencies()` to correct the dependency graph.
7. Validate the result with `nebula.Validate`.
8. Return the complete `GenerateResult`.

### 3. Multi-Phase Output Parser

Extend the parsing logic to handle multiple phase blocks:

```go
// parseMultiPhaseOutput extracts multiple phase files from architect output.
// Each phase is delimited by PHASE_FILE: <filename> ... END_PHASE_FILE markers.
func parseMultiPhaseOutput(output string) ([]*ArchitectResult, error)
```

This reuses the existing `parseArchitectOutput` logic but finds all occurrences of the `PHASE_FILE:...END_PHASE_FILE` pattern rather than stopping at the first one.

### 4. Multi-Phase Architect Prompt

Add a new prompt builder for generate mode in `buildArchitectPrompt`:

```go
case ArchitectModeGenerate:
    b.WriteString("## Task: Generate a Complete Nebula\n\n")
    fmt.Fprintf(&b, "User request: %s\n\n", req.UserPrompt)
    b.WriteString("## Codebase Context\n\n")
    // Embed CodebaseAnalysis formatted output
    b.WriteString("Decompose the user's request into 3-15 focused phases...\n")
    b.WriteString("Each phase must be a coherent unit of work for a single coder-reviewer loop.\n")
    b.WriteString("Assign `depends_on` based on data/code dependencies between phases.\n")
    b.WriteString("Assign `scope` glob patterns to each phase for file ownership.\n\n")
    b.WriteString("Output ALL phases using the PHASE_FILE/END_PHASE_FILE format.\n")
```

### 5. Manifest Generation

```go
// buildManifest constructs a default Manifest from a GenerateRequest.
func buildManifest(req GenerateRequest) Manifest
```

This creates a manifest with sensible defaults: `max_workers = 2`, `max_review_cycles = 5`, `gate = "review"`, and the goals/constraints derived from the user prompt.

### 6. System Prompt Extension

Extend `architectSystemPrompt` with multi-phase output instructions. The existing prompt already documents the `PHASE_FILE:...END_PHASE_FILE` format. Add a section explaining that in generate mode, multiple such blocks are expected, and include guidance on scope assignment and dependency reasoning.

### Testing

In `internal/nebula/generate_test.go`:

- Test `parseMultiPhaseOutput` with a fixture containing 3 phase blocks — verify all three are parsed correctly with proper filenames and frontmatter.
- Test `parseMultiPhaseOutput` with malformed input (missing END_PHASE_FILE) returns an error.
- Test `buildManifest` produces a valid manifest with correct name, defaults, and repo fields.
- Test `Generate` end-to-end with a mock `agent.Invoker` that returns a canned multi-phase response. Verify the result has correct phases, dependencies, and passes validation.

In `internal/nebula/architect_test.go` (if it exists, or create it):

- Test `buildArchitectPrompt` with `ArchitectModeGenerate` includes codebase context sections.
- Test that `applyDefaults` correctly applies to all generated phases.

## Files

- `internal/nebula/generate.go` — New file: `GenerateRequest`, `GenerateResult`, `Generate`, `parseMultiPhaseOutput`, `buildManifest`
- `internal/nebula/generate_test.go` — New file: tests for generation pipeline
- `internal/nebula/architect.go` — Extend: add `ArchitectModeGenerate`, update `buildArchitectPrompt` with generate mode case, extend `architectSystemPrompt` with multi-phase instructions
- `internal/nebula/architect_test.go` — Add/extend: tests for generate-mode prompt building

## Acceptance Criteria

- [ ] `ArchitectModeGenerate` constant is defined and handled in `buildArchitectPrompt`
- [ ] `parseMultiPhaseOutput` correctly extracts multiple `PHASE_FILE:...END_PHASE_FILE` blocks
- [ ] `Generate` function orchestrates analysis -> architect invocation -> parsing -> inference -> validation
- [ ] `buildManifest` produces a valid `Manifest` with name, description, and sensible execution defaults
- [ ] `architectSystemPrompt` includes multi-phase generation instructions
- [ ] Generated phases have `scope` fields populated based on the `## Files` section of each phase
- [ ] The `Generate` pipeline calls `DependencyInferrer.InferDependencies` to correct the dependency graph
- [ ] The pipeline calls `nebula.Validate` and surfaces any validation errors in `GenerateResult.Errors`
- [ ] Mock-based end-to-end test verifies the full pipeline with canned LLM output
- [ ] `go test ./internal/nebula/...` passes
- [ ] `go vet ./...` reports no issues
