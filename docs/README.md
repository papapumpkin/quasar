# Quasar Documentation

The map of this `docs/` directory. Start at the top and work down; each entry
is one line — `title — one-sentence summary`.

Some documents are **planned in this nebula** and do not exist on the initial
commit. They appear here as plain `code spans` (not links) so the index never
ships a broken link; they become links as later phases land. Everything written
as a link exists now.

> Note on `file:line` citations throughout the docs: they were verified against
> `main` at write time and may drift as the code changes.

## Start here

- [README.md](../README.md) — the root entry point: what Quasar is, install,
  quick start, and core concepts.
- [architecture.md](architecture.md) — the four-layer system overview plus three
  flow walkthroughs from approval to pull request.
- [glossary.md](glossary.md) — the vocabulary, alphabetized, each term tied to
  its canonical code location.

## Core mechanics

- [constellations.md](constellations.md) — the declarative TOML workflow format,
  the four node types, the expression mini-language, and every default
  constellation walked through with Mermaid DAGs.
- [runtime.md](runtime.md) — the constellation runtime: Fire, Step, back-edges,
  cycle counting, nested dispatch, budget enforcement, the supervisor, the
  dead-coder healthcheck, and prompt-cache telemetry.
- [fabric.md](fabric.md) — the SQLite schema, every table's purpose and
  writer/reader sites, the blob store, cascading deletes, and the DB-only TUI
  rule.

## Coordination + safety

- `entanglements.md` — the cross-phase symbol-claim lifecycle (planned in this
  nebula).
- `multi-repo.md` — the fleet view, the supervisor, `trigger_queue`, and
  per-repo runtime routing (planned in this nebula).
- [safety.md](safety.md) — the git output safety perimeter: what Quasar can and
  cannot do, sandboxing, pre-commit enforcement, and token scopes.

## Operations

- [cli.md](cli.md) — the full command and flag reference: every subcommand, its
  flags, and a one-hop pointer to the `cmd/<name>.go` that implements it.
- [gc.md](gc.md) — garbage collection of completed nebulas, runs, blobs, and
  stale worktrees: the mark→grace→sweep lifecycle, per-category TTLs, the blob
  and worktree reapers, the audit log, and the `gc_runs` ledger.
- `configuration.md` — `.quasar.yaml`, environment variables, and precedence
  (planned in this nebula).
- [deployment.md](deployment.md) — running Quasar as an always-on multi-repo
  service (system requirements, directory layout, `systemd`, upgrades, backups).
- [per-repo-config.md](per-repo-config.md) — authoring a repo's `.quasar.yaml`
  and its per-repo `sensors/`, `stars/`, `skills/`, and `constellations/`
  override directories.

## Extending Quasar

- [sensors.md](sensors.md) — the sensor model: poll/cursor/dedup contracts, the
  scheduler, the GitHub sensor walkthrough, and how a draft becomes a nebula.
- [sensor-authoring.md](sensor-authoring.md) — a walkthrough for adding a new
  sensor adapter, with a complete compiling worked example.

## Contributor handbook

- `development.md` — local development workflow beyond the handbook (planned in
  this nebula).
- [CLAUDE.md](../CLAUDE.md) — the developer handbook: build and test commands,
  package layout, Go conventions, and nebula authoring rules.

## Historical

These are preserved as artifacts of the project's evolution. They are **not**
the current state of the codebase; structural fixes they describe have since
shipped.

- [audit-2026-06-08.md](audit-2026-06-08.md) — a snapshot of the codebase state
  on 2026-06-08; the structural gaps it identifies (master-review wiring,
  trigger_queue consumer, dead `file_claims`) have since been fixed.
- [constellation-runtime-followup.md](constellation-runtime-followup.md) — a
  design follow-up log for the constellation runtime; superseded by the shipped
  runtime and this docs set.
- [integrations.md](integrations.md) — **superseded.** Now a one-line redirect:
  the `TicketSource` interface and `internal/integrations/` package were renamed
  to the sensors model; see [sensors.md](sensors.md) for the current design.
- [superpowers/specs/2026-06-03-quasar-autonomous-issue-to-pr-design.md](superpowers/specs/2026-06-03-quasar-autonomous-issue-to-pr-design.md)
  — the original autonomous issue-to-PR design spec; historical context for the
  flows now documented in [architecture.md](architecture.md).

## Tooling in this directory

- `linkcheck_test.go` — a Go test (`TestInternalLinksResolve`) that fails the
  build if any relative Markdown link in `docs/*.md` points at a missing file.
  It is why planned documents above are written as code spans rather than links.
