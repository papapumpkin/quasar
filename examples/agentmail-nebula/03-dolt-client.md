+++
id = "dolt-client"
title = "Implement Dolt client/store layer"
depends_on = ["dolt-schema", "go-module"]
+++

Implement `internal/agentmail/store.go` — a `Store` struct wrapping `*sql.DB`
with all database operations. This layer is pure SQL with no MCP awareness.

## Store struct

```go
type Store struct {
    db *sql.DB
}

func NewStore(db *sql.DB) *Store
```

## Methods to implement

Each method should use parameterized queries (no string interpolation).

### Agent management
- `RegisterAgent(ctx, name, role string) (agentID string, err error)` — INSERT into
  `agents` with a generated UUID. Return the ID.
- `Heartbeat(ctx, agentID string) error` — UPDATE `last_heartbeat` to NOW().
- `CleanupStaleAgents(ctx, timeout time.Duration) error` — release all file claims
  for agents whose `last_heartbeat` is older than `timeout` ago.

### Messages
- `SendMessage(ctx, senderID, channel, subject, body string) (int64, error)` —
  INSERT into `messages`, return the message ID.
- `ReadMessages(ctx, since *time.Time, channel *string) ([]Message, error)` —
  SELECT from `messages` with optional filters. Return newest-first.

### File claims
- `ClaimFiles(ctx, agentID string, files []string) (claimed, conflicts []string, err error)` —
  For each file: INSERT if unclaimed, else return as conflict. Use a transaction.
- `ReleaseFiles(ctx, agentID string, files []string) ([]string, error)` —
  DELETE from `file_claims` WHERE agent_id matches. Return released paths.
- `GetFileClaims(ctx, files []string) ([]FileClaim, error)` — SELECT current
  claims. If `files` is nil, return all claims.

### Changes
- `AnnounceChange(ctx, agentID, filePath, summary string) (int64, error)` —
  INSERT into `changes`, return the change ID.
- `GetChangesSince(ctx, since *time.Time, agentID *string) ([]Change, error)` —
  SELECT with optional filters. Newest-first.

## Types

Define Go structs for `Message`, `FileClaim`, and `Change` that mirror the SQL
table columns with appropriate Go types (`time.Time` for timestamps, `int64` for
IDs).

## Tests

Add `internal/agentmail/store_test.go` with tests for each method. If a live Dolt
instance isn't available in CI, use build tags or test helpers to skip gracefully.
Test the transaction behavior of `ClaimFiles` — verify that concurrent claims for
the same file result in one success and one conflict.
