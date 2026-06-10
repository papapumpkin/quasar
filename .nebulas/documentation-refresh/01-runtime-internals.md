+++
id = "runtime-internals"
title = "Runtime internals: constellations, the runtime engine, fabric DB — the 'how the machine works' tier"
type = "task"
priority = 1
depends_on = ["foundation"]
scope = [
    "docs/constellations.md",
    "docs/runtime.md",
    "docs/fabric.md",
]
+++

## Problem

The constellation runtime is the load-bearing piece of the new architecture
and the part most likely to confuse a reader. Today nothing explains:

- What a constellation actually IS (a parsed TOML DAG with edge guards
  evaluated by an expression mini-language)
- How a star differs from a skill differs from a constellation
- How `Runtime.Fire → Step → Step → ... → terminate` walks the DAG and
  persists state for crash-safe resume
- How nested constellations work (the dispatchConstellation that I shipped
  this session)
- What every column in `constellation_runs`, `star_invocations`,
  `nebulas`, `phases`, `entanglements`, `trigger_queue`, `blobs` is for

A reader needs all three of these docs to understand the runtime as a
working system rather than a pile of go files.

## Solution

### `docs/constellations.md` — new

#### TOC

1. **What a constellation is** — DAG of typed nodes, edges with `when:`
   guards, declarative `[meta].max_cycles`, no Go constants for cycle
   limits. Reference `internal/artifacts/types.go` for the loaded struct.
2. **The four node types** — table form:
   | Type | What runs | Defined in | Outputs shape |
   |---|---|---|---|
   | star | an LLM agent | `internal/artifacts/defaults/stars/*.md` | `{result, cost_usd, session_id}` |
   | builtin | a Go operator | `internal/constellations/operators.go` | per-operator |
   | constellation | a child constellation, run synchronously | TOML `ref = "..."` | `{state, run_id, <child outputs>}` |
   | phase_iterator | not yet supported | — | — |
3. **Stars** — the markdown+TOML format. Fields: `name`, `model`,
   `fallback_model`, `skills`, `[tools]`, `[output]`. Walk through
   `internal/artifacts/defaults/stars/coder.md` line by line. Show how
   skills compose (Tools.Allowed = base ∪ each skill's tools_add).
4. **Skills** — what they're for, their format, how composition works.
   `internal/artifacts/defaults/skills/`.
5. **Edge guards and the expression mini-language** — `dot.access`, `==`,
   `&&`, `||`, ternary `cond ? a : b`, string interpolation, tiny stdlib
   (`len`, `has`, `empty`, `default`). Reference `internal/artifacts/
   expr_parse.go` and `expr_eval.go`. Show a real expression with each
   construct, broken down.
6. **The default constellations**, walked through:
   - **coder-reviewer** — the inner loop (`internal/artifacts/defaults/
     constellations/coder-reviewer.toml`). Diagram + cycle-cap behavior.
   - **master-review** — the outer loop with the nested coder-reviewer
     back-edge (`internal/artifacts/defaults/constellations/
     master-review.toml`). Show the fix-loop. Note: this back-edge landed
     in the 2026-06-08 audit; older docs may show a PLACEHOLDER routing.
   - **architect**, **architect-fix**, **open-pr**, **nebula-lifecycle**,
     **merge-gate**, **merge-conflict-resolve** — one paragraph each.
7. **Authoring a constellation override** — drop a file at
   `<repo>/constellations/<name>.toml` and the loader picks it over the
   embedded default. Reference `internal/artifacts/loader.go` for the
   override path resolution.

### `docs/runtime.md` — new

#### TOC

1. **The Runtime struct + RuntimeOpts** — every field, what it does, what
   it costs to omit. Reference `internal/constellations/runtime.go` for the
   actual definitions.
2. **Fire** — step-by-step: load constellation, find entry node, snapshot
   the nebula, build initial State, insert run row, initialize budget,
   return run_id. Reference the actual Fire function with line numbers.
3. **Step** — the dispatch switch (NodeBuiltin/NodeStar/NodeConstellation),
   how outputs become state nodes, edge evaluation via the expression
   engine, back-edge detection (target precedes source in node order),
   cycle increment, persistence.
4. **dispatchStar** — the SAFETY INVARIANT. Stars cannot have git-write
   tools because the runtime owns commits via the `commit` builtin. Walk
   through `internal/constellations/dispatch_star.go`. Show the
   pre-flight budget check, the coordination notes injection, the
   in-cycle checkpointer, the actual claude invocation.
5. **dispatchConstellation** — nested run dispatch (the
   2026-06-08 session's deliverable). Walk through `internal/
   constellations/dispatch_constellation.go`. Cover input seeding,
   synchronous drive to terminal, output projection.
6. **Cycle counting and back-edges** — the runtime detects a back-edge
   when `next` appears at a lower node index than `current`. Each
   traversal increments `State.Cycle`. `meta.max_cycles` is the
   declarative cap.
7. **Budget enforcement** — initialized at Fire, CheckBefore each
   invocation, decremented atomically alongside the star_invocation
   insert via `RecordCost`. Reference `internal/constellations/budget.go`.
8. **State persistence** — what gets saved when. The `State` struct, the
   `dag_state_toml` column, MarshalState/UnmarshalState. Why this enables
   crash-safe Resume.
9. **The Supervisor** — `internal/constellations/supervisor.go`. Claim,
   fire, retry semantics. Cover the `Firer` interface, `SingleRepoFirer`,
   `RuntimeCacheFirer`, the multi-repo `RuntimeCache`. Note the
   diagnostic-log routing in `cmd/fleet.go`.
10. **The healthcheck and dead-coder detection** — the multi-signal
    monitor (wall-clock, file write idle, token rate, tool ratio, CPU
    idle), the Dead state, SIGTERM path, partial-work handoff. Reference
    `internal/claude/healthcheck.go`, `internal/claude/signals.go`.
11. **In-cycle checkpointing** — snapshot the worktree after each green
    build so dead-coder termination loses at most one build cycle.
    Reference the checkpointer.
12. **Prompt cache + telemetry** — the two-zone prompt layout (stable
    system prompt + volatile user prompt), the `CacheMetricStore` JSONL
    log, the `--exclude-dynamic-system-prompt-sections` flag the
    invoker passes. Reference `internal/agent/prompt_layout.go` and
    `internal/claude/claude.go` `buildArgs`.

### `docs/fabric.md` — new

#### TOC

1. **What fabric is** — the canonical SQLite store. Single file at
   `.quasar/fabric.db`. Schema lives in `internal/fabric/migrations/`.
   Every long-lived state crosses this layer; nothing important lives
   only in memory.
2. **Migrations directory** — files are run in lexical order; the
   `schema_migrations` table records what's applied. Walk through what
   each numbered migration introduces (000-010).
3. **Tables** — for each table: purpose, key columns, writers (file:line),
   readers (file:line), telemetry-only flag if applicable. Order:
   - `repos` — registered repo paths
   - `nebulas` — the canonical nebula store (replaces .nebulas/<id>/ on
     disk for fully-migrated repos)
   - `phases` — one row per phase, with body content in blobs
   - `blobs` — content-addressed blob store (sha256, zstd, fanout)
   - `sensor_cursors`, `sensor_events` — per-sensor poll state +
     dedup
   - `constellation_runs` — per-Fire row, with current_node + dag_state_toml
     for resume
   - `star_invocations` — per-Step row, with cost_usd for budget
   - `trigger_queue` — pending → consumed; drained by Supervisor
   - `entanglements` — six-state lifecycle for cross-phase coordination
   - `checkpoints`, `checkpoint_files` — in-cycle worktree snapshots
   - `gc_runs` — per-sweep ledger
   - `discoveries`, `file_claims`, `pulses`, `fabric` — legacy / partial;
     mark as such, link to audit-2026-06-08.md
4. **Cascading deletes** — the FK graph. A nebula delete cascades to its
   phases, its constellation_runs, its star_invocations, its
   entanglements. Show why GC can safely delete a single nebula row.
5. **Blob lifecycle** — write on Put (zstd-compress and hash), GC mark
   pass walks every registered reference column, sweep deletes
   unreferenced blobs older than `min_age_before_sweep`. Reference
   `internal/blobstore/`.
6. **State.TOML serialization** — how dag_state_toml is structured
   (`Inputs`, `Nodes`, `Nebula` snapshot, `Cycle`, `Meta`). Show a
   sample serialized state.
7. **Reading from outside the runtime** — the fleet view's pattern.
   "TUI is DB-only" arch rule: TUI reads via fabric stores, never
   imports runtime / sensors / gc. Reference `internal/arch_test/
   boundaries_test.go`.

## Files

- `docs/constellations.md` (new) — declarative workflow surface
- `docs/runtime.md` (new) — the engine + supervisor + healthcheck
- `docs/fabric.md` (new) — DB schema and every table's role

## Acceptance Criteria

- [ ] `docs/constellations.md` cites file:line for at least: the
  Constellation struct, each node type's dispatch site, the expression
  parser, and each shipped default constellation
- [ ] `docs/constellations.md` includes a Mermaid DAG diagram of the
  coder-reviewer constellation and one of the master-review constellation
  (showing the new back-edge to the inner coder-reviewer)
- [ ] `docs/runtime.md` covers every section in the TOC above with at
  least one file:line citation per section
- [ ] `docs/runtime.md` includes a sequence diagram or numbered step
  list for the Fire → Step → terminate happy path of a coder-reviewer
  run, with file:line for each step
- [ ] `docs/fabric.md` covers every table in the current schema; each
  entry names the migration that introduced it, its purpose, at least
  one writer site, at least one reader site (or notes "telemetry-only"
  with audit-2026-06-08.md citation)
- [ ] No doc claims a feature that isn't in code; spot-check by greping
  for symbol names mentioned in the docs
- [ ] All Mermaid blocks have valid syntax (the GitHub Markdown
  preview renders them)
- [ ] `bash scripts/lint.sh` exits 0
