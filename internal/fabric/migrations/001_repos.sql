-- 001_repos.sql — multi-repo foundation.
--
-- Adds the registry of git repositories Quasar may operate on, plus the
-- repo_path association on nebulas so a repo's in-flight work can be located
-- (and orphaned) when the repo is unregistered.

CREATE TABLE IF NOT EXISTS repos (
  path         TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  status       TEXT NOT NULL DEFAULT 'active',
  added_at     INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS repos_status ON repos (status);

-- Associate nebulas with the repo that owns them. Pre-existing rows default to
-- the empty string (unassociated); the supervisor backfills as it imports.
ALTER TABLE nebulas ADD COLUMN repo_path TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS nebulas_repo_path ON nebulas (repo_path);
