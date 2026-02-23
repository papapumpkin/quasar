+++
id = "fabric-rename"
title = "Rename internal/board to internal/fabric"
type = "task"
priority = 1
depends_on = []
scope = ["internal/fabric/**", "internal/nebula/worker_board.go", "internal/nebula/worker_options.go"]
+++

## Problem

The contract-board nebula built the coordination substrate under `internal/board` with types named `Board`, `Contract`, `FileClaim`, etc. The canonical vocabulary has been finalized and requires a systematic rename to align the entire codebase.

## Solution

This is a mechanical refactoring phase. Move the package and rename all types, constants, and references.

### Package move

```
internal/board/  →  internal/fabric/
```

All files move. Test files move with them.

### Type renames

| Old | New | Notes |
|-----|-----|-------|
| `Board` (interface) | `Fabric` | Central coordination interface |
| `SQLiteBoard` | `SQLiteFabric` | Implementation struct |
| `NewSQLiteBoard` | `NewSQLiteFabric` | Constructor |
| `ErrFileAlreadyClaimed` | `ErrFileAlreadyClaimed` | Keep as-is (already correct) |
| `Contract` | `Entanglement` | Interface agreement type |
| `KindType` | `KindType` | Keep (shared with entanglements) |
| `KindFunction` | `KindFunction` | Keep |
| `KindInterface` | `KindInterface` | Keep |
| `KindMethod` | `KindMethod` | Keep |
| `KindPackage` | `KindPackage` | Keep |
| `KindFile` | `KindFile` | Keep |
| `StatusFulfilled` | `StatusFulfilled` | Keep |
| `StatusDisputed` | `StatusDisputed` | Keep |
| `FileClaim` | `Claim` | File ownership record |
| `BoardSnapshot` | `FabricSnapshot` | Aggregated state |
| `RenderSnapshot` | `RenderSnapshot` | Keep name, update internals |
| `PublishContract` | `PublishEntanglement` | Fabric method |
| `PublishContracts` | `PublishEntanglements` | Fabric method |
| `ContractsFor` | `EntanglementsFor` | Fabric method |
| `AllContracts` | `AllEntanglements` | Fabric method |

### State constant renames

| Old | New |
|-----|-----|
| `StatePolling` | `StateScanning` |
| `StateHumanDecision` | `StateHumanDecision` | Keep (maps to hail concept at runtime, not in state name) |

Add new constant: `StatusPending = "pending"` for entanglements.

### Method renames on Fabric interface

```go
type Fabric interface {
    SetPhaseState(ctx context.Context, phaseID, state string) error
    GetPhaseState(ctx context.Context, phaseID string) (string, error)
    PublishEntanglement(ctx context.Context, e Entanglement) error
    PublishEntanglements(ctx context.Context, entanglements []Entanglement) error
    EntanglementsFor(ctx context.Context, phaseID string) ([]Entanglement, error)
    AllEntanglements(ctx context.Context) ([]Entanglement, error)
    ClaimFile(ctx context.Context, filepath, ownerPhaseID string) error
    ReleaseClaims(ctx context.Context, ownerPhaseID string) error
    FileOwner(ctx context.Context, filepath string) (string, error)
    ClaimsFor(ctx context.Context, phaseID string) ([]string, error)
    Close() error
}
```

### Import path updates

Every file that imports `"github.com/aaronsalm/quasar/internal/board"` must change to `"github.com/aaronsalm/quasar/internal/fabric"`. Key consumers:

- `internal/nebula/worker_board.go` → rename to `internal/nebula/worker_fabric.go`
- `internal/nebula/worker_options.go` — update option names (`WithBoard` → `WithFabric`, etc.)
- `internal/nebula/worker_board_test.go` → rename to `internal/nebula/worker_fabric_test.go`
- `cmd/nebula_apply.go` (if it references board)
- `cmd/nebula_adapters.go` (if it references board)

### SQLite table renames

In the schema initialization SQL within `SQLiteFabric`:
- `contracts` table → `entanglements` table
- Column `producer` stays, add `consumer TEXT` column (NULL = any downstream)
- Rename column references in all queries

### Poller/Publisher type updates

- `Publisher.PublishPhase` → internally creates `[]Entanglement` instead of `[]Contract`
- `LLMPoller` — update prompt text to use "entanglement" vocabulary
- `PushbackHandler` — update escalation messages

## Files

- `internal/fabric/` — all files from `internal/board/` renamed and updated
- `internal/nebula/worker_fabric.go` — renamed from `worker_board.go`
- `internal/nebula/worker_fabric_test.go` — renamed from `worker_board_test.go`
- `internal/nebula/worker_options.go` — updated option names
- Any `cmd/` files that import `internal/board`

## Acceptance Criteria

- [ ] `internal/board/` no longer exists; all code lives in `internal/fabric/`
- [ ] All type names follow the canonical vocabulary table
- [ ] All import paths updated throughout the codebase
- [ ] `StateScanning` replaces `StatePolling`
- [ ] `StatusPending` constant added for entanglements
- [ ] SQLite schema uses `entanglements` table with `consumer` column
- [ ] `go build ./...` succeeds
- [ ] `go test ./...` passes
- [ ] `go vet ./...` clean
