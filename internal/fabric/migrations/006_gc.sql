-- 006_gc.sql — soft-delete columns and the GC run ledger.
--
-- The garbage collector (internal/gc) is the only path that hard-deletes from
-- the lifecycle tables. Everything else soft-deletes by stamping deleted_at, so
-- a row remains recoverable (via `quasar nebula undelete`) until its grace
-- window expires. This migration adds the deleted_at marker to every GC-able
-- table plus partial indexes so the sweep's "WHERE deleted_at IS NOT NULL"
-- scans stay cheap as the tables grow.
--
-- gc_runs records one row per category sweep so `quasar gc` and post-mortems can
-- see what the reaper did, how much it reclaimed, and whether it errored.
--
-- All adds are additive ALTER TABLE columns; the schema_migrations ledger runs
-- each migration exactly once, so non-idempotent DDL is safe here.

ALTER TABLE nebulas            ADD COLUMN deleted_at INTEGER;
ALTER TABLE constellation_runs ADD COLUMN deleted_at INTEGER;
ALTER TABLE sensor_events      ADD COLUMN deleted_at INTEGER;

CREATE INDEX IF NOT EXISTS nebulas_deleted
  ON nebulas (deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS constellation_runs_deleted
  ON constellation_runs (deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS sensor_events_deleted
  ON sensor_events (deleted_at) WHERE deleted_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS gc_runs (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  started_at      INTEGER NOT NULL,
  completed_at    INTEGER,
  category        TEXT NOT NULL,
  swept_count     INTEGER NOT NULL DEFAULT 0,
  reclaimed_bytes INTEGER NOT NULL DEFAULT 0,
  error           TEXT
);

CREATE INDEX IF NOT EXISTS gc_runs_category ON gc_runs (category, started_at);
