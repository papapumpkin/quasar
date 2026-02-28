+++
id = "schema-evolution"
title = "Evolve SQLite schema with discoveries and beads"
type = "feature"
priority = 1
depends_on = ["fabric-rename"]
scope = ["internal/fabric/sqlite.go", "internal/fabric/sqlite_test.go", "internal/fabric/fabric.go"]
+++

## Problem

The renamed Fabric has three tables (`fabric_tasks`, `entanglements`, `claims`). The full design requires two more tables (`discoveries`, `beads`) and schema changes to the entanglements table (`consumer` column, `pending` status). The Fabric interface needs new methods for discoveries and beads.

## Solution

### Schema additions

Add to the SQLite initialization:

```sql
CREATE TABLE IF NOT EXISTS discoveries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source_task TEXT NOT NULL,
    kind        TEXT NOT NULL,
    detail      TEXT NOT NULL,
    affects     TEXT,
    resolved    BOOLEAN DEFAULT FALSE,
    created     TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS beads (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id   TEXT NOT NULL,
    content   TEXT NOT NULL,
    kind      TEXT NOT NULL,
    created   TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### Entanglements table evolution

The `entanglements` table (renamed from `contracts`) needs:
- `consumer TEXT` column — the task that depends on this entanglement, NULL means any downstream task
- `status` default changed from `'fulfilled'` to `'pending'` — entanglements start pending and become fulfilled when the producer completes

Update the CREATE TABLE to:
```sql
CREATE TABLE IF NOT EXISTS entanglements (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    producer  TEXT NOT NULL,
    consumer  TEXT,
    interface TEXT NOT NULL,
    status    TEXT NOT NULL DEFAULT 'pending',
    created   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(producer, consumer, interface)
);
```

### New types

```go
// Discovery represents an agent-surfaced issue.
type Discovery struct {
    ID         int64
    SourceTask string
    Kind       string    // one of the DiscoveryKind* constants
    Detail     string
    Affects    string    // specific task_id or empty for broadcast
    Resolved   bool
    CreatedAt  time.Time
}

const (
    DiscoveryEntanglementDispute  = "entanglement_dispute"
    DiscoveryMissingDependency    = "missing_dependency"
    DiscoveryFileConflict         = "file_conflict"
    DiscoveryRequirementsAmbiguity = "requirements_ambiguity"
    DiscoveryBudgetAlert          = "budget_alert"
)

// Bead represents agent working memory.
type Bead struct {
    ID        int64
    TaskID    string
    Content   string
    Kind      string    // note, decision, failure, reviewer_feedback
    CreatedAt time.Time
}

const (
    BeadNote             = "note"
    BeadDecision         = "decision"
    BeadFailure          = "failure"
    BeadReviewerFeedback = "reviewer_feedback"
)
```

### Fabric interface extensions

Add to the `Fabric` interface:

```go
    // Discoveries
    PostDiscovery(ctx context.Context, d Discovery) error
    Discoveries(ctx context.Context, taskID string) ([]Discovery, error)
    AllDiscoveries(ctx context.Context) ([]Discovery, error)
    ResolveDiscovery(ctx context.Context, id int64) error
    UnresolvedDiscoveries(ctx context.Context) ([]Discovery, error)

    // Beads
    AddBead(ctx context.Context, b Bead) error
    BeadsFor(ctx context.Context, taskID string) ([]Bead, error)
```

### SQLiteFabric implementation

Implement all new methods on `SQLiteFabric`. Follow the existing patterns — prepared statements, context propagation, wrapped errors.

### FabricSnapshot update

Extend `FabricSnapshot` with discovery and bead counts:
```go
type FabricSnapshot struct {
    Entanglements       []Entanglement
    Claims              map[string]string
    Completed           []string
    InProgress          []string
    UnresolvedDiscoveries []Discovery
}
```

Update `RenderSnapshot` to include unresolved discoveries in the rendered output.

## Files

- `internal/fabric/fabric.go` — Extended Fabric interface, new types (Discovery, Bead), new constants
- `internal/fabric/sqlite.go` — Schema additions, new method implementations
- `internal/fabric/sqlite_test.go` — Tests for all new CRUD operations
- `internal/fabric/contract.go` → rename to `internal/fabric/snapshot.go` — Updated FabricSnapshot and RenderSnapshot

## Acceptance Criteria

- [ ] discoveries and beads tables created on initialization
- [ ] entanglements table has consumer column and pending default status
- [ ] All 5 discovery kinds have constants
- [ ] All 4 bead kinds have constants
- [ ] Fabric interface includes discovery and bead methods
- [ ] SQLiteFabric implements all new methods
- [ ] FabricSnapshot includes unresolved discoveries
- [ ] RenderSnapshot renders discovery context for LLM consumption
- [ ] `go test ./internal/fabric/...` passes
- [ ] `go vet ./...` clean
