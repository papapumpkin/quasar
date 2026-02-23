+++
id = "neutron-archival"
title = "Neutron archival and stale state reaper"
type = "feature"
priority = 2
depends_on = ["telemetry", "discovery-cli", "tycho-scheduler"]
scope = ["internal/neutron/**", "cmd/fabric_archive.go"]
+++

## Problem

When a nebula epoch completes, the fabric retains all its state — entanglements, claims, discoveries, pulses, task records. This state should be archived into a self-contained snapshot (a "neutron") and purged from the active fabric so it stays clean for future epochs. Additionally, stale claims and epochs need automatic cleanup to prevent coordination deadlocks.

## Solution

### Neutron type

A neutron is a standalone SQLite file containing the archived state of one epoch.

```go
// Neutron represents an archived epoch snapshot.
type Neutron struct {
    EpochID   string
    CreatedAt time.Time
    DBPath    string // path to the archived SQLite file
}
```

### Archive pipeline

```go
// Archive snapshots the current epoch's state from the fabric into a neutron.
func Archive(ctx context.Context, f fabric.Fabric, epochID string, outputPath string) (*Neutron, error)
```

The archive process:
1. Validates all claims are released (error if any remain)
2. Validates no unresolved discoveries remain (error if any, with option to force)
3. Creates a new SQLite file at `outputPath`
4. Copies into it:
   - Final entanglements as resolved — the interfaces this epoch produced
   - The full discovery log — every dispute, hail, and resolution
   - Aggregate stats — total tokens, wall-clock time, rework cycles per task, cost
   - The task graph with final states
   - All pulses from all tasks in the epoch
5. Does NOT copy: claims, intermediate scanning states, worker assignments
6. Purges the epoch's rows from the active fabric
7. Logs completion to telemetry

### Neutron schema

The neutron SQLite has its own simplified schema:

```sql
CREATE TABLE metadata (
    epoch_id    TEXT NOT NULL,
    created_at  TIMESTAMP NOT NULL,
    total_cost  REAL,
    wall_clock  INTEGER,  -- seconds
    task_count  INTEGER,
    cycle_count INTEGER
);

CREATE TABLE tasks (
    task_id     TEXT PRIMARY KEY,
    final_state TEXT NOT NULL,
    cycles_used INTEGER,
    cost_usd    REAL
);

CREATE TABLE entanglements (
    id        INTEGER PRIMARY KEY,
    producer  TEXT NOT NULL,
    consumer  TEXT,
    interface TEXT NOT NULL,
    status    TEXT NOT NULL
);

CREATE TABLE discoveries (
    id          INTEGER PRIMARY KEY,
    source_task TEXT NOT NULL,
    kind        TEXT NOT NULL,
    detail      TEXT NOT NULL,
    resolved    BOOLEAN,
    created     TIMESTAMP
);

CREATE TABLE pulses (
    id        INTEGER PRIMARY KEY,
    task_id   TEXT NOT NULL,
    content   TEXT NOT NULL,
    kind      TEXT NOT NULL,
    created   TIMESTAMP
);
```

### Purge

```go
// Purge removes all state for the given epoch from the active fabric
// without archiving. Use for abandoned epochs.
func Purge(ctx context.Context, f fabric.Fabric, epochID string) error
```

### Stale state reaper

```go
// Reap identifies and cleans up stale state.
type Reaper struct {
    Fabric     fabric.Fabric
    StaleClaim time.Duration // default 30 minutes
    StaleEpoch time.Duration // default 1 hour
}

// Run checks for stale claims and epochs, releasing/flagging as appropriate.
func (r *Reaper) Run(ctx context.Context) ([]ReapAction, error)

type ReapAction struct {
    Kind    string // "released_claim", "flagged_epoch"
    Details string
}
```

- Claims older than `StaleClaim` with no corresponding running task get released automatically
- Epochs with no state transitions in `StaleEpoch` get flagged in the cockpit (not auto-purged — human decision)

### CLI implementation

Complete the placeholder subcommands from the fabric-cli phase:

**`quasar fabric archive --epoch <id> --output <path>`**
- Runs the archive pipeline
- Default output path: `.quasar/neutrons/<epoch-id>.db`

**`quasar fabric purge --epoch <id>`**
- Runs purge (with confirmation prompt unless `--force`)

**`quasar archive`** — alias for `quasar fabric archive`

## Files

- `internal/neutron/neutron.go` — `Neutron` type, `Archive`, `Purge` functions
- `internal/neutron/reaper.go` — `Reaper` type for stale state cleanup
- `internal/neutron/neutron_test.go` — Tests for archive/purge with in-memory SQLite
- `internal/neutron/reaper_test.go` — Tests for stale detection and cleanup
- `cmd/fabric_archive.go` — Complete the archive and purge subcommand implementations

## Acceptance Criteria

- [ ] `Archive` creates a standalone neutron SQLite with correct schema and data
- [ ] Neutron contains entanglements, discoveries, pulses, tasks, metadata — but NOT claims or scanning states
- [ ] `Archive` refuses to run if claims or discoveries are unresolved (unless forced)
- [ ] `Purge` removes all epoch state from the active fabric
- [ ] `Reaper` releases claims older than threshold with no running task
- [ ] `Reaper` flags epochs with no recent transitions
- [ ] `quasar fabric archive` and `quasar archive` work end-to-end
- [ ] `go test ./internal/neutron/...` passes
- [ ] `go vet ./...` clean
