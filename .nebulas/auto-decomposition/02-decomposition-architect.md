+++
id = "decomposition-architect"
title = "Add decompose mode to the architect agent for splitting struggling phases"
type = "feature"
priority = 1
depends_on = ["struggle-detection"]
+++

## Problem

The existing `RunArchitect` function in `internal/nebula/architect.go` supports two modes via `ArchitectMode`: `"create"` and `"refactor"`. Neither mode handles the case of taking a struggling in-flight phase and decomposing it into 2-3 smaller, more tractable sub-phases. Decomposition requires different prompt engineering: the architect must understand what the phase *attempted*, what went wrong (struggle signals), and produce sub-phases that partition the original work with clear boundaries.

## Solution

Extend the architect subsystem with a `"decompose"` mode and a dedicated prompt that includes struggle context.

### New ArchitectMode Constant

Add to the existing constants in `internal/nebula/architect.go`:

```go
ArchitectModeDecompose ArchitectMode = "decompose"
```

### Extended ArchitectRequest

Add fields to `ArchitectRequest` to carry decomposition context:

```go
type ArchitectRequest struct {
    Mode            ArchitectMode
    UserPrompt      string
    Nebula          *Nebula
    PhaseID         string
    // Decomposition-specific fields (only used when Mode == ArchitectModeDecompose):
    StruggleReason  string           // human-readable summary from StruggleSignal.Reason
    CyclesUsed      int              // how many cycles were consumed before decomposition
    AllFindings     []loop.ReviewFinding // accumulated findings from the struggling phase
    CostSoFar       float64          // TotalCostUSD from CycleState at time of pause
}
```

Note: The `loop.ReviewFinding` import creates a dependency from `internal/nebula` to `internal/loop`. If this is undesirable, define a minimal `Finding` struct in `internal/nebula/architect.go` and convert at the call site. Prefer the simpler approach unless circular imports arise.

### Decomposition Prompt

Add a `decomposeSystemPrompt` constant and a `buildDecomposePrompt` function. The prompt must instruct the architect to:

1. Read the original phase description and acceptance criteria.
2. Analyze the struggle context: which signals triggered, what findings recurred, how much budget was spent.
3. Produce exactly 2-3 sub-phases (not 1, not 4+) that partition the original work.
4. Each sub-phase must have a unique ID prefixed with the original phase ID (e.g., `"original-id-part-1"`).
5. Sub-phases must declare `depends_on` edges among themselves if ordering matters.
6. The union of sub-phase acceptance criteria must cover the original phase's criteria.

The `buildDecomposePrompt` function builds the user prompt by combining:
- The original phase's frontmatter and body (from `Nebula.PhasesByID[req.PhaseID]`)
- The struggle reason string
- The accumulated findings (formatted as a bullet list)
- The cost and cycle context

### Decomposition Result

`RunArchitect` already returns `*ArchitectResult` with a single `PhaseSpec`. For decomposition, the architect produces multiple phases. Add a new return type:

```go
// DecomposeResult holds the output of a decomposition architect invocation.
type DecomposeResult struct {
    OriginalPhaseID string
    SubPhases       []ArchitectResult // 2-3 sub-phases
    Errors          []string
}

// RunDecompose invokes the architect in decompose mode and parses the output
// into multiple sub-phases. It validates that 2-3 phases are produced and that
// their IDs are prefixed with the original phase ID.
func RunDecompose(ctx context.Context, invoker agent.Invoker, req ArchitectRequest) (*DecomposeResult, error)
```

`RunDecompose` calls `invoker.Invoke` with the decompose agent, parses the multi-phase output (the architect outputs multiple `+++`-delimited phase blocks), validates the count (2-3), applies defaults from the manifest, and validates each sub-phase against the existing DAG.

### Parsing

Add a `parseDecomposeOutput(raw string) ([]ArchitectResult, error)` function that splits on `+++` boundaries and parses each block using the existing `parseArchitectOutput` logic.

## Files

- `internal/nebula/architect.go` — add `ArchitectModeDecompose` constant, extend `ArchitectRequest` with decomposition fields, add `decomposeSystemPrompt`
- `internal/nebula/decompose.go` — `DecomposeResult`, `RunDecompose`, `buildDecomposePrompt`, `parseDecomposeOutput`
- `internal/nebula/decompose_test.go` — table-driven tests: valid 2-phase output, valid 3-phase output, reject 1-phase output, reject 4-phase output, ID prefix validation, empty struggle context handling, prompt construction verification

## Acceptance Criteria

- [ ] `ArchitectModeDecompose` is a valid `ArchitectMode` constant
- [ ] `RunDecompose` returns an error if the architect produces fewer than 2 or more than 3 sub-phases
- [ ] Each sub-phase ID is validated to be prefixed with the original phase ID
- [ ] `buildDecomposePrompt` includes the original phase body, struggle reason, accumulated findings, and cost context
- [ ] `parseDecomposeOutput` correctly splits multi-phase `+++`-delimited architect output
- [ ] Defaults from the nebula manifest are applied to each sub-phase
- [ ] `DecomposeResult.Errors` collects validation warnings without aborting (non-fatal issues)
- [ ] No circular import between `internal/nebula` and `internal/loop` (use a local Finding type if needed)
- [ ] `go test ./internal/nebula/...` passes with at least 8 decomposition test cases
- [ ] `go vet ./internal/nebula/...` reports no issues
