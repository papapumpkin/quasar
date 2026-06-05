+++
id = "nebula-to-sqlite-migration"
title = "Nebulas move to SQLite as canonical state; blobstore for large content; cascading GC-friendly schema"
type = "task"
priority = 1
depends_on = ["multi-repo-foundation"]
scope = [
    "internal/blobstore/blobstore.go",
    "internal/blobstore/blobstore_test.go",
    "internal/fabric/sqlite.go",
    "internal/fabric/nebula_store.go",
    "internal/fabric/nebula_store_test.go",
    "internal/fabric/migrations/**",
    "internal/nebula/types.go",
    "internal/nebula/parse.go",
    "internal/nebula/state.go",
    "cmd/nebula_apply.go",
    "cmd/nebula_apply_test.go",
]
+++

## Problem

Today nebulas are filesystem-backed: `.nebulas/<id>/nebula.toml` is the manifest, `<phase>.md` files are the phases, `nebula.state.toml` is the runtime state. This worked for hand-authored single-repo Quasar but breaks down hard for the new model:
- Sensor-created nebulas would litter the filesystem with hundreds of directories
- GC of completed nebulas requires recursive directory walks with race-prone deletions
- Cross-repo fleet queries (the TUI dashboard wants to show all nebulas across all registered repos) require walking every repo's `.nebulas/` directory
- State.toml ↔ filesystem drift is a real failure mode after crashes
- Storing large LLM outputs as TOML inflates files and makes diffing useless

The right answer is SQLite as canonical storage with a content-addressed blobstore for large content. The filesystem `.nebulas/<id>/` directory pattern survives as an optional authoring surface for hand-written nebulas (imported into SQLite on first run), but is no longer the source of truth.

## Solution

### SQLite schema (additions)

New migration `internal/fabric/migrations/NNN_nebulas_to_sqlite.sql`:

```sql
CREATE TABLE nebulas (
  id            TEXT PRIMARY KEY,
  repo_path     TEXT NOT NULL REFERENCES repos(path) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  description   TEXT,

  -- source attribution (NULL for manually authored)
  source_name   TEXT,
  source_id     TEXT,
  source_url    TEXT,

  -- denormalized manifest blocks
  defaults_toml   TEXT,
  execution_toml  TEXT,
  context_toml    TEXT,

  -- lifecycle status
  status        TEXT NOT NULL DEFAULT 'draft',

  -- accumulated through lifecycle
  master_review_toml TEXT,
  pr_url             TEXT,
  pr_number          INTEGER,
  pr_opened_at       INTEGER,
  pr_merge_sha       TEXT,

  created_at    INTEGER NOT NULL,
  updated_at    INTEGER NOT NULL,
  gc_at         INTEGER
);

CREATE INDEX nebulas_repo_status ON nebulas (repo_path, status);
CREATE INDEX nebulas_gc          ON nebulas (gc_at) WHERE gc_at IS NOT NULL;
CREATE INDEX nebulas_source      ON nebulas (source_name, source_id);

CREATE TABLE phases (
  nebula_id          TEXT NOT NULL REFERENCES nebulas(id) ON DELETE CASCADE,
  id                 TEXT NOT NULL,
  seq                INTEGER NOT NULL,
  title              TEXT NOT NULL,
  body_blob_hash     TEXT NOT NULL,         -- sha256 → ~/.quasar/blobs/...
  body_preview       TEXT,                  -- first 500 chars for fleet display
  frontmatter_toml   TEXT NOT NULL,

  status             TEXT NOT NULL DEFAULT 'pending',
  started_at         INTEGER,
  completed_at       INTEGER,
  result_toml        TEXT,                  -- cost, sha, etc. (small structured)
  diff_blob_hash     TEXT,                  -- sha256 of the cycle's diff
  PRIMARY KEY (nebula_id, id)
);

CREATE INDEX phases_status ON phases (nebula_id, status);

CREATE TABLE blobs (
  hash         TEXT PRIMARY KEY,
  size_bytes   INTEGER NOT NULL,
  created_at   INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL
);
```

`constellation_runs`, `star_invocations`, and `master_reviews` tables are added in Phase 5 (where the runtime that populates them lives). This phase only adds the data tables for nebula state and the blobs registry.

### Blobstore

New package `internal/blobstore/`. Single file plus tests; small but load-bearing.

```go
// Store is a content-addressed blob store. Blobs are stored in
// ~/.quasar/blobs/<sha256[:2]>/<sha256[2:]> (git-style fanout). Content is
// zstd-compressed on write and decompressed on read. Writes are atomic via
// write-tmp-then-rename.
type Store struct {
    root string  // ~/.quasar/blobs
    db   *sql.DB // for the blobs registry table
}

func New(root string, db *sql.DB) (*Store, error)

// Put computes the SHA-256 of content, writes it to the store (if not
// already present), inserts/updates the blobs row, and returns the hash.
// Multiple Put calls with identical content are idempotent (same hash,
// same file).
func (s *Store) Put(ctx context.Context, content []byte) (string, error)

// Get returns the decompressed content for the given hash. Returns
// ErrBlobNotFound if the hash is not in the store.
func (s *Store) Get(ctx context.Context, hash string) ([]byte, error)

// Has returns whether the blob exists without reading its content.
func (s *Store) Has(ctx context.Context, hash string) bool

// Walk iterates over every blob in the registry. Used by Phase 7's GC
// mark-and-sweep.
func (s *Store) Walk(ctx context.Context) (iter.Seq[BlobInfo], error)

// Delete removes a blob from disk and from the registry. Used by Phase 7.
func (s *Store) Delete(ctx context.Context, hash string) error
```

zstd library: `github.com/klauspost/compress/zstd` (level 3, default). About 4-6× reduction on LLM-output text.

### Nebula store

`internal/fabric/nebula_store.go` is the typed Go API over the nebulas/phases tables. Replaces the filesystem-based load/save pattern.

```go
type Store struct {
    db    *sql.DB
    blobs *blobstore.Store
}

func NewNebulaStore(db *sql.DB, blobs *blobstore.Store) *Store

// Insert creates a new nebula row. Generates an ID (e.g. nebula-<timestamp>-<slug>).
func (s *Store) Insert(ctx context.Context, n NebulaRow) (id string, err error)

// Get returns a fully populated nebula including its phases. Phase bodies
// come from the blobstore.
func (s *Store) Get(ctx context.Context, id string) (*Nebula, error)

// List returns nebulas matching the filter. PhaseBodies are NOT loaded
// (use Get for that); previews are used in list views.
func (s *Store) List(ctx context.Context, filter ListFilter) ([]*NebulaSummary, error)

// SetStatus transitions a nebula's status atomically with updated_at bump.
func (s *Store) SetStatus(ctx context.Context, id string, newStatus string) error

// InsertPhase adds a phase row to an existing nebula. The body is written
// to the blobstore; the row stores the hash + a preview.
func (s *Store) InsertPhase(ctx context.Context, nebulaID string, phase PhaseRow) error

// UpdatePhaseResult records phase completion outcome.
func (s *Store) UpdatePhaseResult(ctx context.Context, nebulaID, phaseID string, result PhaseResult) error

// AppendMasterReview persists a master review cycle's outcome.
func (s *Store) AppendMasterReview(ctx context.Context, nebulaID string, review MasterReviewRow) error

// MarkForGC sets gc_at = now + grace; the GC sweep cascades the actual delete later.
func (s *Store) MarkForGC(ctx context.Context, id string, graceDur time.Duration) error
```

All writes happen inside `BEGIN IMMEDIATE` transactions. Blob writes happen before the row insert so a crash never leaves a dangling hash reference.

### Import-on-first-run

When the supervisor (or any CLI invocation) starts:
1. Check if SQLite has any rows in `nebulas`
2. If not, walk every registered repo's `.nebulas/<id>/` directory
3. For each found directory: parse manifest + phase files + state.toml; INSERT into SQLite
4. The on-disk files are NOT deleted (preserve user content), but Quasar no longer reads from them after this — they become snapshots
5. Log a one-time message: `imported N nebulas from filesystem into SQLite. Future writes are SQLite-canonical.`

This means user-authored nebula directories continue to work — they just route through SQLite after the first import. The `quasar nebula apply <path>` command also routes new submissions through the importer.

### Manual authoring via `quasar nebula apply <path>`

The existing command keeps its semantics:
1. Parse `<path>/nebula.toml`
2. Parse all `<path>/*.md` phase files
3. Call `NebulaStore.Insert(ctx, ...)` with `repo_path = currentRepoPath`
4. Call `NebulaStore.InsertPhase(ctx, ...)` for each phase
5. Return the new nebula ID

The on-disk files are not touched after import. If a user changes them and re-runs `apply`, a new nebula row is created (with `-2` ID suffix). The user's filesystem stays a source of authoring; SQLite is the source of execution.

### Updates to existing nebula package

`internal/nebula/types.go` and friends had Go structs that mirrored the filesystem shape (manifest, phases as in-memory slices). These structs stay — they're still useful for the parse / authoring side — but the runtime loads them from the SQLite-backed store, not from disk. The architect, the loop, and the worker all switch from "read from disk" to "read from `NebulaStore.Get(ctx, id)`."

State.toml writes are removed. All state lives in SQLite.

### What goes away

- `internal/nebula/state.go`'s `SaveState`, `LoadState` functions (the filesystem state.toml read/write)
- The `nebula.state.toml` file format. Pre-existing files on disk are read once during import and then ignored.

### What stays

- The `Nebula` Go struct shape (still used for in-memory representation; just loaded from SQLite now)
- `internal/nebula/parse.go` (still parses TOML manifests from disk for import-on-first-run and `quasar nebula apply`)
- The on-disk filesystem format remains valid for hand-authored input. It just isn't read at execution time.

## Files

- `internal/blobstore/blobstore.go` (new) — Store with Put/Get/Has/Walk/Delete
- `internal/blobstore/blobstore_test.go` (new) — round-trip, dedup, atomicity, walk
- `internal/fabric/sqlite.go` — wire the new migration
- `internal/fabric/migrations/NNN_nebulas_to_sqlite.sql` (new) — nebulas + phases + blobs tables
- `internal/fabric/nebula_store.go` (new) — typed Go API over the tables; transactional writes
- `internal/fabric/nebula_store_test.go` (new) — table tests with t.TempDir() SQLite + blobstore
- `internal/nebula/types.go` — no schema change to the in-memory struct; SourceName/SourceID become populated from DB rather than parsed from file
- `internal/nebula/parse.go` — `Load(dir)` still exists for import; gains a sibling helper that converts the parsed in-memory Nebula into NebulaStore.Insert calls
- `internal/nebula/state.go` — delete SaveState/LoadState; the file is now empty or deleted entirely
- `cmd/nebula_apply.go` — switches from `LoadState`/`SaveState` to `NebulaStore.Insert` (manual import path)
- `cmd/nebula_apply_test.go` — assertions updated

## Acceptance Criteria

- [ ] `internal/blobstore/` compiles and tests pass
- [ ] `Store.Put` computes SHA-256, writes file at `<root>/<sha[:2]>/<sha[2:]>`, inserts row in `blobs` table
- [ ] `Store.Put` is idempotent: two calls with the same content return the same hash and create one file, one row
- [ ] `Store.Get(hash)` returns the original content (after zstd decompression)
- [ ] `Store.Walk` iterates over all rows in the `blobs` table
- [ ] `nebulas` table has `repo_path` foreign key to `repos(path)` with `ON DELETE CASCADE`
- [ ] `phases.body_blob_hash` references a blob; `phases.body_preview` is populated with the first ~500 chars at insert time
- [ ] `NebulaStore.Insert(ctx, ...)` creates a nebulas row with status `draft` by default
- [ ] `NebulaStore.Get(ctx, id)` returns the nebula including phase bodies (loaded from blobs)
- [ ] `NebulaStore.List(ctx, filter)` returns summaries without loading phase bodies (uses preview)
- [ ] Import-on-first-run: when SQLite has zero nebulas rows and registered repos have `.nebulas/<id>/` directories, those directories are imported on startup; subsequent reads come from SQLite
- [ ] Imported nebula's `.nebulas/<id>/` directory remains on disk untouched
- [ ] `quasar nebula apply <path>` still parses TOML+Markdown and inserts into SQLite; assigns repo_path = the current registered repo (errors if no repo is registered for the current CWD)
- [ ] State.toml files are no longer written. Existing state.toml files on disk are read once during import and then ignored.
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all exit 0
