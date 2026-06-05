-- 002_nebulas_to_sqlite.sql — make SQLite canonical for nebula state.
--
-- The base schema and 001 created a thin nebulas table (ticket-draft
-- provenance only). The multi-repo model promotes the nebula to the universal
-- artifact that flows through the entire lifecycle, so this migration:
--   * rebuilds nebulas with the denormalized manifest blocks, lifecycle
--     columns, and a real repo_path foreign key to repos(path);
--   * adds the phases table (bodies live in the blobstore, addressed by hash);
--   * adds the blobs registry the blobstore mark-and-sweep GC walks.
--
-- SQLite cannot add a FOREIGN KEY to an existing column, so the nebulas table
-- is rebuilt via the canonical create-copy-drop-rename dance. Pre-existing rows
-- keep their id/provenance/status; manifest blocks and timestamps backfill as
-- the supervisor imports filesystem nebulas. Foreign-key ENFORCEMENT is a
-- per-connection PRAGMA left off for now (the repo registry orphans nebulas on
-- unregister rather than cascading deletes); the constraint is declared so the
-- relationship is captured and enforcement can be switched on later.

CREATE TABLE nebulas_new (
  id            TEXT PRIMARY KEY,
  repo_path     TEXT NOT NULL DEFAULT '' REFERENCES repos(path) ON DELETE CASCADE,
  name          TEXT NOT NULL DEFAULT '',
  description   TEXT,

  -- source attribution (empty/NULL for manually authored)
  source_type   TEXT NOT NULL DEFAULT '',
  source_name   TEXT,
  source_id     TEXT,
  source_url    TEXT,

  -- legacy on-disk pointer retained for the ticket-draft flow
  path          TEXT NOT NULL DEFAULT '',

  -- denormalized manifest blocks (TOML)
  defaults_toml   TEXT,
  execution_toml  TEXT,
  context_toml    TEXT,

  -- lifecycle status
  status        TEXT NOT NULL DEFAULT 'draft',

  -- accumulated through the lifecycle
  master_review_toml TEXT,
  pr_url             TEXT,
  pr_number          INTEGER,
  pr_opened_at       INTEGER,
  pr_merge_sha       TEXT,

  created_at    INTEGER NOT NULL DEFAULT 0,
  updated_at    INTEGER NOT NULL DEFAULT 0,
  gc_at         INTEGER
);

INSERT INTO nebulas_new (id, repo_path, source_type, source_name, source_id, path, status)
  SELECT id, repo_path, source_type, source_name, source_id, path, status FROM nebulas;

DROP TABLE nebulas;
ALTER TABLE nebulas_new RENAME TO nebulas;

CREATE INDEX IF NOT EXISTS nebulas_repo_status ON nebulas (repo_path, status);
CREATE INDEX IF NOT EXISTS nebulas_repo_path   ON nebulas (repo_path);
CREATE INDEX IF NOT EXISTS nebulas_source      ON nebulas (source_name, source_id);
CREATE INDEX IF NOT EXISTS nebulas_gc          ON nebulas (gc_at) WHERE gc_at IS NOT NULL;

CREATE TABLE phases (
  nebula_id          TEXT NOT NULL REFERENCES nebulas(id) ON DELETE CASCADE,
  id                 TEXT NOT NULL,
  seq                INTEGER NOT NULL,
  title              TEXT NOT NULL,
  body_blob_hash     TEXT NOT NULL,         -- sha256 -> ~/.quasar/blobs/...
  body_preview       TEXT,                  -- first ~500 chars for fleet display
  frontmatter_toml   TEXT NOT NULL,

  status             TEXT NOT NULL DEFAULT 'pending',
  started_at         INTEGER,
  completed_at       INTEGER,
  result_toml        TEXT,                  -- cost, sha, etc. (small structured)
  diff_blob_hash     TEXT,                  -- sha256 of the cycle's diff
  PRIMARY KEY (nebula_id, id)
);

CREATE INDEX IF NOT EXISTS phases_status ON phases (nebula_id, status);

CREATE TABLE IF NOT EXISTS blobs (
  hash         TEXT PRIMARY KEY,
  size_bytes   INTEGER NOT NULL,
  created_at   INTEGER NOT NULL,
  last_seen_at INTEGER NOT NULL
);
