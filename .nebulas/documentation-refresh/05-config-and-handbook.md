+++
id = "config-and-handbook"
title = "Configuration reference + contributor handbook + CLAUDE.md alignment — the last mile so the docs and code stay yoked"
type = "task"
priority = 3
depends_on = ["foundation", "runtime-internals", "coordination", "safety-and-ops", "sensors"]
scope = [
    "docs/configuration.md",
    "docs/development.md",
    "CLAUDE.md",
    "docs/README.md",
    "docs/audit-2026-06-08.md",
    "docs/constellation-runtime-followup.md",
    "docs/per-repo-config.md",
    "docs/deployment.md",
]
+++

## Problem

After phases 0–4 land we have a doc suite that covers every feature, but
three things still need attention:

1. **`docs/configuration.md` doesn't exist** as a single reference for
   `.quasar.yaml`. Configuration knobs are scattered across the per-feature
   docs. Operators need one page.
2. **`docs/development.md` doesn't exist** — the Go conventions in
   CLAUDE.md are aimed at LLM agents authoring nebulas, not at a human
   contributor. A human handbook (style guide, testing patterns, how to
   add a new sub-package, how to run the arch tests) is missing.
3. **CLAUDE.md needs a final pass** — after the audit + this nebula's
   first five phases, claims in CLAUDE.md must be cross-checked against
   the new doc set so nothing contradicts.
4. **Older docs** (`audit-2026-06-08.md`, `constellation-runtime-followup.md`,
   `per-repo-config.md`, `deployment.md`) need either incorporation,
   redirect notes, or "historical snapshot" framing so a reader knows
   what's current.
5. **`docs/README.md` from phase 0** needs to be revisited with the full
   doc set in hand so the index is comprehensive.

## Solution

### `docs/configuration.md` — new

#### TOC

1. **Configuration precedence** — CLI flags > env (`QUASAR_*`) >
   `.quasar.yaml` > defaults. Reference `cmd/root.go` initConfig.
2. **Global vs per-repo `.quasar.yaml`** — global lives at
   `~/.quasar.yaml`; per-repo at `<repo>/.quasar.yaml`. Per-repo
   `[pre_commit]` is loaded by the multi-repo runtime via
   `repoPreCommitFor` (`cmd/fleet.go`); global config is loaded by
   subcommands via `config.Load()`.
3. **The full `.quasar.yaml` surface** — every top-level key, each as a
   sub-section. For each: schema, default, what reads it. Cover at
   minimum:
   - `claude_path`, `verbose`
   - `[pre_commit]` — `commands`, `fail_on_error`
   - `[github]` — `base_branch` and any retained ticket settings
   - `[budget]` — `default_max_usd`, `default_max_review_cycles`
   - `[branch]` — `prefix`, `base`
   - `[gc]` — `enabled`, `tick_interval`, `grace_window`, `[gc.ttls.*]`,
     `[gc.blobs]`
   - `[cockpit]` — `enabled`, `addr`, `trusted_proxies` (if implemented;
     verify before claiming)
   - `[merge_gate]` — `verify_command`, `verify_timeout`
   - `[health]` per-star (cross-link to runtime.md's healthcheck section)
4. **Per-sensor TOML** — `<repo>/sensors/<name>.toml`. Cross-link to
   docs/sensor-authoring.md for the schema.
5. **Per-nebula TOML** — `<repo>/.nebulas/<id>/nebula.toml`. Cross-link
   to docs/architecture.md for the nebula model. Cover the manifest
   surface: `[nebula]`, `[defaults]`, `[execution]`, `[context]`,
   `[dependencies]`. Note that `gate` has been removed (only `trust`
   is supported going forward).
6. **Environment variables** — every `QUASAR_*` Quasar reads. Cover
   `CLAUDECODE` (stripped), `CLAUDE_CODE_DISABLE_MCP_POPUPS` (added).
7. **Token resolution** — file > env > vendor fallback. Cross-link
   docs/sensors.md.

### `docs/development.md` — new

#### TOC

1. **Audience** — human contributors, not LLM agents. (CLAUDE.md is the
   LLM agent handbook; this doc is for humans.)
2. **Local setup** — `go build -o quasar .`, where the binary writes,
   where the test database lives.
3. **Repo layout** — every top-level directory + what's in it. Reference
   the CLAUDE.md Project Structure block but expand: `internal/agent/`,
   `internal/artifacts/`, `internal/blobstore/`, `internal/bus/`,
   `internal/checkpoint/`, `internal/claude/`, `internal/config/`,
   `internal/constellations/`, `internal/dag/`, `internal/fabric/`,
   `internal/filter/`, `internal/gc/`, `internal/gitops/`,
   `internal/loop/`, `internal/nebula/`, `internal/neutron/`,
   `internal/repos/`, `internal/sensors/`, `internal/snapshot/`,
   `internal/telemetry/`, `internal/tui/`, `internal/tycho/`,
   `internal/ui/`, plus `cmd/` and `docs/`.
4. **Build + test commands** — `go build ./...`, `go test ./...`,
   `bash scripts/lint.sh`. The arch tests under `internal/arch_test/`.
5. **The arch tests** — what each enforces (TUI is DB-only, no direct
   git outside gitops, no direct gh outside sensors/github, file size
   < 400 lines per file, file count < 20 per package, godoc on
   exported symbols, etc.). Reference `internal/arch_test/`.
6. **Style** — pointers into CLAUDE.md's Go Conventions section so a
   contributor doesn't have to read CLAUDE.md from the LLM perspective.
   Cover: interface placement, error handling, function size, testing
   patterns, output destinations (stderr for human-facing, stdout for
   machine-readable).
7. **Adding a sub-package** — checklist. New directory under `internal/`,
   doc comment on the package, exported symbols documented, tests in
   the same package (no `_test` package unless intentional), arch test
   updates if relevant.
8. **Adding a new constellation** — drop a TOML file in
   `internal/artifacts/defaults/constellations/`, run `quasar lint` to
   verify the expression syntax, write at least one runtime test that
   walks the DAG.
9. **Adding a new star or skill** — same pattern in
   `internal/artifacts/defaults/stars/` or `.../skills/`.
10. **Working with the fabric** — when to query the fabric directly,
    when to use a typed store, the test-DB pattern from
    `internal/fabric/sqlite.go`.
11. **Working with the bus** — when to publish, when to subscribe,
    the fire-and-forget convention for `bus.Publish` errors.
12. **Writing a nebula spec** — the manifest + phase format, validation
    via `quasar nebula validate`. Cross-link to CLAUDE.md "Nebula
    Authoring".
13. **The autorun preference** — gate = trust only. Cross-link
    docs/configuration.md for the manifest surface.

### `CLAUDE.md` — sync pass

After the new docs land, walk CLAUDE.md and:

- Replace prose with cross-links where the new docs cover the topic in
  more depth (e.g. "see docs/safety.md" instead of restating the safety
  perimeter)
- Remove any remaining stale claims (the audit removed some; spot-check
  again)
- Verify the project structure block matches `docs/development.md`'s
  expanded version

### `docs/README.md` — finalize

Revisit the index from phase 0 with the full doc set in hand. Add
sections for the docs landed in phases 1–4 and this phase. Mark the
historical docs as such.

### Older docs — classify

For each:

- **`docs/audit-2026-06-08.md`** — historical snapshot of the codebase
  state on that date. Add a banner at the top: "This document is a
  historical artifact from the 2026-06-08 audit. The structural fixes
  catalogued here have shipped; the current state of these subsystems
  is described in: [links]."
- **`docs/constellation-runtime-followup.md`** — verify what's still
  relevant; if everything in it has shipped, mark as superseded with a
  banner pointing at the relevant current docs. If parts remain
  pending, keep those parts and mark the rest superseded.
- **`docs/per-repo-config.md`** — overlap with docs/configuration.md.
  If the new doc supersedes it, reduce to a redirect. If it covers
  something the new doc doesn't, keep and cross-link.
- **`docs/deployment.md`** — this exists today; verify content against
  current state. If accurate, leave with a cross-link from
  docs/README.md. If stale, mark for a future deployment-cookbook
  nebula run and add a "may be out of date" banner.

## Files

- `docs/configuration.md` (new) — the single config reference
- `docs/development.md` (new) — human contributor handbook
- `CLAUDE.md` (modify) — sync pass: cross-links + remove dupes
- `docs/README.md` (modify) — finalize the index with all phases' output
- `docs/audit-2026-06-08.md` (modify) — add historical-banner
- `docs/constellation-runtime-followup.md` (modify) — classify
- `docs/per-repo-config.md` (modify) — redirect or cross-link
- `docs/deployment.md` (modify if stale) — verify and banner if needed

## Acceptance Criteria

- [ ] `docs/configuration.md` covers every top-level `.quasar.yaml` key
  that exists in `internal/config/`; reviewer cross-checks against
  `Config` struct fields
- [ ] `docs/development.md` lists every directory under `internal/`
  with at least a one-sentence description (cross-check against `ls
  internal/`)
- [ ] `docs/development.md` describes every arch test in
  `internal/arch_test/`; cross-check against the test files
- [ ] CLAUDE.md does not duplicate prose from the new docs; topics that
  appear in both are cross-linked, not restated
- [ ] CLAUDE.md project structure block matches `docs/development.md`
  expanded version (sync direction: development.md is canonical)
- [ ] `docs/README.md` is comprehensive: every file in `docs/` appears
  in the index with a classification (current / historical / redirect)
- [ ] Historical docs carry a clear banner so a reader landing on them
  via search knows their status
- [ ] No doc in `docs/` is uncategorized in `docs/README.md`
- [ ] `bash scripts/lint.sh` exits 0
