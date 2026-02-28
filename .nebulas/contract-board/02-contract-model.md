+++
id = "contract-model"
title = "Contract data model and extraction types"
type = "feature"
priority = 2
depends_on = ["board-store"]
scope = ["internal/board/contract.go"]
+++

## Problem

The board store needs to know what a "contract" actually is. When a phase completes, what information is useful for downstream phases? We need a clear data model that captures the interface surface produced by a phase — the types, functions, interfaces, and files it touched — in a form that can be injected into a downstream phase's context.

## Solution

Define the contract data model in `internal/board/contract.go`:

```go
// ContractKind identifies what type of code artifact a contract describes.
type ContractKind string

const (
    ContractType      ContractKind = "type"      // struct, type alias
    ContractFunction  ContractKind = "function"   // exported function
    ContractInterface ContractKind = "interface"  // interface definition
    ContractMethod    ContractKind = "method"     // method on a type
    ContractPackage   ContractKind = "package"    // package-level summary
    ContractFile      ContractKind = "file"       // file created or modified
)

// Contract represents a single interface artifact produced by a completed phase.
type Contract struct {
    Producer  string       // phase ID that produced this
    Kind      ContractKind
    Name      string       // symbol name (e.g., "Board", "NewSQLiteBoard") or filepath
    Signature string       // full definition (e.g., "type Board interface { ... }")
    Package   string       // Go package path (e.g., "internal/board")
}

// BoardSnapshot is the full board state injected into a polling phase's context.
type BoardSnapshot struct {
    Contracts  []Contract        // all fulfilled contracts
    FileClaims map[string]string // filepath -> owning phase ID
    Completed  []string          // phase IDs that are done
    InProgress []string          // phase IDs currently running
}
```

Also add a `RenderSnapshot(snap BoardSnapshot) string` function that formats the snapshot into a human-readable string suitable for injection into an LLM prompt. Format:

```
## Board State

### Completed Phases
- phase-id-1
- phase-id-2

### Available Contracts
#### internal/board (from: board-store)
- type Board interface { SetPhaseState(...) error; ... }
- func NewSQLiteBoard(dbPath string) (Board, error)

### Active File Claims
- internal/board/board.go → board-store
- internal/board/sqlite.go → board-store

### In-Progress Phases
- polling-state
```

## Files

- `internal/board/contract.go` — Contract, ContractKind, BoardSnapshot types, RenderSnapshot function

## Acceptance Criteria

- [ ] ContractKind constants cover type, function, interface, method, package, file
- [ ] Contract struct has Producer, Kind, Name, Signature, Package fields
- [ ] BoardSnapshot aggregates contracts, file claims, completed/in-progress phase IDs
- [ ] RenderSnapshot produces a readable string suitable for LLM context injection
- [ ] All types are documented with GoDoc comments
- [ ] `go vet ./internal/board/...` clean
