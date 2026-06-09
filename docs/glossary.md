# Quasar Glossary

The vocabulary you need to read the rest of the docs and the code. Each entry
is a one-sentence definition, the canonical code location, and a "see also"
pointer to the document that covers the term in depth.

Some "see also" targets are written as plain `code spans` rather than links
because the document is **planned in this nebula** and does not exist on the
initial commit; those become links once the relevant phase lands. Existing
documents are linked normally.

File:line citations were verified against `main` at write time; symbols may
drift as the code evolves.

---

### Architect (star)

The LLM agent that decomposes an approved seed nebula into executable phases.
Defined in `internal/artifacts/defaults/stars/architect-star.md` and run by the
`architect` constellation that `fleet.Store.Approve`
(`internal/tui/fleet/fleet.go:330`) enqueues.
*See also:* `docs/sensors.md` → "How a draft becomes a nebula" (planned in this
nebula).

### Bus

The typed publish-subscribe event channel that decouples event producers
(engine, runtime) from consumers (the TUI). The `Bus` interface is at
`internal/bus/bus.go:21`; the canonical `Event` struct, whose `Kind` field
discriminates the payload, is at `internal/bus/bus.go:127`.
*See also:* `docs/runtime.md` (planned in this nebula).

### Coder (star)

The LLM agent that writes the diff for a single phase inside the coder-reviewer
constellation. Defined in `internal/artifacts/defaults/stars/coder.md`.
*See also:* `docs/constellations.md` (planned in this nebula).

### Coder-reviewer constellation

The inner loop that drives one phase to a green build: the coder writes a diff,
the runtime commits it, the reviewer judges it, and a back-edge revises up to
`[meta].max_cycles` times. Defined in
`internal/artifacts/defaults/constellations/coder-reviewer.toml` (cap at line
11).
*See also:* `docs/constellations.md` → "The default constellations" (planned in
this nebula).

### Constellation

A declarative workflow DAG written in TOML: a list of `[[nodes]]` (stars,
builtins, or nested constellations) wired by `[[edges]]` with boolean `when`
guards. The embedded defaults live under
`internal/artifacts/defaults/constellations/`; a repo may override them.
*See also:* `docs/constellations.md` (planned in this nebula).

### Cycle

One traversal of a back-edge in a constellation run — the unit the cycle cap
counts. The counter is the `Cycle` field of the run's evaluation state,
`internal/constellations/state.go:39`.
*See also:* `docs/runtime.md` → "Back-edges and cycle counting" (planned in this
nebula).

### Effector

Any external tool a run ultimately reaches: the `claude` CLI (via
`agent.Invoker`, implemented by `internal/claude/claude.go:20`), the `git`
binary (via `internal/gitops/`), and the `gh` CLI (confined to the GitHub
sensor and PR creation).
*See also:* [docs/architecture.md](architecture.md) → "Effectors".

### Entanglement

A producer's declared, in-flight, or terminal claim on a symbol (an exported
type or function) that other phases coordinate around, so a consumer phase can
wait for or react to a producer phase. The typed lifecycle store is
`internal/fabric/entanglement_store.go:22`.
*See also:* `docs/entanglements.md` (planned in this nebula).

### Fabric

The SQLite-backed persistence layer: runs, phases, nebulas, triggers, blobs,
and entanglements all live here. The database is opened by `NewSQLiteFabric`,
`internal/fabric/sqlite.go:86`.
*See also:* `docs/fabric.md` (planned in this nebula).

### file_claims (vestigial)

A table and a set of `ClaimFile`/`ReleaseFile` methods
(`internal/fabric/sqlite.go:41` and following) that survive from the
pre-constellation coordination model. No constellation-runtime path writes to
it; it is retained but dead.
*See also:* [docs/audit-2026-06-08.md](audit-2026-06-08.md).

### Fire

The verb for "instantiate a constellation run": snapshot the constellation
source, build the initial `State`, and insert a run row at the entry node.
`Runtime.Fire`, `internal/constellations/runtime.go:164`.
*See also:* `docs/runtime.md` (planned in this nebula).

### Fleet view

The multi-repo TUI showing each registered repo's awaiting-approval drafts,
in-flight runs, and recent terminal nebulas. The command is
`cmd/fleet.go:33`; the `tea.Model` is `internal/tui/fleet/model.go:30`.
*See also:* `docs/multi-repo.md` (planned in this nebula).

### Gitops perimeter

The single wrapper through which every git write passes; it permits pushes only
to `quasar/*` refs and rejects destructive ops. The allowlist regex is
`internal/gitops/push.go:23` (`quasarBranchRe`); commits route through
`internal/gitops/commit.go:42`.
*See also:* [docs/safety.md](safety.md).

### Master reviewer (star)

The LLM agent that judges a completed nebula's full diff and decides
ship / fix / escalate. Defined in
`internal/artifacts/defaults/stars/master-reviewer-star.md` and run by the
master-review constellation
(`internal/artifacts/defaults/constellations/master-review.toml`).
*See also:* `docs/constellations.md` (planned in this nebula).

### Master-review constellation

The outer loop wrapping coder-reviewer: a `fix` verdict dispatches the
coder-reviewer constellation as a nested run, then a back-edge re-judges the
applied fix, bounded by `[meta].max_cycles` (default 3). Defined in
`internal/artifacts/defaults/constellations/master-review.toml`.
*See also:* `docs/constellations.md` (planned in this nebula).

### Nebula

A multi-phase task specification: a `nebula.toml` manifest plus one Markdown
file per phase. Approved nebulas become rows in the `nebulas` table.
*See also:* the "Nebula Authoring" section of [CLAUDE.md](../CLAUDE.md).

### NodeConstellation (nested dispatch)

The node type that lets one constellation call another as a synchronous child
run — the primitive that makes master-review's `fix` a real inner loop instead
of a placeholder. Implemented by `Runtime.dispatchConstellation`,
`internal/constellations/dispatch_constellation.go:26`.
*See also:* `docs/runtime.md` (planned in this nebula).

### Phase

One file inside a nebula, ultimately one unit of coder-reviewer work. Phases
carry frontmatter (id, dependencies, scope) and persist as rows keyed by
`(nebula_id, id)`.
*See also:* the "Phase Files" section of [CLAUDE.md](../CLAUDE.md).

### reviewer_decision (builtin)

The builtin operator that parses the reviewer star's JSON output into a typed
decision (verdict, approved, comments) the coder-reviewer constellation routes
on. `opReviewerDecision`, `internal/constellations/reviewer_decision.go:40`.
*See also:* `docs/constellations.md` (planned in this nebula).

### Reviewer (star)

The LLM agent inside the coder-reviewer constellation that judges a single
phase's diff and emits the JSON the `reviewer_decision` builtin parses. Defined
in `internal/artifacts/defaults/stars/reviewer.md`.
*See also:* `docs/constellations.md` (planned in this nebula).

### Runtime

The engine that drives one repo's constellation runs, stepping each run node by
node until terminal. `internal/constellations/runtime.go:60`; the per-node
advance is `Runtime.Step`, `internal/constellations/runtime.go:214`.
*See also:* `docs/runtime.md` (planned in this nebula).

### RuntimeCacheFirer

The adapter that lazily constructs and caches one `Runtime` per repo and
satisfies the supervisor's `Firer` seam, so a single supervisor can route
triggers across many repos. `internal/constellations/runtime_cache.go:137`.
*See also:* `docs/multi-repo.md` (planned in this nebula).

### Sensor

A poll-driven adapter that turns external signals (GitHub issues, and, in
future, Slack mentions or cron ticks) into seed nebulas. The `Sensor` interface
is `internal/sensors/sensors.go:92`; the GitHub adapter's `Poll` is
`internal/sensors/github/github.go:124`.
*See also:* `docs/sensors.md` (planned in this nebula).

### Skill

A reusable Markdown + TOML prompt fragment and tool grant that composes into a
star. The embedded defaults live under
`internal/artifacts/defaults/skills/`.
*See also:* `docs/constellations.md` (planned in this nebula).

### Star

An LLM agent character defined as Markdown + TOML — the unit a constellation
`star` node instantiates. The embedded defaults live under
`internal/artifacts/defaults/stars/`.
*See also:* `docs/constellations.md` (planned in this nebula).

### Step

One node firing in a constellation run: dispatch the current node, record its
outputs, evaluate outgoing edges, persist the transition. `Runtime.Step`,
`internal/constellations/runtime.go:214`.
*See also:* `docs/runtime.md` (planned in this nebula).

### Supervisor

The component that drains `trigger_queue` and fires constellation runs in the
fleet view's lifetime. `internal/constellations/supervisor.go:53`; its claim-
then-fire loop is `Supervisor.Tick`, `internal/constellations/supervisor.go:80`.
*See also:* `docs/multi-repo.md` (planned in this nebula).

### Trigger

A row in `trigger_queue` that says "fire this constellation on this nebula,"
carrying the target nebula's `repo_path`. Inserted by `fleet.Store.Approve`
(`internal/tui/fleet/fleet.go:330`) on approval, or by the sensor scheduler.
*See also:* `docs/multi-repo.md` (planned in this nebula).

### Worktree

The per-cycle, isolated git worktree the coder writes diffs into, so concurrent
phases never collide in a shared checkout. Managed under `internal/gitops/`.
*See also:* [docs/safety.md](safety.md).
