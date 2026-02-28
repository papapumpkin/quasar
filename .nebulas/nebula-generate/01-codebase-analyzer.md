+++
id = "codebase-analyzer"
title = "Build codebase analysis pipeline for nebula generation"
type = "feature"
priority = 1
depends_on = []
scope = ["internal/nebula/analyze.go", "internal/nebula/analyze_test.go"]
+++

## Problem

The `nebula generate` command needs deep codebase understanding to decompose a user prompt into meaningful phases. The existing `snapshot.Scanner` produces a condensed project snapshot (directory tree, conventions, module info), but the architect agent needs more structured context: package-level summaries, key exported symbols, dependency relationships between packages, and awareness of existing patterns.

Currently, `RunArchitect` in `internal/nebula/architect.go` receives context only from the existing nebula's manifest and phases — it has no mechanism to ingest codebase structure. Without codebase-aware context, the generated phases will be generic and disconnected from the actual code layout, producing phases that reference nonexistent files or miss critical integration points.

The analysis pipeline must bridge `snapshot.Scanner` output into a structured format that the multi-phase architect (phase 03) can embed in its prompt, enabling the LLM to reason about which packages need modification, where new files belong, and how phases should be scoped.

## Solution

Create `internal/nebula/analyze.go` containing a `CodebaseAnalysis` type and an `AnalyzeCodebase` function that wraps `snapshot.Scanner` and enriches its output with package-level metadata.

### Types

```go
// CodebaseAnalysis holds structured codebase context for the architect agent.
type CodebaseAnalysis struct {
    ProjectSnapshot string            // Raw snapshot from snapshot.Scanner
    Packages        []PackageSummary  // Top-level package summaries
    ModulePath      string            // Go module path (e.g., "github.com/papapumpkin/quasar")
}

// PackageSummary describes a single Go package and its key exports.
type PackageSummary struct {
    ImportPath   string   // Full import path
    RelativePath string   // Path relative to module root
    GoFiles      []string // .go filenames (excluding test files)
    ExportedSymbols []string // Exported type/func/var names (top-level only)
}
```

### Function

```go
// AnalyzeCodebase runs the snapshot scanner and enriches the result with
// package-level metadata. The analysis is used to provide codebase context
// to the multi-phase architect agent.
func AnalyzeCodebase(ctx context.Context, workDir string, maxSnapshotSize int) (*CodebaseAnalysis, error)
```

Implementation approach:

1. Create a `snapshot.Scanner` with `WorkDir: workDir` and `MaxSize: maxSnapshotSize`. Call `Scan(ctx)` to get the project snapshot string.
2. Walk `workDir` to discover Go packages (directories containing `.go` files). For each package, read file names and extract exported symbol names using `go/parser` in `parser.PackageClauseOnly` or `parser.ParseComments` mode — just enough to get top-level declarations without full AST analysis.
3. Detect the module path from `go.mod` (simple line scan for `module` directive).
4. Cap the total analysis output to a configurable token budget to avoid blowing up the architect prompt.

### Prompt Formatting

Add a method to `CodebaseAnalysis`:

```go
// FormatForPrompt renders the analysis as a markdown string suitable for
// embedding in an architect agent prompt.
func (a *CodebaseAnalysis) FormatForPrompt() string
```

This produces a structured markdown block with:
- Project snapshot (truncated if necessary)
- Package listing with relative paths and key exports
- Module path for import context

### Testing

Write `internal/nebula/analyze_test.go` with:
- A test using a temporary directory containing a small Go module with 2-3 packages. Verify that `AnalyzeCodebase` returns correct package summaries and module path.
- A test that verifies `FormatForPrompt` produces valid markdown with expected sections.
- A test for the snapshot size cap — create a large project tree and verify output stays within bounds.

## Files

- `internal/nebula/analyze.go` — New file: `CodebaseAnalysis`, `PackageSummary`, `AnalyzeCodebase`, `FormatForPrompt`
- `internal/nebula/analyze_test.go` — New file: table-driven tests for analysis and formatting

## Acceptance Criteria

- [ ] `AnalyzeCodebase(ctx, ".", 32000)` returns a non-nil `*CodebaseAnalysis` when run from the quasar repo root
- [ ] `PackageSummary` entries include all `internal/*` packages with their exported symbols
- [ ] `FormatForPrompt()` output is valid markdown with `## Project Snapshot`, `## Packages`, and `## Module` sections
- [ ] Total output from `FormatForPrompt()` respects the `maxSnapshotSize` budget
- [ ] Function accepts `context.Context` and propagates cancellation
- [ ] All new types and functions have GoDoc comments
- [ ] `go test ./internal/nebula/...` passes with the new test file
- [ ] `go vet ./...` reports no issues
