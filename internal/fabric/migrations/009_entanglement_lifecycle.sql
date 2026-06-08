-- 009_entanglement_lifecycle.sql
--
-- Entanglements gain a full lifecycle: declared → claimed → in_flight →
-- fulfilled, with withdrawn (terminal failure) and deprecated (symbol removed)
-- as branches. The base schema's UNIQUE (producer, kind, name) still treats
-- name + kind as the symbol identity; these columns add time-anchored
-- bookkeeping so the TUI can show "declared 4m ago by phase X" and the
-- coordination pre-flight can rank active intents by recency and surface the
-- in-flight signature draft a sibling coder should reuse.
--
-- (Migration 008 is 008_checkpoints.sql; this lifecycle work lands as 009.)
ALTER TABLE entanglements ADD COLUMN run_id            TEXT;
ALTER TABLE entanglements ADD COLUMN phase_id          TEXT NOT NULL DEFAULT '';
ALTER TABLE entanglements ADD COLUMN declared_at       INTEGER;
ALTER TABLE entanglements ADD COLUMN claimed_at        INTEGER;
ALTER TABLE entanglements ADD COLUMN in_flight_at      INTEGER;
ALTER TABLE entanglements ADD COLUMN terminated_at     INTEGER;
ALTER TABLE entanglements ADD COLUMN current_signature TEXT;

CREATE INDEX IF NOT EXISTS entanglements_status_name
    ON entanglements (status, name);
CREATE INDEX IF NOT EXISTS entanglements_run
    ON entanglements (run_id);
CREATE INDEX IF NOT EXISTS entanglements_active
    ON entanglements (name, status)
    WHERE status IN ('declared', 'claimed', 'in_flight', 'deprecated');

-- Backward compatibility: existing 'pending' rows migrate to 'fulfilled' when
-- their producing run is terminal-done, 'withdrawn' when terminal-failed, and
-- are left untouched otherwise (the producer is still running). The producer
-- column holds the phase id, but legacy rows may also have been keyed by run;
-- match either so no terminal row is missed.
UPDATE entanglements
   SET status = 'fulfilled', terminated_at = strftime('%s', 'now')
 WHERE status = 'pending'
   AND producer IN (SELECT id FROM constellation_runs WHERE state = 'done');

UPDATE entanglements
   SET status = 'withdrawn', terminated_at = strftime('%s', 'now')
 WHERE status = 'pending'
   AND producer IN (SELECT id FROM constellation_runs WHERE state = 'failed');
