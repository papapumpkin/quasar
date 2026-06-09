+++
id = "coordination"
title = "Coordination layer: entanglements, multi-repo fleet, conflict resolution — the cross-cutting concerns the runtime relies on"
type = "task"
priority = 2
depends_on = ["runtime-internals"]
scope = [
    "docs/entanglements.md",
    "docs/multi-repo.md",
    "docs/conflict-resolution.md",
]
+++

## Problem

Three subsystems exist that don't fit cleanly inside any single Runtime
explanation but are load-bearing for correctness:

1. **Entanglements** — the symbol-level coordination that keeps parallel
   coders from silently overwriting each other's work
2. **Multi-repo** — the per-repo Runtime cache + Supervisor that makes
   one Quasar instance serve many repositories
3. **Conflict resolution** — the merge-gate constellation and
   conflict-resolver star that turn a marker conflict into a routed
   recovery instead of a corrupted tree

Each deserves its own doc with diagrams, lifecycle tables, and code
citations. None of them existed in working form until the most recent
nebulas, so existing docs say nothing about them.

## Solution

### `docs/entanglements.md` — new

#### TOC

1. **What an entanglement is** — a producer's typed declaration that it
   intends to produce, modify, or deprecate a named symbol. Other phases
   read these to decide whether they can safely proceed.
2. **The five-state lifecycle** — diagram + table. (Note: the original
   spec described six states including `claimed`; the 2026-06-08 audit
   removed `Claim` because the runtime never invoked it. Document the
   actual production behavior: declared → in_flight → fulfilled |
   withdrawn | deprecated.)
   - `declared` — architect operator parsed the spec and detected the
     symbol
   - `in_flight` — a green build touched the symbol; `current_signature`
     reflects what's about to ship
   - `fulfilled` — the producer's run terminated `done` and the merge
     gate marked it shipped
   - `withdrawn` — the producer's run terminated `failed`
   - `deprecated` — the producer's diff removed the symbol declaration
3. **The state transitions in code** — for each transition, the call site
   (file:line):
   - architect operator → `Declare` (`internal/constellations/operators.go`)
   - dispatch post-commit → `MarkInFlight` (`internal/constellations/
     runtime.go` or operators.go)
   - neutron diff walk → `Deprecate` (`internal/neutron/`)
   - merge-gate success → `Fulfill` (`internal/constellations/
     operators_merge.go`)
   - terminal failure → `Withdraw` (`internal/constellations/runtime.go`)
4. **The neutron diff walker** — how producer-symbol extraction from
   the spec text works, how deletion detection from the diff works.
5. **Coordination preflight** — before each coder dispatch, the runtime
   queries active entanglements that intersect the phase's scope and
   injects a `## Coordination notes` section into the prompt. Reference
   `internal/constellations/coordination.go`. Show a sample injected
   block with each advice template (`in_flight`, `deprecated`, etc.).
6. **The advisory contract** — coordination is advisory, not authoritative.
   A coder MAY override notes (via `[coordination].ignore_*` in phase
   frontmatter) and the override is recorded to telemetry. The merge gate
   is the hard backstop.
7. **The audit findings on `Claim`** — explicit pointer to
   docs/audit-2026-06-08.md → Gap A, noting that the schema columns
   `claimed_at` / `phase_id` are retained for forward-compat but
   dormant.
8. **Schema and indexes** — every column on `entanglements` and what
   it's for. Reference `internal/fabric/migrations/009_entanglement_lifecycle.sql`.

### `docs/multi-repo.md` — new

#### TOC

1. **The multi-repo model** — one Quasar instance, many registered repos.
   Each repo has its own `.quasar.yaml`, its own working directory, its
   own sensor TOML files. Shared: the fabric DB, the blob store, the
   claude invoker.
2. **Repo registration** — `quasar repo register <path>` adds a row to
   the `repos` table. Walk through `internal/repos/registry.go` and
   `cmd/repo.go`.
3. **The repos.Resolver** — how the artifacts.Loader knows to look at
   `<repo>/constellations/` for overrides before falling back to embedded
   defaults. Reference `internal/repos/resolver.go` and `internal/
   artifacts/loader.go`.
4. **The fleet view** — three lanes (awaiting approval, in-flight,
   recent), repo-grouped, with optional fold/unfold. Reference
   `internal/tui/fleet/model.go` and `internal/tui/fleet/render.go`.
   Show how each lane's query reaches the fabric.
5. **The supervisor in the fleet** — `cmd/fleet.go` `startTriggerSupervisor`
   constructs a `RuntimeCache` and starts a `Supervisor` goroutine for
   the dashboard's lifetime. Walk through the construction sequence.
   Cover the diagnostic-log routing (`.quasar/supervisor.log`, NOT
   stderr) and why.
6. **The RuntimeCache** — `internal/constellations/runtime_cache.go`.
   Shared deps vs per-repo deps, lazy construction, no memoization of
   failures (so a transient config error doesn't kill a repo for the
   session). Cite the test for per-repo isolation.
7. **The Firer interface** — Supervisor's seam. `SingleRepoFirer` for
   tests / single-repo, `RuntimeCacheFirer` for multi-repo routing by
   `repo_path` from the trigger row.
8. **End-to-end approval flow** (numbered, with file:line):
   1. Operator presses [a] in the fleet view
   2. `Store.Approve` writes a `trigger_queue` row with the nebula's
      `repo_path`
   3. Within 1s, `Supervisor.Tick` selects pending rows
   4. For each: claim (UPDATE ... WHERE state='pending'), then Fire
   5. `RuntimeCacheFirer.Fire` looks up the per-repo Runtime
   6. `Runtime.Fire` instantiates a `constellation_runs` row at the
      entry node
   7. Step is called by — actually it isn't yet, the run sits at the
      entry node until a separate driver advances it. (Be honest about
      this gap: today the Supervisor only initiates the run; advancing
      the run requires a separate Step-driver that doesn't yet exist
      in cmd/. Document this clearly so the next contributor knows
      where to wire it.)

### `docs/conflict-resolution.md` — new

#### TOC

1. **Why conflicts happen** — two parallel coders writing into worktrees
   that touch overlapping scope. Worktrees prevent in-place corruption;
   the merge gate is what catches the collision at integration.
2. **The merge gate** — `internal/constellations/operators_merge.go`
   `opMergeAttempt` and the `merge-gate.toml` constellation. Three
   outcomes: clean / markers / build_failure. Routes to the
   conflict-resolver constellation on the latter two.
3. **`MergeAttempt.Try`** — `internal/gitops/merge_attempt.go`. The
   isolated temp worktree, the verify command (default
   `go build ./... && go test -short ./...`), the verify-timeout safety
   (Setsid + group-kill + WaitDelay backstop). Reference the audit doc
   for the specific bug this fixes.
4. **The conflict-resolver star** — `internal/artifacts/defaults/stars/
   conflict-resolver.md`. Haiku-class model, narrow tool allowlist, the
   rich prompt context that includes both originating phases' specs +
   diffs + entanglement state.
5. **The two modes** — `markers` mode (literal `<<<<<<<` blocks in
   files) vs `no_markers` mode (post-merge build failure, semantic
   conflict). Same star, different rubric sections from the
   conflict-resolution-rules skill.
6. **The `render_conflict_context` builtin** — `internal/constellations/
   operators_conflict.go`. What it stitches together. Show a sample
   rendered block.
7. **Escalation rules** — config-file conflicts, delete-vs-modify on
   protected paths, repeated-failure patterns → immediate
   `_awaiting_human` without consuming a cycle.
8. **Telemetry** — `conflict_resolutions.jsonl`. The
   `emit_conflict_telemetry` builtin. `quasar conflicts report` if it
   exists (verify before claiming).

## Files

- `docs/entanglements.md` (new) — lifecycle + neutron + coordination preflight
- `docs/multi-repo.md` (new) — registry, RuntimeCache, supervisor, fleet flow
- `docs/conflict-resolution.md` (new) — merge gate + conflict-resolver star

## Acceptance Criteria

- [ ] `docs/entanglements.md` lists exactly five lifecycle statuses (the
  audited reality), with each state's set-site cited file:line
- [ ] `docs/entanglements.md` explains the Claim removal explicitly so
  a reader who finds a stale reference elsewhere knows what happened
- [ ] `docs/multi-repo.md` walks the end-to-end approval flow with
  file:line for each step
- [ ] `docs/multi-repo.md` honestly documents the Step-driver gap (no
  pretending the supervisor advances runs after firing); the prose
  points the next contributor to where the wiring should land
- [ ] `docs/conflict-resolution.md` covers both modes with sample
  context blocks
- [ ] Every cited symbol exists at the cited line; the reviewer spot-checks
  five citations per doc
- [ ] All cross-references between these three docs and the existing
  set (architecture, runtime, fabric) are bidirectional — both docs link
  to each other
- [ ] `bash scripts/lint.sh` exits 0
