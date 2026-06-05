+++
id = "arch-tests-and-docs"
title = "Architecture tests: enforce safety perimeter, blob-ref registration, sensor isolation; docs: safety, deployment, per-repo config"
type = "task"
priority = 3
depends_on = ["multi-repo-foundation", "rename-integrations-to-sensors", "file-loader-and-discrimination", "nebula-to-sqlite-migration", "github-sensor-produces-nebula", "constellation-runtime", "builtin-constellations-and-stars", "gc-engine", "tui-multi-repo-fleet-lane"]
scope = [
    "internal/archtest/**",
    "docs/**",
]
+++

## Problem

Earlier phases each carried a few load-bearing rules — e.g. "no `exec.Command(\"git\", ...)` outside `internal/gitops`", "every blob-referencing column must be registered with the blobstore", "stars must not import sensor packages". Without arch tests these rules rot; new code reintroduces them silently and the next person to touch the safety perimeter or GC has to re-derive everything.

This phase makes those rules executable. It also writes the three pieces of human-readable documentation a multi-repo deployment actually needs: how the safety perimeter works, how to deploy the service on an EC2, and how to author a per-repo `.quasar.yaml`.

This is the last phase. Once it lands, the constellation-runtime nebula is done.

## Solution

### Arch tests

`internal/archtest/` holds Go-test-style assertions against the package graph. Implementation uses `golang.org/x/tools/go/packages` to load the module and walk imports/calls.

Tests (`internal/archtest/arch_test.go`):

1. **No raw git outside `internal/gitops`**
   - Walk every package, find every `exec.Command` and `exec.CommandContext` call site
   - For each call site, statically resolve the first arg; if it's the literal `"git"`, the package must be `internal/gitops/...`
   - Catches: someone reintroducing the watcher/refactor pattern of shelling out to git directly

2. **No `gh` outside `internal/sensors/github` or `internal/gitops`**
   - Same shape as above for `"gh"` literal
   - Catches: someone calling `gh pr create` from a star/operator instead of going through the `gh_open_pr` builtin

3. **Every blob-hash column is registered**
   - Parse the migrations directory; find every column whose name ends in `_blob_hash`
   - Parse every `init()` in the module; find every `blobstore.RegisterReference("table", "column")` call
   - The set of `(table, column)` pairs from migrations must equal the set from `RegisterReference` calls
   - Catches: blob refs that the GC sweep would miss → silent data loss after sweep

4. **Stars don't import sensors**
   - The `internal/stars/...` namespace (where star adapter code lives) must not import anything under `internal/sensors/...`
   - Stars receive nebulas, not sensor events; this enforces the layering
   - Catches: someone collapsing the abstraction by passing a `sensors.Event` through the runtime

5. **TUI is DB-only**
   - `internal/tui/...` must not import `internal/sensors/github`, `internal/runtime`, `internal/gc`
   - TUI reads from SQLite via `internal/fabric/...` only
   - Catches: TUI growing knobs that bypass the runtime

6. **No `time.Now()` in GC engine code paths**
   - `internal/gc/...` must not call `time.Now()` directly; all time access goes through the injected `clock` field
   - Catches: tests that suddenly become flaky because someone added a "harmless" timestamp

7. **No CLI in `cmd/` writes to stdout for human output**
   - Every `cmd/` package must use `ui.Printer` for human messages; `os.Stdout` writes are only allowed in commands explicitly tagged `// arch-test: stdout-allowed` (e.g. `version`, structured output)
   - Catches: the existing CLAUDE.md convention being silently broken

8. **No `state.toml` writes**
   - No production code writes a file matching `**/state.toml`
   - Catches: phase-3 regression where someone reintroduces dual-write

Each test is its own Go test function so failures are localized; CI runs them as part of `go test ./...`.

### Implementation notes

`internal/archtest/loader.go` loads the module once per test process via `packages.Load(...)` with the appropriate mode for syntax + types. Per-test functions then walk the resulting `[]*packages.Package`. Caching is essential — loading the whole module is slow (~3-5s).

`internal/archtest/calls.go` provides a `FindCalls(funcQualifiedName, argResolver)` helper that returns `[]CallSite` with file:line + statically-resolved literal args. Used by tests 1, 2, 7.

`internal/archtest/imports.go` provides `ImportsOf(pkgPath)` returning the recursive import closure for an arch boundary check.

`internal/archtest/sql.go` parses the migrations directory using a very small SQL-ish regex pass (not a full parser — we only need column names matching `_blob_hash`). The migrations are already small and grep-friendly.

Tests are *slow* by Go standards (3-10s total) — that's fine; they catch class-of-bug regressions that would otherwise eat days.

### Documentation

Three new files under `docs/`. Markdown, no auto-generation.

#### `docs/safety.md`

Audience: someone evaluating whether to point Quasar at their repo.

Sections:
- **What Quasar can and cannot do** — explicit list: opens PRs to `quasar/*` branches, never force-pushes, never merges, never deletes branches, never pushes to main/master. Reads everything, writes only to its own branches.
- **The safety perimeter** — the `internal/gitops/` package is the only path that runs `git`. Stars cannot shell out. The `gh_open_pr` builtin uses a token scoped to PR creation only.
- **Sandboxing model** — each constellation_run gets a fresh worktree under `.git/worktrees/quasar/<run-id>/`. The runtime never reuses worktrees. Pre-commit hooks run in the worktree.
- **Pre-commit enforcement** — every `git commit` invocation goes through `internal/gitops/commit.go` which runs the repo's `[pre_commit].commands` before commit and aborts on failure. There is no bypass.
- **Token scopes** — recommended GitHub PAT scopes for the bot user: `public_repo` (or `repo` for private), no admin, no workflow-write.
- **What to do if Quasar misbehaves** — kill the supervisor, soft-undelete recent nebulas, inspect `gc-audit.log` and `constellation_runs` for what was attempted.

#### `docs/deployment.md`

Audience: operator setting up Quasar on EC2 (or any always-on Linux host).

Sections:
- **System requirements** — Go 1.25+, SQLite (vendored), `git` 2.30+, `gh` CLI authenticated as the bot user, `claude` CLI available, optional `zstd` for blob compression (vendored fallback).
- **Directory layout**:
  ```
  /opt/quasar/                       # binary + embedded defaults
  /var/lib/quasar/state.sqlite       # canonical state
  /var/lib/quasar/blobs/             # content-addressed store
  /var/lib/quasar/gc-audit.log       # JSONL audit log
  /var/lib/quasar/runs/<run-id>/     # ephemeral run logs
  /etc/quasar/quasar.yaml            # global config
  /srv/repos/<owner>/<name>/         # checked-out repos
  ```
- **Registering a repo** — walkthrough of `quasar repo register /srv/repos/papapumpkin/quasar`, what tables get rows, what files the resolver expects.
- **systemd unit** — example `quasar.service` invoking `quasar supervise`, with `Restart=on-failure`, `WatchdogSec=120s` (heartbeat to `sd_notify`), `ProtectSystem=strict`, `ReadWritePaths=/var/lib/quasar /srv/repos`.
- **Upgrading** — stop the unit, swap the binary, the supervisor's single-instance guard prevents overlap, sensors resume from cursors automatically.
- **Backup** — snapshot `/var/lib/quasar/` atomically (SQLite supports `.backup` API; blobs are content-addressed so rsync is safe).

#### `docs/per-repo-config.md`

Audience: the developer adding Quasar to their repo for the first time.

Sections:
- **The `.quasar.yaml` at repo root** — minimum required keys, with comments.
  ```yaml
  pre_commit:
    commands:
      - go vet ./...
      - go test -short ./...
    fail_on_error: true

  budget:
    default_max_usd: 30.0
    default_max_review_cycles: 5

  branch:
    prefix: quasar/    # all Quasar branches start with this
    base: main         # what Quasar PRs target
  ```
- **The `sensors/` directory** — one TOML per sensor instance, schema documented inline with the GitHub sensor as the worked example (uses Phase 4's format).
- **The `stars/`, `skills/`, `constellations/` directories** — how per-repo overrides work; mention that authoring is optional and the embedded defaults cover the common case.
- **Worked example** — a complete repo config that polls GitHub issues labeled `quasar`, uses default stars, fires the architect on approval, uses `go vet && go test -short` as pre-commit.
- **Troubleshooting** — `quasar lint` for static issues; `quasar sensor poll <repo> <sensor>` for forcing a poll; `quasar gc audit --since 1h` for "where did my nebula go".

### Test approach

- Arch tests: each is a Go test that loads packages, walks the AST, and asserts the rule. Run via `go test ./internal/archtest/...`. They run as part of CI.
- Docs: render with `markdownlint` (vendored config); CI fails on broken internal links via a tiny `linkcheck` test that parses every `[label](path)` and checks the path exists.

## Files

- `internal/archtest/loader.go` (new)
- `internal/archtest/calls.go` (new)
- `internal/archtest/imports.go` (new)
- `internal/archtest/sql.go` (new)
- `internal/archtest/arch_test.go` (new) — the 8 tests above
- `internal/archtest/testdata/` (new) — tiny fixture module for unit-testing the loader helpers
- `docs/safety.md` (new)
- `docs/deployment.md` (new)
- `docs/per-repo-config.md` (new)
- `docs/linkcheck_test.go` (new) — internal-link checker
- `README.md` — update top-level "How to deploy" section to link to the new docs

## Acceptance Criteria

- [ ] `go test ./internal/archtest/...` passes on the post-runtime tree
- [ ] Arch test 1 fails if a new `exec.Command("git", ...)` is added outside `internal/gitops`
- [ ] Arch test 2 fails if `exec.Command("gh", ...)` is added outside `internal/sensors/github` or `internal/gitops`
- [ ] Arch test 3 fails if a new `*_blob_hash` column is added without a matching `blobstore.RegisterReference`
- [ ] Arch test 4 fails if anything under `internal/stars/...` imports `internal/sensors/...`
- [ ] Arch test 5 fails if `internal/tui/...` imports `internal/sensors/github`, `internal/runtime`, or `internal/gc`
- [ ] Arch test 6 fails if `internal/gc/...` calls `time.Now()` directly
- [ ] Arch test 7 fails if a `cmd/` package writes to `os.Stdout` without the `// arch-test: stdout-allowed` marker
- [ ] Arch test 8 fails if any production code writes a `state.toml` file
- [ ] `docs/safety.md`, `docs/deployment.md`, `docs/per-repo-config.md` exist and pass linkcheck
- [ ] `README.md` top-level deployment section links to the three new docs
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all exit 0
