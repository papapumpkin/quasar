+++
id = "safety-and-ops"
title = "Safety perimeter, GC engine, CLI reference — the operations surface a deployer reads"
type = "task"
priority = 2
depends_on = ["foundation"]
scope = [
    "docs/safety.md",
    "docs/gc.md",
    "docs/cli.md",
]
+++

## Problem

`docs/safety.md` exists but predates major changes: the gitops package is now
load-bearing for every git write, the pre-commit pipeline is uniform across
all commits, and the worktree-per-cycle isolation is real. The doc needs to
reflect that reality.

There is no `docs/gc.md` despite the GC engine having shipped TTL-based mark-
and-sweep, the JSONL audit log, the gc_runs ledger, and the worktree reaper.

There is no `docs/cli.md` despite the CLI having grown to 15+ subcommands.
Operators have no single place to look up what a command does or what its
flags mean.

## Solution

### `docs/safety.md` — rewrite

#### TOC

1. **What Quasar can do** — explicit allowlist:
   - Open PRs against base branches (via `gh`, scoped to PR-create perms)
   - Push to `quasar/*` branches in the repo's worktrees
   - Read everything (issues, comments, file contents)
   - Run claude in subprocesses scoped to per-cycle worktrees
2. **What Quasar cannot do** — explicit denylist:
   - Force-push to any branch (`internal/gitops/push.go` `isForbiddenBranch`)
   - Push directly to main/master/the base branch
   - Merge PRs (the `gh pr merge` command is denied in star tool grants)
   - Delete branches or tags
   - Commit on behalf of a star (commits flow only through the runtime's
     `commit` builtin)
3. **The safety perimeter** — `internal/gitops/` is the only package
   that runs `git`. Arch test
   `TestNoDirectGitExecOutsideGitops` enforces this. Reference
   `internal/arch_test/safety_test.go` for the test, name known
   exceptions (the TODO-migrate list) and why they're not yet folded in.
4. **Stars and git writes — the SAFETY INVARIANT** — explain why stars
   cannot have git-write tools. Reference `internal/constellations/
   dispatch_star.go` for the invariant comment. Show what a star's
   `[tools].denied` block should contain.
5. **Worktree isolation per cycle** — each `constellation_runs` gets a
   fresh worktree under `.git/worktrees/quasar/<run-id>/`. The
   merge gate runs in its own temp worktree. Reference where the
   worktree paths are constructed.
6. **The pre-commit gate** — every `gitops.Client.Commit` invocation
   threads the repo's `[pre_commit]` config; there is no bypass. The
   `runtime.go` commit invocation is the single thread point. Cover
   `gitops.PreCommitConfig.FailOnError` semantics.
7. **Token scopes** — recommended GitHub PAT scopes for the bot user.
   `public_repo` for open-source, `repo` for private; never admin or
   workflow-write.
8. **What to do if Quasar misbehaves** — kill the supervisor (Ctrl-C
   on the fleet), inspect `.quasar/supervisor.log`, look at
   `constellation_runs.state` for stuck runs, kill specific runs via
   `quasar runs kill <id>` (verify command exists; if not, document
   the SQL workaround).
9. **Audit trail** — the JSONL logs (gc-audit, conflict_resolutions,
   coordination_log, health_events, cache_metrics) — what each
   captures and where it lives.

### `docs/gc.md` — new

#### TOC

1. **Why GC exists** — fabric grows unboundedly without a reaper:
   completed nebulas, their phases, blobs, star_invocations, sensor
   events, trigger queue rows, constellation runs.
2. **The lifecycle phases** — mark (soft-delete via `deleted_at`),
   wait grace window, sweep (hard delete via FK cascade).
3. **Per-category TTLs** — show the `.quasar.yaml` `[gc]` block with
   defaults from `internal/config/gc.go`. Cover every category:
   `completed_nebulas`, `failed_nebulas`, `constellation_runs`,
   `sensor_events`, `trigger_queue_consumed`, `audit_log`.
4. **The mark-and-sweep blob GC** — `internal/blobstore/gc.go`. Walk
   reachable blob hashes from every registered reference column,
   sweep everything else older than `min_age_before_sweep`. Cover the
   `RegisterReference` pattern + the arch test that enforces it.
5. **The worktree reaper** — `internal/gitops/worktree_reaper.go`.
   When a run is terminal, its worktree is reclaimable. Conservative:
   never deletes a worktree whose run row is non-terminal.
6. **The audit log** — `internal/gc/audit.go`. JSONL at
   `.quasar/gc-audit.log`. One line per mark / sweep / reap. Tail
   format. The lumberjack-style rotator.
7. **The `gc_runs` ledger** — every sweep pass records a row:
   started_at, completed_at, category, swept_count, reclaimed_bytes,
   error. Reference `internal/gc/engine.go` `recordRun`. The 2026-06-08
   audit extended `quasar gc audit --since` to walk this table.
8. **CLI surface**:
   - `quasar gc run --dry-run` — preview
   - `quasar gc run --category <name>` — single-category sweep
   - `quasar gc blobs [--dry-run]` — blob sweep
   - `quasar gc audit --since 24h` — JSONL tail + gc_runs summary
   - `quasar nebula undelete <id>` — recover within grace window
9. **Safety: never GC during active work** — the engine acquires a
   busy_timeout and skips any category whose primary table has rows
   in non-terminal state. Blob sweep skips entirely if any run is
   running.
10. **Operational pattern** — a typical day's GC schedule. When to
    sweep blobs (overnight). When to rotate the audit log.

### `docs/cli.md` — new

#### TOC

One subcommand per section. For each: synopsis, full flag list, what
it actually does (file:line for the runRoot function), exit codes,
example invocation. Cover at minimum:

- `quasar` (no args) — auto-launch the cockpit; behavior in cmd/root.go
- `quasar init` — scaffold `.quasar.yaml`
- `quasar doctor` — health check
- `quasar version` — structured version on stdout
- `quasar repo register|unregister|list|pause|resume|show`
- `quasar nebula validate` / `nebula apply` / `nebula import` / `nebula undelete`
- `quasar sensor poll`
- `quasar lint`
- `quasar fleet` (alias `tui`) — multi-repo dashboard
- `quasar cockpit` — legacy single-repo browser (mark as legacy)
- `quasar gc run|blobs|audit`
- `quasar cache report`
- `quasar coordination report`
- `quasar conflicts report`
- `quasar coder report`

Each section ends with a "Implementation" note pointing at the
relevant `cmd/<name>.go` file. Operators reading this for
troubleshooting can jump to the code in one hop.

A table at the top maps each subcommand to which doc explains the
underlying feature:

| Command | Feature doc |
|---|---|
| `repo register` | docs/multi-repo.md |
| `fleet` | docs/multi-repo.md |
| `gc *` | docs/gc.md |
| ... | ... |

## Files

- `docs/safety.md` (rewrite) — the safety perimeter, updated for the
  current gitops + runtime architecture
- `docs/gc.md` (new) — GC engine + audit log + ledger
- `docs/cli.md` (new) — every subcommand, with implementation pointers

## Acceptance Criteria

- [ ] `docs/safety.md` no longer references retired packages
  (`integrations`); the SAFETY INVARIANT section cites
  `internal/constellations/dispatch_star.go`; the arch-test section
  cites `internal/arch_test/safety_test.go`
- [ ] `docs/safety.md` lists every JSONL log surface that exists today
  (gc-audit, conflict_resolutions, coordination_log, health_events,
  cache_metrics); each entry includes the path and the writer file:line
- [ ] `docs/gc.md` covers every category in the current
  `internal/gc/categories.go`, every CLI flag in `cmd/gc.go`, and the
  `gc_runs` summary the 2026-06-08 audit added
- [ ] `docs/cli.md` lists every subcommand currently in `cmd/`; the
  reviewer cross-checks against `ls cmd/`
- [ ] Every subcommand section in `docs/cli.md` has an "Implementation"
  pointer to the `cmd/<name>.go` file; the file exists for every
  pointer
- [ ] `bash scripts/lint.sh` exits 0
