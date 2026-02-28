+++
id = "dependency-inference"
title = "Implement dependency graph inference for generated phases"
type = "feature"
priority = 2
depends_on = ["codebase-analyzer"]
scope = ["internal/nebula/depgraph.go", "internal/nebula/depgraph_test.go"]
+++

## Problem

When the architect agent generates multiple phases from a user prompt, it produces a flat list of phase specs. The agent may suggest `depends_on` relationships, but they are often incomplete or optimistic — the LLM does not reliably reason about transitive dependencies, scope overlaps, or ordering constraints implied by file ownership.

For example, if phase A modifies `internal/nebula/types.go` to add a new type and phase B uses that type in `internal/nebula/architect.go`, phase B must depend on phase A. The LLM might miss this if it doesn't cross-reference the `scope` globs across phases. Similarly, two phases that both touch `cmd/nebula.go` need explicit serialization unless `allow_scope_overlap` is set.

The dependency inference engine must analyze the set of generated phases and produce a corrected dependency graph that respects file ownership rules, detects implicit ordering constraints from scope overlaps, and validates the result is a DAG (no cycles). This engine runs after the architect agent produces raw phases and before the writer commits them to disk.

## Solution

Create `internal/nebula/depgraph.go` with a `DependencyInferrer` that post-processes a slice of `PhaseSpec` values and returns corrected `DependsOn` fields.

### Types

```go
// DependencyInferrer analyzes a set of phases and infers missing dependency
// edges based on scope overlap, file ownership, and ordering heuristics.
type DependencyInferrer struct {
    Phases []PhaseSpec
}

// InferenceResult contains the corrected phases and any warnings about
// dependency adjustments that were made.
type InferenceResult struct {
    Phases   []PhaseSpec // Phases with corrected DependsOn fields
    Added    []DepEdge   // Edges that were added by inference
    Warnings []string    // Human-readable warnings about the corrections
}

// DepEdge represents a single dependency edge between two phases.
type DepEdge struct {
    From string // Phase ID that depends on another
    To   string // Phase ID that is depended upon
    Reason string // Why this edge was inferred
}
```

### Core Function

```go
// InferDependencies analyzes scope overlaps, file ownership patterns, and
// explicit depends_on declarations to produce a corrected dependency graph.
// It returns an error if the resulting graph contains cycles that cannot
// be resolved.
func (d *DependencyInferrer) InferDependencies() (*InferenceResult, error)
```

Implementation approach:

1. **Scope overlap detection**: For each pair of phases, compute glob intersection of their `Scope` fields. If two phases share files and neither has `AllowScopeOverlap` set, add a dependency edge from the higher-numbered phase to the lower-numbered one (by priority, then by position).

2. **File mention extraction**: Parse the `## Files` section of each phase body to extract file paths mentioned. Cross-reference these against other phases' scopes — if phase B mentions a file owned by phase A's scope, add B depends-on A.

3. **Transitive reduction**: After adding inferred edges, compute the transitive reduction to avoid redundant dependencies (e.g., if A->B->C and A->C, remove A->C).

4. **Cycle detection**: Use the existing DAG construction from `internal/nebula/validate.go` (which uses `dag.DAG`) to verify the result is acyclic. If a cycle is detected, return an error with the cycle path.

5. **Blocks expansion**: Expand any `Blocks` fields into corresponding `DependsOn` entries on the target phases, matching the logic in `Validate`.

### Helper Functions

```go
// scopesOverlap returns true if two sets of glob patterns could match
// the same file paths.
func scopesOverlap(a, b []string) bool

// extractFileMentions parses a phase body's ## Files section and returns
// the file paths mentioned.
func extractFileMentions(body string) []string

// transitiveReduction removes redundant edges from the dependency graph.
func transitiveReduction(phases []PhaseSpec) []PhaseSpec
```

### Testing

Write `internal/nebula/depgraph_test.go` with table-driven tests:

- **No overlap**: Three phases with disjoint scopes produce no additional edges.
- **Direct overlap**: Two phases sharing `internal/nebula/*.go` scope get serialized.
- **File mention cross-reference**: Phase B mentions `internal/nebula/types.go` in its body, phase A has `scope = ["internal/nebula/types.go"]` — B depends on A.
- **Transitive reduction**: A->B->C with explicit A->C reduces to A->B->C.
- **Cycle detection**: Conflicting dependencies produce a clear error.
- **Blocks expansion**: Phase with `blocks = ["phase-b"]` causes phase-b to depend on it.
- **AllowScopeOverlap**: Phases with `allow_scope_overlap = true` do not get inferred edges from overlap.

## Files

- `internal/nebula/depgraph.go` — New file: `DependencyInferrer`, `InferenceResult`, `DepEdge`, `InferDependencies`, helper functions
- `internal/nebula/depgraph_test.go` — New file: table-driven tests for all inference scenarios

## Acceptance Criteria

- [ ] `InferDependencies` adds edges when two phases have overlapping scopes and neither has `allow_scope_overlap = true`
- [ ] `InferDependencies` adds edges when a phase body mentions files owned by another phase's scope
- [ ] Transitive reduction removes redundant edges from the output
- [ ] Cycle detection returns a descriptive error with the cycle path
- [ ] `Blocks` fields are correctly expanded into `DependsOn` entries
- [ ] `InferenceResult.Added` accurately reports all inferred edges with reasons
- [ ] `AllowScopeOverlap = true` suppresses scope-based inference for that phase
- [ ] All new types and functions have GoDoc comments
- [ ] `go test ./internal/nebula/...` passes
- [ ] `go vet ./...` reports no issues
