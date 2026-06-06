-- 003_runtime_tables.sql — constellation runtime + trigger queue tables.
--
-- The multi-repo model flows a nebula through a constellation runtime: sensors
-- seed a nebula (status='awaiting_approval'), the operator approves it, and a
-- supervisor consumes a trigger_queue row to start a constellation_run. Each
-- step of that run persists a star_invocation so surfaces (the TUI fleet view)
-- can render a step trace by polling the database, robust to runtime restarts.
--
-- These tables are introduced here because the fleet TUI reads from them and is
-- the first consumer to land. The runtime that *writes* the per-step rows lands
-- in a later phase; columns are declared so that work is additive. All creates
-- are IF NOT EXISTS so a later runtime migration can co-exist idempotently.

CREATE TABLE IF NOT EXISTS constellation_runs (
  id                 TEXT PRIMARY KEY,
  nebula_id          TEXT NOT NULL REFERENCES nebulas(id) ON DELETE CASCADE,
  constellation_name TEXT NOT NULL DEFAULT '',
  state              TEXT NOT NULL DEFAULT 'running', -- running|paused|blocked_on_review|killed|done|failed
  current_node       TEXT NOT NULL DEFAULT '',        -- e.g. "coder", "reviewer"
  step_index         INTEGER NOT NULL DEFAULT 0,
  step_count         INTEGER NOT NULL DEFAULT 0,
  log_path           TEXT,                            -- runtime log file tailed by the detail view
  created_at         INTEGER NOT NULL DEFAULT 0,
  updated_at         INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS constellation_runs_state  ON constellation_runs (state);
CREATE INDEX IF NOT EXISTS constellation_runs_nebula ON constellation_runs (nebula_id);

CREATE TABLE IF NOT EXISTS star_invocations (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id     TEXT NOT NULL REFERENCES constellation_runs(id) ON DELETE CASCADE,
  seq        INTEGER NOT NULL DEFAULT 0,
  node       TEXT NOT NULL DEFAULT '',  -- constellation node that fired this star
  star_name  TEXT NOT NULL DEFAULT '',  -- the star (skill) invoked
  state      TEXT NOT NULL DEFAULT '',  -- running|done|failed
  started_at INTEGER,
  ended_at   INTEGER
);

CREATE INDEX IF NOT EXISTS star_invocations_run ON star_invocations (run_id, seq);

CREATE TABLE IF NOT EXISTS trigger_queue (
  id                 INTEGER PRIMARY KEY AUTOINCREMENT,
  nebula_id          TEXT NOT NULL,
  constellation_name TEXT NOT NULL,
  state              TEXT NOT NULL DEFAULT 'pending', -- pending|consumed
  created_at         INTEGER NOT NULL DEFAULT 0,
  consumed_at        INTEGER
);

CREATE INDEX IF NOT EXISTS trigger_queue_state ON trigger_queue (state);
