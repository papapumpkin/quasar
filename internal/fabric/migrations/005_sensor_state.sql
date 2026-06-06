-- 005_sensor_state.sql — durable state for the sensor scheduler.
--
-- A sensor scheduler ticks on its poll_interval, asks the sensor for events
-- since a persisted cursor, and records every observed event so a crash mid-tick
-- never re-seeds a nebula for work already turned into one. Two tables back this:
--
--   * sensor_cursors — the opaque, sensor-defined cursor per (repo, sensor),
--     advanced after each successful poll so progress survives a restart.
--   * sensor_events  — one row per observed external item. The UNIQUE constraint
--     on (repo_path, sensor_name, external_id) is the deduplication mechanism:
--     a re-observed id INSERTs as a no-op (INSERT OR IGNORE), so the scheduler
--     skips it rather than seeding a duplicate nebula.
--
-- Timestamps are unix seconds (INTEGER), matching the nebulas/phases tables.
-- Foreign-key ENFORCEMENT is a per-connection PRAGMA left off for now (see
-- 002); the REFERENCES clauses capture the relationships so enforcement can be
-- switched on later without a schema change.

CREATE TABLE sensor_cursors (
  repo_path    TEXT NOT NULL REFERENCES repos(path) ON DELETE CASCADE,
  sensor_name  TEXT NOT NULL,
  cursor       BLOB NOT NULL,            -- JSON-encoded sensor-specific cursor
  updated_at   INTEGER NOT NULL,
  PRIMARY KEY (repo_path, sensor_name)
);

CREATE TABLE sensor_events (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  repo_path    TEXT NOT NULL REFERENCES repos(path) ON DELETE CASCADE,
  sensor_name  TEXT NOT NULL,
  external_id  TEXT NOT NULL,
  received_at  INTEGER NOT NULL,
  processed_at INTEGER,                  -- NULL until processed; set when seeded
  nebula_id    TEXT REFERENCES nebulas(id) ON DELETE SET NULL,
  UNIQUE (repo_path, sensor_name, external_id)
);

CREATE INDEX sensor_events_unprocessed
  ON sensor_events (repo_path, sensor_name, processed_at)
  WHERE processed_at IS NULL;
