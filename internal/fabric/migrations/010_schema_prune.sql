-- 010_schema_prune.sql — drop dead indexes the 2026-06-08 audit found.
--
-- The two indexes below covered query patterns that never appear in Go code:
--
--   * constellation_runs_budget_low — created in 007 to support a TUI
--     "running low on budget" alert that was never implemented. No Go code
--     issues a query matching its WHERE clause (state='running' AND
--     budget_usd_remaining < 1.0).
--
--   * constellation_runs_parent — created in 004 to support filtering by
--     parent_run_id. Nested constellation dispatch (NodeConstellation in
--     dispatch_constellation.go) reads the column but no query in production
--     code filters by it; runs are joined via run_id PK lookups instead.
--
-- Both are pure cost: they consume write amplification on every constellation
-- run update without serving any query. Dropping them is safe — re-add the
-- supporting query first if you need the alert / the parent-filter lookup.
--
-- The two known-dead columns the audit also flagged (constellation_snapshot
-- and budget_exhausted_at) are NOT dropped here. SQLite ALTER TABLE DROP
-- COLUMN works in 3.35+ but is risky on existing databases mid-flight; the Go
-- writers for those columns are removed in the same audit commit, so they
-- become inert ambient telemetry until a v1 schema lock cleans them up.

DROP INDEX IF EXISTS constellation_runs_budget_low;
DROP INDEX IF EXISTS constellation_runs_parent;
