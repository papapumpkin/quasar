-- 008_checkpoints.sql — in-cycle worktree checkpoints.
--
-- A checkpoint snapshots the coder's worktree after a green build so a coder
-- killed mid-cycle (healthcheck termination, crash) forfeits at most the work
-- since the last known-good build instead of the whole cycle. Each checkpoint
-- records the command that triggered it and a content-addressed manifest blob
-- (canonical JSON {path: blob_hash}) describing every file in the tree.
--
-- checkpoint_files lists the per-file blob hash for every file in the snapshot
-- so the mark-and-sweep GC counts each referenced file blob as live. A file blob
-- referenced only from inside the manifest JSON would be invisible to the GC's
-- column scan (it walks *_blob_hash columns, not blob contents) and reclaimed
-- while still referenced — silent data loss. The child table makes every file
-- blob a first-class column reference.
--
-- Reclamation: the fabric runs with PRAGMA foreign_keys OFF, so the declared
-- ON DELETE CASCADE does NOT fire automatically. The GC's sweepRuns therefore
-- deletes checkpoint_files and checkpoints explicitly when it reaps a run (see
-- internal/gc/categories.go); once those rows are gone the manifest and file
-- blob references disappear and the next blob sweep reclaims the now-
-- unreferenced blobs. The FK declarations document ownership and would make the
-- cascade automatic the moment enforcement is turned on.
--
-- All creates are IF NOT EXISTS so a later migration can co-exist idempotently;
-- the schema_migrations ledger runs this file exactly once regardless.

CREATE TABLE IF NOT EXISTS checkpoints (
  id                 INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id             TEXT NOT NULL REFERENCES constellation_runs(id) ON DELETE CASCADE,
  cycle              INTEGER NOT NULL,
  trigger_cmd        TEXT NOT NULL,             -- the build-class command that fired this checkpoint
  manifest_blob_hash TEXT NOT NULL,             -- blob hash of canonical JSON {path: blob_hash}
  created_at         INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS checkpoints_run ON checkpoints (run_id, cycle);

CREATE TABLE IF NOT EXISTS checkpoint_files (
  checkpoint_id  INTEGER NOT NULL REFERENCES checkpoints(id) ON DELETE CASCADE,
  path           TEXT NOT NULL,
  file_blob_hash TEXT NOT NULL,                  -- blob hash of this file's exact bytes
  mode           INTEGER NOT NULL DEFAULT 420    -- unix permission bits (420 = 0o644)
);

CREATE INDEX IF NOT EXISTS checkpoint_files_cp ON checkpoint_files (checkpoint_id);
