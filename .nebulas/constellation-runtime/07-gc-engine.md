+++
id = "gc-engine"
title = "Garbage collector: TTL-based sweep of completed nebulas, grace window, mark-and-sweep blobs, worktree cleanup, JSONL audit log"
type = "task"
priority = 2
depends_on = ["nebula-to-sqlite-migration", "constellation-runtime"]
scope = [
    "internal/gc/**",
    "internal/blobstore/gc.go",
    "internal/fabric/migrations/**",
    "internal/config/gc.go",
    "cmd/gc.go",
]
+++

## Problem

With SQLite as canonical state (Phase 3) and the runtime persisting per-step rows (Phase 5), the database grows unboundedly without a reaper. Completed nebulas, their phases, blobs, `star_invocations`, `constellation_runs`, `sensor_events`, and `trigger_queue` rows accumulate — and orphan blobs accumulate even faster because every architect re-run or coder retry writes new content into the blobstore.

We need a garbage collector that:
- Cleans up *completed* state on a configurable TTL (per category)
- Honors a grace window so accidental deletions can be recovered
- Reclaims unreferenced blobs via mark-and-sweep, not refcount (refcounts drift)
- Cleans up stale git worktrees that the runtime never reaped
- Logs everything to a JSONL audit log so post-mortems are possible

GC is the *only* path that hard-deletes from these tables. Everything else uses soft-delete (`deleted_at IS NOT NULL`).

## Solution

### Configuration

GC TTLs live in `.quasar.yaml` (global, not per-repo — the database is global). Defaults are conservative.

```yaml
gc:
  enabled: true
  tick_interval: 1h           # how often the sweeper wakes up
  grace_window: 24h           # how long after soft-delete before hard-delete

  ttls:
    completed_nebulas: 168h   # 7d — keep PR'd nebulas around for inspection
    failed_nebulas:    720h   # 30d — keep failures longer for debugging
    constellation_runs: 168h  # 7d — runs cascade to star_invocations
    sensor_events:     720h   # 30d — keep dedup trail
    trigger_queue_consumed: 24h  # consumed triggers
    audit_log:         8760h  # 1y — JSONL log rotation, not deletion

  blobs:
    sweep_interval: 24h       # mark-and-sweep is expensive; daily is enough
    min_age_before_sweep: 1h  # don't reap blobs newer than this (in-flight refs)
```

CLI overrides for one-shot ops:
- `quasar gc run --dry-run` — print what would be deleted, take no action
- `quasar gc run --category completed_nebulas`
- `quasar gc blobs --dry-run`
- `quasar gc audit --since 24h` — tail the JSONL audit log

### Schema additions

Migration `internal/fabric/migrations/NNN_gc.sql`:

```sql
-- Add soft-delete columns to GC-able tables
ALTER TABLE nebulas             ADD COLUMN deleted_at INTEGER;
ALTER TABLE constellation_runs  ADD COLUMN deleted_at INTEGER;
ALTER TABLE sensor_events       ADD COLUMN deleted_at INTEGER;

CREATE INDEX nebulas_deleted             ON nebulas(deleted_at)             WHERE deleted_at IS NOT NULL;
CREATE INDEX constellation_runs_deleted  ON constellation_runs(deleted_at)  WHERE deleted_at IS NOT NULL;
CREATE INDEX sensor_events_deleted       ON sensor_events(deleted_at)       WHERE deleted_at IS NOT NULL;

CREATE TABLE gc_runs (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  started_at    INTEGER NOT NULL,
  completed_at  INTEGER,
  category      TEXT NOT NULL,
  swept_count   INTEGER NOT NULL DEFAULT 0,
  reclaimed_bytes INTEGER NOT NULL DEFAULT 0,
  error         TEXT
);
```

Existing FKs already cascade: deleting a nebula deletes its phases; deleting a `constellation_run` deletes its `star_invocations`. Soft-delete is what we *write*; FK cascades happen at hard-delete time only.

### Engine

`internal/gc/engine.go`:

```go
type Engine struct {
    db          *sql.DB
    config      Config
    blobs       *blobstore.Store
    worktrees   WorktreeReaper      // depends on internal/gitops
    audit       *AuditLog           // JSONL writer
    clock       func() time.Time    // injectable for tests
    logger      io.Writer
}

func New(opts Opts) (*Engine, error)

// Run starts the GC loop. Blocks until ctx is canceled.
// Sweeps run sequentially per tick — never two sweeps of the same
// category concurrently. Blob mark-and-sweep runs on its own slower schedule.
func (e *Engine) Run(ctx context.Context) error

// RunOnce does one full pass and returns. Used by `quasar gc run`.
func (e *Engine) RunOnce(ctx context.Context, opts RunOnceOpts) (*Report, error)
```

The sweep is two-phase per category:

1. **Mark phase** — `UPDATE table SET deleted_at = now WHERE deleted_at IS NULL AND <ttl-expired>` (idempotent)
2. **Sweep phase** — `DELETE FROM table WHERE deleted_at < now - grace_window` (after grace expires)

This means a nebula's lifecycle is:
- `completed` for 7 days (TTL)
- `deleted_at = T` is set (mark) — invisible to TUI by default, recoverable via `quasar nebula undelete <id>` until grace expires
- After 24 more hours (grace) the row is hard-deleted, FK cascade removes phases, blobs become unreferenced

### Blob mark-and-sweep

`internal/blobstore/gc.go`:

```go
// Sweep walks the database for blob references, marks reachable hashes,
// then deletes everything else from the blob directory.
//
// Reference sources scanned:
//   - phases.spec_blob_hash
//   - phases.review_blob_hash
//   - star_invocations.input_blob_hash
//   - star_invocations.output_blob_hash
//   - any column matching the registered reference pattern
//
// Blobs newer than minAge are skipped (in-flight writes that haven't
// committed their reference yet).
func (s *Store) Sweep(ctx context.Context, db *sql.DB, minAge time.Duration) (*SweepReport, error)
```

Reference columns are *registered* by each package that stores blob hashes — see `internal/blobstore/refs.go`:

```go
func RegisterReference(table, column string)
```

This avoids a global string-match scan. Adding a new blob-referencing column without registering it is caught by the arch test in Phase 9.

The walk is done in one transaction:
- Build a `seen` set in memory (each hash is 32 bytes; 1M blobs = 32MB — fine)
- For each registered (table, column), `SELECT DISTINCT column FROM table WHERE column IS NOT NULL`
- Walk the blob directory; for each blob file older than `minAge`, if its hash is not in `seen`, delete it

A failure mid-sweep is safe: the next sweep recomputes from scratch. No persistent state is required.

### Worktree reaper

`internal/gitops/worktree_reaper.go` (extension of existing gitops):

```go
// Reap removes worktrees under <repo>/.git/worktrees/quasar/* whose
// branch is gone, whose constellation_run is in terminal state,
// or whose mtime is older than maxAge with no live run referencing it.
func (r *WorktreeReaper) Reap(ctx context.Context, repoPath string, maxAge time.Duration) (*ReapReport, error)
```

The reaper is conservative: a worktree corresponds to a `constellation_run`; if that run is `running` or `paused`, the worktree is kept regardless of age. This prevents the GC from racing the runtime.

### JSONL audit log

`internal/gc/audit.go`. One line per GC action:

```json
{"ts":"2026-06-04T22:00:00Z","category":"completed_nebulas","action":"mark","nebula_id":"abc123","reason":"ttl_expired"}
{"ts":"2026-06-04T22:00:00Z","category":"completed_nebulas","action":"sweep","nebula_id":"abc123","cascaded_phases":4,"reclaimed_bytes":12340}
{"ts":"2026-06-04T22:05:00Z","category":"blobs","action":"sweep","hash":"deadbeef...","size_bytes":4096}
```

Location: `<quasar-data-dir>/gc-audit.log` with daily rotation. Default retention is 1 year; the file rotation cleans up old files but is not the responsibility of the GC engine — a simple `lumberjack`-style rotator handles it.

### Undelete (recovery)

`quasar nebula undelete <id>` clears `deleted_at` on a row that's still within grace window. Errors if the row was already hard-deleted. This is the only escape hatch for accidental GC; the TUI offers it as a one-keypress action on the "Recently Deleted" tab.

### Safety: never GC while runtime is busy

The engine acquires a lightweight advisory lock (`PRAGMA busy_timeout = 5000`) and skips any category whose primary table has rows in non-terminal status. For example:
- Skips `constellation_runs` sweep if any run is `running` for that repo
- Skips blob sweep entirely if any run is `running` anywhere

This means in practice the GC is "best-effort during the day, thorough at night when the fleet is idle."

### Test approach

Engine tests use `:memory:` SQLite plus a fake clock:
- TTL math: row created at T=0, TTL=1h, marked at T=1h, swept at T=1h+24h-grace
- Grace recovery: undelete clears `deleted_at` if within grace; errors after
- Cascade: hard-delete of nebula deletes its phases (FK constraint, not GC logic)

Blob sweep tests use a temp directory blobstore and a fake registered-reference scanner:
- Reachable blobs survive
- Unreferenced blobs older than `minAge` are deleted
- Unreferenced blobs newer than `minAge` survive
- Concurrent write during sweep is detected (mtime check before delete)

Worktree reaper tests use a real git repo in `t.TempDir()` and verify it never deletes a worktree whose run row is non-terminal.

## Files

- `internal/gc/engine.go` (new) — Engine struct with Run/RunOnce
- `internal/gc/engine_test.go` (new)
- `internal/gc/audit.go` (new) — JSONL audit writer
- `internal/gc/audit_test.go` (new)
- `internal/gc/categories.go` (new) — per-category sweepers (nebulas, runs, sensor_events, trigger_queue)
- `internal/gc/categories_test.go` (new)
- `internal/blobstore/gc.go` (new) — Sweep implementation
- `internal/blobstore/refs.go` (new) — RegisterReference / registry
- `internal/blobstore/gc_test.go` (new)
- `internal/gitops/worktree_reaper.go` (new)
- `internal/gitops/worktree_reaper_test.go` (new)
- `internal/config/gc.go` (new) — GCConfig struct, defaults, validation
- `internal/fabric/migrations/NNN_gc.sql` (new)
- `cmd/gc.go` (new) — `quasar gc run|blobs|audit` subcommands
- `cmd/gc_test.go` (new)
- `cmd/nebula_undelete.go` (new) — `quasar nebula undelete <id>`

## Acceptance Criteria

- [ ] `gc.Engine.Run` ticks at the configured `tick_interval` and sweeps each enabled category
- [ ] A completed nebula whose `completed_at` is older than `ttls.completed_nebulas` gets `deleted_at` set on the next tick
- [ ] A nebula with `deleted_at` older than `grace_window` is hard-deleted; its phases cascade away
- [ ] `quasar nebula undelete <id>` clears `deleted_at` if within grace; errors otherwise
- [ ] `blobstore.Store.Sweep` deletes blobs not referenced by any registered (table, column) and older than `min_age_before_sweep`
- [ ] `RegisterReference(table, column)` is required for every column storing a blob hash; arch test in Phase 9 enforces this
- [ ] `WorktreeReaper.Reap` removes only worktrees whose run row is in terminal state OR is gone entirely
- [ ] GC skips `constellation_runs` sweep for a repo if any run for that repo is `running`
- [ ] Blob sweep skips entirely if any run anywhere is `running`
- [ ] Every mark, sweep, and reap action appends a JSONL line to `gc-audit.log`
- [ ] `quasar gc run --dry-run` prints what would be deleted but takes no action
- [ ] `quasar gc audit --since 24h` tails the JSONL log filtered by time
- [ ] `gc_runs` table records each category-sweep with `swept_count` and `reclaimed_bytes`
- [ ] Tests use an injected clock; no `time.Now()` in production GC code outside the `clock` field
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all exit 0
