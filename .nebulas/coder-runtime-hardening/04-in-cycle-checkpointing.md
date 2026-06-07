+++
id = "in-cycle-checkpointing"
title = "Snapshot the worktree after every green build inside a cycle — subprocess death loses at most one build cycle of work"
type = "task"
priority = 2
depends_on = ["dead-coder-detection"]
scope = [
    "internal/runtime/checkpoint.go",
    "internal/runtime/checkpoint_test.go",
    "internal/runtime/engine.go",
]
+++

## Problem

A coder that's been working productively for 20 minutes — three successful builds, two passing test runs — gets killed by the healthcheck at minute 25 because of one stuck test. All 20 minutes of working code is recoverable from the partial worktree but there's no machine-readable record of "this is the state after build #2 passed cleanly."

Without checkpoints, the reviewer has to judge a snapshot that may include broken in-flight work from the moment of death. With checkpoints, the reviewer can choose the latest known-good state and only forfeit work since that checkpoint.

This is a small phase: detect "build just passed", snapshot, and expose the snapshots to the reviewer.

## Solution

### Checkpoint trigger

A checkpoint fires when the coder runs a "build-class" tool (configurable per-star, defaults to `go build ./...`, `go vet ./...`, `go test -short ./...`) and the result is exit 0.

`internal/runtime/checkpoint.go`:

```go
// Checkpointer snapshots the worktree state on green-build signals.
// Snapshots are content-addressed via the blobstore so multiple coders
// (different cycles, different phases) can share unchanged files.
type Checkpointer struct {
    workdir   string
    blobs     *blobstore.Store
    store     *CheckpointStore
    triggers  []string  // commands that, when exit-0, trigger a snapshot
}

func New(workdir string, blobs *blobstore.Store, store *CheckpointStore, triggers []string) *Checkpointer

// MaybeCheckpoint is called by the runtime after every tool result.
// If the tool was a build-class trigger and exit was 0, it snapshots.
// Snapshots are deduped against the previous one — if no files changed,
// no new row is written.
func (c *Checkpointer) MaybeCheckpoint(ctx context.Context, result ToolResult) (*Checkpoint, error)
```

### Schema

New migration `006_checkpoints.sql`:

```sql
CREATE TABLE checkpoints (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id        TEXT NOT NULL REFERENCES constellation_runs(id) ON DELETE CASCADE,
  cycle         INTEGER NOT NULL,
  trigger       TEXT NOT NULL,             -- the command that fired this
  manifest_blob_hash TEXT NOT NULL,        -- JSON {path: blob_hash} of all changed files
  created_at    INTEGER NOT NULL
);
CREATE INDEX checkpoints_run ON checkpoints(run_id, cycle);
```

The `manifest_blob_hash` blob is `{"internal/foo.go": "sha256...", "internal/bar.go": "sha256...", ...}`. Restoring a checkpoint = walking the manifest, fetching each file's blob, writing to disk.

### Restore on dead-coder

When the supervisor catches `DeadCoderError` (Phase 03), it walks the run's checkpoints, picks the latest, and exposes both states to the reviewer:

- `partial/` — the worktree at moment of death
- `checkpoint/` — the latest green-build snapshot

The reviewer judges three outcomes:
- **ship the checkpoint** — work since the checkpoint is not worth keeping; reviewer approves the checkpoint state
- **ship the partial** — work since the checkpoint is good; reviewer approves the partial state
- **retry from checkpoint** — queue a new coder starting from the checkpoint, with the partial state as `## Prior context` in its prompt

### Cost

A typical Go file is 5–15 KB; a typical phase touches 10–20 files. Each checkpoint is ~50–100 KB of blob writes, deduped against unchanged files. A 25-minute cycle with 5 green builds writes maybe 250 KB of new blob data total. Negligible.

The blobstore's mark-and-sweep GC (constellation-runtime Phase 7) cleans up checkpoint blobs that are no longer referenced once their parent run completes.

### Configurability

Per-star override of triggers in TOML:

```toml
# stars/coder.md frontmatter (excerpt)
[checkpoint]
triggers = ["go build ./...", "go vet ./...", "go test -short ./..."]
enabled = true
```

Defaults are Go-centric since this repo is Go. A Python repo would override to `["python -m pytest --collect-only", "ruff check ."]`.

### Tests

- Snapshot creates blobs for each file in the workdir; manifest blob hash is deterministic
- Two snapshots back-to-back with no file changes → second snapshot reuses all blobs, only writes a new `checkpoints` row referencing the same `manifest_blob_hash`
- Restore walks a snapshot and reproduces the byte-exact original workdir
- Dead-coder fixture: kill coder after 2 checkpoints; assert supervisor finds both, picks the later one

## Files

- `internal/runtime/checkpoint.go` (new)
- `internal/runtime/checkpoint_test.go` (new)
- `internal/fabric/checkpoint_store.go` (new) — DB-backed store for the `checkpoints` table
- `internal/fabric/checkpoint_store_test.go` (new)
- `internal/fabric/migrations/006_checkpoints.sql` (new)
- `internal/runtime/engine.go` (modify) — wire Checkpointer into the per-tool-call path
- `internal/runtime/supervisor.go` (modify) — on DeadCoderError, load latest checkpoint + present both states to reviewer

## Acceptance Criteria

- [ ] `Checkpointer.MaybeCheckpoint` writes a checkpoint when given an exit-0 result for a trigger command
- [ ] Same call with exit-1 result writes NO checkpoint
- [ ] Two back-to-back snapshots with zero file changes share the same `manifest_blob_hash`
- [ ] Restore from a checkpoint writes byte-identical file content (verified by content hash)
- [ ] Migration `006_checkpoints.sql` creates the table with the correct FK to `constellation_runs(id)` and ON DELETE CASCADE
- [ ] Supervisor's DeadCoderError handler loads the latest checkpoint and exposes `partial/` + `checkpoint/` to the reviewer
- [ ] Default triggers are Go-centric (`go build`, `go vet`, `go test -short`)
- [ ] Per-star TOML override of triggers takes effect
- [ ] Blob GC sweep (constellation-runtime Phase 7) reclaims checkpoint blobs once the parent run is GC'd
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
