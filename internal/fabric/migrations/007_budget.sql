-- Per-run budget enforcement for constellation runs. Budget is a first-class
-- column set at Fire time and decremented per star_invocation cost; a run with
-- a NULL initial budget is uncapped (the enforcement no-ops). The columns reuse
-- the existing star_invocations cost bookkeeping rather than a separate ledger.
ALTER TABLE constellation_runs ADD COLUMN budget_usd_initial   REAL;
ALTER TABLE constellation_runs ADD COLUMN budget_usd_remaining REAL;
ALTER TABLE constellation_runs ADD COLUMN budget_exhausted_at  INTEGER;

-- Supports the TUI "running low on budget" alert: a cheap lookup of running
-- runs whose remaining budget has fallen under a dollar.
CREATE INDEX constellation_runs_budget_low
    ON constellation_runs (budget_usd_remaining)
    WHERE state = 'running' AND budget_usd_remaining < 1.0;
