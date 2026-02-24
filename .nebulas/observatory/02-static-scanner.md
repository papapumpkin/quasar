+++
id = "static-scanner"
title = "Static entanglement extraction from phase specs"
type = "feature"
priority = 1
depends_on = ["fabric-activation"]
scope = ["internal/fabric/static*.go"]
+++

## Problem

Entanglements are currently discovered only at runtime — the `Publisher` runs `git diff` after a phase completes, parses changed Go files with `go/parser`, and publishes extracted symbols to the fabric. This is reactive: a consumer phase only learns whether its producer actually created the expected types/functions *after* the producer finishes.

A terraform-style plan mode needs static, pre-execution analysis. The phase spec bodies contain structured information (## Files sections listing paths, ## Solution sections describing types/interfaces to create, `scope` fields with glob patterns) that can be parsed to predict what each phase will produce and consume.

Without static scanning, there's no way to show the entanglement contract graph before apply, detect missing producers, or identify scope conflicts upfront.

## Solution

### 1. New package: `internal/fabric/static.go`

Add a `StaticScanner` that extracts expected entanglements from phase specs without executing anything.

```go
// StaticScanner extracts expected entanglements from phase spec bodies
// by parsing structured sections (## Files, scope globs, and inline
// code references).
type StaticScanner struct {
    WorkDir string // for resolving scope globs against the filesystem
}

// PhaseContract represents the statically-derived inputs and outputs of a phase.
type PhaseContract struct {
    PhaseID   string
    Produces  []Entanglement // what this phase is expected to create
    Consumes  []Entanglement // what this phase expects to find
    Scope     []string       // resolved file paths from scope globs
}

// Scan analyzes all phases and returns their predicted contracts.
func (s *StaticScanner) Scan(phases []PhaseSpec) ([]PhaseContract, error)
```

Where `PhaseSpec` is the nebula phase spec type (ID, Body, Scope, DependsOn).

### 2. Extraction strategies

The scanner uses three extraction strategies, combined:

**a) Scope-based**: Resolve `scope` glob patterns against the filesystem. Each matched `.go` file is parsed with `go/parser` (reuse the existing `extractGoSymbols` from `publisher.go`) to discover current exported symbols. These become the phase's *potential* produces.

**b) Files-section parsing**: Parse the `## Files` markdown section from the phase body. Extract file paths (lines matching `- \`path/to/file.go\``). For Go files that already exist, parse them for exported symbols. For new files mentioned in the solution, extract type/function names from inline code blocks.

**c) Cross-reference with dependencies**: For each phase, look at what its `depends_on` phases produce. Any symbols that appear in the consumer's body text (## Solution, ## Problem) that match a producer's exports are marked as consumed entanglements.

### 3. Contract resolution

After scanning all phases, resolve the contract graph:

```go
// ResolveContracts checks all contracts for completeness.
// Returns a ContractReport with fulfilled, missing, and conflicting entries.
func ResolveContracts(contracts []PhaseContract, deps map[string][]string) *ContractReport

type ContractReport struct {
    Fulfilled []ContractEntry   // consumer expects X, producer provides X
    Missing   []ContractEntry   // consumer expects X, no producer found
    Conflicts []ContractEntry   // multiple producers for same symbol
    Warnings  []string          // ambiguous or weak matches
}

type ContractEntry struct {
    Consumer    string       // phase ID
    Producer    string       // phase ID (empty if missing)
    Entanglement Entanglement
}
```

### 4. Reuse existing infrastructure

- Reuse `publisher.go`'s `extractGoSymbols`, `FormatFuncSignature`, `FormatTypeSignature` for Go AST parsing
- Reuse `Entanglement` type and kind constants (`KindType`, `KindFunction`, `KindInterface`, etc.)
- Consider extracting the shared Go parsing functions into a `symbols.go` file if `publisher.go` starts to feel overloaded

## Files

- `internal/fabric/static.go` — `StaticScanner`, `PhaseContract`, `Scan()` implementation
- `internal/fabric/contracts.go` — `ResolveContracts`, `ContractReport`, `ContractEntry`
- `internal/fabric/static_test.go` — Tests for static scanning and contract resolution

## Acceptance Criteria

- [ ] `StaticScanner.Scan()` extracts produces/consumes from phase specs with scope globs
- [ ] `## Files` section parsing extracts file paths from markdown
- [ ] Go files matched by scope are parsed for exported symbols
- [ ] `ResolveContracts` correctly identifies fulfilled, missing, and conflicting entries
- [ ] Cross-referencing between producer outputs and consumer body text works for type/function names
- [ ] Table-driven tests cover: empty phases, phases with scope only, phases with Files section only, multi-phase contract resolution
- [ ] `go vet ./...` passes
