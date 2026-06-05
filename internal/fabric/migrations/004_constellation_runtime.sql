-- 004_constellation_runtime.sql — extend the runtime tables for the engine that
-- actually executes constellations.
--
-- 003 declared constellation_runs/star_invocations/trigger_queue so the fleet
-- TUI (the first reader) could render a step trace. This migration is the
-- writer side: the runtime needs to snapshot the constellation TOML at
-- instantiation, persist its DAG walker state for crash-safe resume, track
-- parent/child runs (sub-constellations and phase_iterator), and record a
-- per-step heartbeat the supervisor reaper scans.
--
-- All adds are additive ALTER TABLE columns; the schema_migrations ledger runs
-- each migration exactly once, so non-idempotent DDL is safe here.

ALTER TABLE constellation_runs ADD COLUMN repo_path              TEXT NOT NULL DEFAULT '';
ALTER TABLE constellation_runs ADD COLUMN constellation_snapshot BLOB;
ALTER TABLE constellation_runs ADD COLUMN dag_state_toml         TEXT;
ALTER TABLE constellation_runs ADD COLUMN parent_run_id          TEXT;
ALTER TABLE constellation_runs ADD COLUMN cycle                  INTEGER NOT NULL DEFAULT 0;
ALTER TABLE constellation_runs ADD COLUMN completed_at           INTEGER;
ALTER TABLE constellation_runs ADD COLUMN heartbeat_at           INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS constellation_runs_heartbeat ON constellation_runs (heartbeat_at);
CREATE INDEX IF NOT EXISTS constellation_runs_parent    ON constellation_runs (parent_run_id);

ALTER TABLE star_invocations ADD COLUMN cycle               INTEGER;
ALTER TABLE star_invocations ADD COLUMN cost_usd            REAL;
ALTER TABLE star_invocations ADD COLUMN duration_ms         INTEGER;
ALTER TABLE star_invocations ADD COLUMN rationale_blob_hash TEXT;
ALTER TABLE star_invocations ADD COLUMN rationale_preview   TEXT;
ALTER TABLE star_invocations ADD COLUMN parsed_result_toml  TEXT;

ALTER TABLE trigger_queue ADD COLUMN repo_path TEXT NOT NULL DEFAULT '';
