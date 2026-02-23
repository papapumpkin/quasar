+++
id = "board-store"
title = "SQLite WAL board store with schema and Go interface"
type = "feature"
priority = 1
scope = ["internal/board/**"]
+++

## Problem

Quasar's worker dispatch loop currently has no shared state between phases. When a phase completes, its outputs (exported types, function signatures, interfaces) are not captured anywhere for dependent phases to inspect. The only coordination signal is "done" — dependents start blindly with no context about what was produced upstream.

File-based state (TOML/JSON) doesn't scale for concurrent access: parsing entire files on every read, write contention with file-level locks (as experienced with Dolt in Beads). The board needs sub-millisecond reads, concurrent reader/writer support, and indexed queries.

## Solution

Create `internal/board/` with a SQLite WAL-mode database backing three tables:

### Schema

```sql
CREATE TABLE board (
    task_id    TEXT PRIMARY KEY,
    state      TEXT NOT NULL,     -- queued, polling, running, blocked, done, failed
    worker     TEXT NOT NULL DEFAULT '',
    posted_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE contracts (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    producer   TEXT NOT NULL,      -- phase ID that produced this
    kind       TEXT NOT NULL,      -- type, function, interface, package, file
    name       TEXT NOT NULL,      -- symbol name or file path
    signature  TEXT NOT NULL,      -- full signature / type definition
    package    TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'fulfilled',  -- fulfilled, disputed
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(producer, kind, name)
);

CREATE TABLE file_claims (
    filepath   TEXT PRIMARY KEY,
    owner_task TEXT NOT NULL,
    claimed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### Go Interface

```go
// Board is the shared state store for contract-based execution.
type Board interface {
    // Phase state
    SetPhaseState(ctx context.Context, phaseID, state string) error
    GetPhaseState(ctx context.Context, phaseID string) (string, error)

    // Contracts
    PublishContract(ctx context.Context, c Contract) error
    PublishContracts(ctx context.Context, contracts []Contract) error
    ContractsFor(ctx context.Context, phaseID string) ([]Contract, error)
    AllContracts(ctx context.Context) ([]Contract, error)

    // File claims
    ClaimFile(ctx context.Context, filepath, ownerPhaseID string) error
    ReleaseClaims(ctx context.Context, ownerPhaseID string) error
    FileOwner(ctx context.Context, filepath string) (string, error)
    ClaimsFor(ctx context.Context, phaseID string) ([]string, error)

    // Lifecycle
    Close() error
}
```

Open the database with `PRAGMA journal_mode=WAL` and `PRAGMA busy_timeout=5000` immediately after connection. Use `database/sql` with `modernc.org/sqlite` driver. The `.db` file lives at `<nebula-dir>/<name>.board.db`.

Constructor: `NewSQLiteBoard(dbPath string) (Board, error)` — creates tables if not exist, enables WAL, returns the Board.

## Files

- `internal/board/board.go` — Board interface, Contract type, state constants
- `internal/board/sqlite.go` — SQLite implementation of Board
- `internal/board/sqlite_test.go` — Table-driven tests for all Board methods

## Acceptance Criteria

- [ ] SQLite database opens in WAL mode with busy timeout
- [ ] All three tables created on first open (idempotent)
- [ ] Board interface fully implemented with SQLite backend
- [ ] ClaimFile fails (returns error) if file already claimed by different phase
- [ ] PublishContract is idempotent (upsert on unique constraint)
- [ ] Concurrent reads and writes do not deadlock (test with goroutines)
- [ ] `go test ./internal/board/...` passes
- [ ] `go vet ./internal/board/...` clean
