# Fabric — the SQLite persistence layer

**Fabric** (`internal/fabric/`) is Quasar's canonical store: a single SQLite
database (WAL mode, one connection) through which every long-lived piece of state
crosses. Nothing important lives only in memory — a nebula, a constellation run's
DAG state, a sensor cursor, a budget, a checkpoint, and a symbol claim are all
rows here, which is what makes crash-safe resume and the DB-only TUI possible.

The base schema is created on first open (`internal/fabric/sqlite.go:19-77`,
applied in `NewSQLiteFabric`, `sqlite.go:86-124`); ordered migrations layer on top
(`internal/fabric/migrate.go`). For who *reads* this state, see
[runtime.md](runtime.md) (the engine) and [architecture.md](architecture.md). The
TUI's read-only contract is in [§7](#7-reading-from-outside-the-runtime).
The `entanglements` table and its lifecycle are documented in
[entanglements.md](entanglements.md); the `repos` / `trigger_queue` tables and
per-repo routing in [multi-repo.md](multi-repo.md); the
`conflict_resolutions.jsonl` telemetry in
[conflict-resolution.md](conflict-resolution.md).

---

## 1. What fabric is

`SQLiteFabric` (`sqlite.go:79-124`) opens the DB with `SetMaxOpenConns(1)`
(SQLite has a single writer; one connection avoids `SQLITE_BUSY` between pooled
connections), `PRAGMA journal_mode=WAL` (readers never block writers), and
`PRAGMA busy_timeout=5000`. Foreign-key **enforcement** is left off
(`sqlite.go` runs no `PRAGMA foreign_keys=ON`); `REFERENCES … ON DELETE CASCADE`
clauses are declared to document ownership but cascades are performed manually by
the GC (see [§4](#4-cascading-deletes)).

Sibling stores (the run store, nebula store, etc.) operate on the same handle,
exposed via `SQLiteFabric.DB()` (`sqlite.go:129-131`).

---

## 2. Migrations

`runMigrations` (`migrate.go:24-51`) applies every embedded `migrations/*.sql`
file not yet recorded in the `schema_migrations` ledger, in lexical filename
order, each inside its own transaction, exactly once (`applyMigration`,
`migrate.go:83-105`). Because the ledger prevents re-runs, migrations may contain
non-idempotent DDL (`ALTER TABLE`). What each introduces:

| Migration | Introduces |
|---|---|
| `001_repos.sql` | `repos` registry; `nebulas.repo_path` |
| `002_nebulas_to_sqlite.sql` | rebuilds `nebulas` (manifest blocks, lifecycle, FK to `repos`); adds `phases`, `blobs` |
| `003_runtime_tables.sql` | `constellation_runs`, `star_invocations`, `trigger_queue` |
| `004_constellation_runtime.sql` | run-engine columns: `dag_state_toml`, `constellation_snapshot`, `parent_run_id`, `cycle`, `heartbeat_at`; invocation `cost_usd`, `cycle`, `rationale_blob_hash` |
| `005_sensor_state.sql` | `sensor_cursors`, `sensor_events` |
| `006_gc.sql` | `deleted_at` soft-delete columns; `gc_runs` ledger |
| `007_budget.sql` | `constellation_runs` budget columns |
| `008_checkpoints.sql` | `checkpoints`, `checkpoint_files` |
| `009_entanglement_lifecycle.sql` | entanglement lifecycle columns + states |
| `010_schema_prune.sql` | drops two dead indexes the 2026-06-08 audit found |

The base schema (`sqlite.go:19-77`) predates the ledger and creates the legacy
tables (`fabric`, `entanglements`, `file_claims`, `discoveries`, `pulses`) plus
the original thin `nebulas` that 002 rebuilds.

---

## 3. Tables

For each table: purpose, the migration that introduced it, and at least one
writer and reader site.

### repos
Registered git repo paths the fleet may operate on. *Migration 001.*
Writer: `Registry.Register` (`internal/repos/registry.go:39`),
`SetStatus` (`registry.go:160`). Reader: `Registry.List` (`registry.go:116`),
`Get` (`registry.go:146`).

### nebulas
The canonical nebula store — the universal artifact that flows through the whole
lifecycle (replacing on-disk `.nebulas/<id>/` for migrated repos). *Migration 002
(over the base table).* Writer: `NebulaStore.Insert` (`nebula_store.go:65`),
`SetStatus` (`nebula_store.go:232`), `AppendMasterReview` (`nebula_store.go:299`).
Reader: `Get` (`nebula_store.go:109`), `List` (`nebula_store.go:183`, the fleet
list view).

### phases
One row per phase; the body content lives in a blob (`body_blob_hash`).
*Migration 002.* Writer: `InsertPhase` (`nebula_store.go:89`), `UpdatePhaseResult`
(`nebula_store.go:273`). Reader: `getPhases` (`nebula_store.go:149`, called by
`Get`).

### blobs
Content-addressed registry (sha256) for large/binary content; the blob bytes are
zstd-compressed on disk under the blob store root. *Migration 002.* Writer:
`blobstore.Store.Put` (`internal/blobstore/blobstore.go:79`, hash + compress +
`upsertRow`), reached e.g. via `InsertPhase` (`nebula_store.go:89-90`). Reader:
`Store.Get` (`blobstore.go:153`); `Store.Walk` (`blobstore.go:187`) for GC.

### sensor_cursors
The opaque per-`(repo, sensor)` poll cursor, advanced after each successful poll.
*Migration 005.* Writer: `SensorCursorStore.Set` (`sensor_store.go:46`). Reader:
`Get` (`sensor_store.go:27`).

### sensor_events
One row per observed external item; the `UNIQUE(repo, sensor, external_id)`
constraint is the dedup mechanism (`INSERT OR IGNORE`). *Migration 005.* Writer:
`SensorEventStore.Insert` (`sensor_store.go:91`), `MarkProcessed`
(`sensor_store.go:123`). Reader: `Unprocessed` (`sensor_store.go:159`),
`UnprocessedExternalIDs` (`sensor_store.go:144`).

### constellation_runs
One row per Fire, carrying `current_node` + `dag_state_toml` for crash-safe
resume, plus budget and `heartbeat_at`. *Migration 003 (+004, 006, 007).* Writer:
`ConstellationRunStore.InsertRun` (`constellation_store.go:72`), `SaveProgress`
(`constellation_store.go:138`), `Complete` (`constellation_store.go:155`),
`Heartbeat` (`constellation_store.go:166`). Reader: `GetRun`
(`constellation_store.go:110`, by `Step`); the fleet view reads it directly
(`internal/tui/fleet/fleet.go:198`, `:292`).

### star_invocations
One row per Step that ran a star, with `cost_usd` for the budget. *Migration 003
(+004).* Writer: `RecordInvocationCost` (`constellation_store.go:286`, success +
budget decrement, atomic) and `InsertStarInvocation` (`constellation_store.go:248`,
failure trace). Reader: the fleet step trace `Store.Trace`
(`fleet.go:379-382`); `CostBreakdown` (`constellation_store.go:324`).

### trigger_queue
Pending → consumed work items; drained by the supervisor. *Migration 003 (+004).*
Writer: enqueue at `Store.Approve` (`fleet.go:330`, `INSERT` at `fleet.go:345`);
claim (consume) at `Supervisor.claim` (`internal/constellations/supervisor.go:172`).
Reader: `Supervisor.selectPending` (`supervisor.go:142`).

### entanglements
Cross-phase symbol-claim coordination with a six-state lifecycle. The states are
defined in `internal/fabric/fabric.go:53-57`: `declared`, `claimed`, `in_flight`,
`withdrawn`, `deprecated` (plus `fulfilled`, `fabric.go:48`); `activeStatuses`
(`fabric.go:66`) is the in-flight-intent subset. *Base schema + migration 009
(lifecycle columns).* Writer: `EntanglementStore.Declare`
(`entanglement_store.go:35`), `MarkInFlight` (`entanglement_store.go:93`),
`Fulfill`/`Withdraw`/`Deprecate` (`entanglement_store.go:138`/`:155`/`:117`).
Reader: `Active` (`entanglement_store.go:173`), `ActiveAll`
(`entanglement_store.go:207`, the coordination pre-flight).

### checkpoints / checkpoint_files
In-cycle worktree snapshots: a checkpoint records a content-addressed manifest
blob; `checkpoint_files` lists each file's blob hash so the GC counts every file
blob as a first-class reference (`008_checkpoints.sql:9-14`). *Migration 008.*
Writer: `CheckpointStore.Insert` (`checkpoint_store.go:52`, parent + child rows
atomically). Reader: `Latest` (`checkpoint_store.go:98`), `ListForRun`
(`checkpoint_store.go:124`).

### gc_runs
One ledger row per category sweep (swept count, reclaimed bytes, error).
*Migration 006.* Writer: `Engine.recordRun` (`internal/gc/engine.go:287`). **No
production Go reader** — it is a write-only post-mortem ledger read via ad-hoc SQL
and the GC tests (`internal/gc/engine_test.go:185`).

### Legacy / partial tables
These predate the constellation model and have **no live runtime caller** outside
tests and the neutron archival path. *Base schema, `sqlite.go`.* Treat them as
historical; see [audit-2026-06-08.md](audit-2026-06-08.md).

- **`fabric`** — old per-phase state map. Writer `SetPhaseState` (`sqlite.go:134`),
  reader `GetPhaseState` (`sqlite.go:147`); not on the supervisor/runtime path.
- **`file_claims`** — old phase file ownership (`sqlite.go:301`/`:351`); the
  audit confirmed it dead in production code.
- **`discoveries`** — agent-surfaced issues (`sqlite.go:413`/`:432`); telemetry
  only.
- **`pulses`** — structured context emissions (`sqlite.go:502`/`:525`); never
  populated by active code.

---

## 4. Cascading deletes

Because foreign-key enforcement is off, `ON DELETE CASCADE` does not fire
automatically — the GC performs cascades manually so a delete never orphans
children or strands blob references. In `internal/gc/categories.go`:

- **`deleteWithChildren`** (`categories.go:264`) deletes a nebula and its `phases`
  in one transaction (`sweepNebulas`, `categories.go:47`).
- **`deleteRunWithChildren`** (`categories.go:292`) deletes a run *and* its
  `star_invocations`, `checkpoints`, and `checkpoint_files` (grandchildren first),
  so every `*_blob_hash` reference disappears before the next blob sweep runs
  (`sweepRuns`, `categories.go:108`). This explicit order is why GC can safely
  reclaim a single run's blobs without silent data loss (`008_checkpoints.sql:16-22`).

So a nebula delete cascades to its phases, its constellation_runs, their
star_invocations, and their checkpoints — letting GC reap one nebula row's entire
subtree.

---

## 5. Blob lifecycle

A blob is written on `Put` (`blobstore.go:79`): the content is sha256-hashed,
zstd-compressed, written atomically to `<root>/<aa>/<hash>`, and registered in
`blobs`. Reclamation is mark-and-sweep: `Store.Sweep`
(`internal/blobstore/gc.go:43`) builds the live set by scanning **every registered
reference column** (`liveSet`, `gc.go:90`), then deletes any registry blob that is
both unreferenced *and* older than `minAge`.

The reference-column registry is populated from package `init()` functions via
`blobstore.RegisterReference` (`internal/blobstore/reference.go:28`); `References()`
(`reference.go:39`) returns the full list. The fabric package registers all five
blob columns in `internal/fabric/blobrefs.go:11-20`: `phases.body_blob_hash`,
`phases.diff_blob_hash`, `star_invocations.rationale_blob_hash`,
`checkpoints.manifest_blob_hash`, `checkpoint_files.file_blob_hash`. A
`TestBlobHashColumnsRegistered` fails CI if a new `*_blob_hash` column is added
without a matching registration — without it, the sweep would reclaim a
still-referenced blob (silent loss).

---

## 6. State.TOML serialization

The `dag_state_toml` column holds a serialized `constellations.State`
(`internal/constellations/state.go:28-42`), produced by `MarshalState`
(`state.go:203-209`) and restored by `UnmarshalState` (`state.go:212-224`). Its
shape:

```toml
cycle = 1

[inputs]            # constellation-level inputs (usually empty)

[nodes]             # each completed node's outputs, keyed by node ID
  [nodes.decide]
    verdict  = "approved"
    approved = true

[nebula]            # denormalized snapshot taken at Fire
  id     = "neb-123"
  name   = "fix-truncate"
  status = "running"
  # phases = [...], current_phase = {...} when set by phase_iterator

[meta]
  total_cost_usd = 0.42
  run_started_at = 1749430000
  max_cycles     = 3
```

Storing the full evaluation context in the row is what makes resume a pure
read: see [runtime.md §8](runtime.md#8-state-persistence).

---

## 7. Reading from outside the runtime

The fleet TUI reads run/nebula state **only** through fabric stores — it never
imports the runtime, the GC engine, or a concrete sensor adapter. This is
enforced by `TestTUIIsDBOnly` (`internal/arch_test/boundaries_test.go:37-54`),
which fails the build if anything under `internal/tui/` imports
`internal/runtime`, `internal/gc`, or `internal/sensors/github`. The rationale
(`boundaries_test.go:32-36`): a TUI that imports the runtime grows side-channel
knobs that bypass the constellation engine; one that imports a sensor couples the
dashboard to a specific forge. **The DB is the contract** — every surface reads
the same rows the engine writes, which is what keeps this persistence layer
canonical.
