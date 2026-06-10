+++
id = "foundation"
title = "Foundation: README + architecture overview + glossary — the onboarding surface a new contributor reads first"
type = "task"
priority = 1
scope = [
    "README.md",
    "docs/architecture.md",
    "docs/glossary.md",
    "docs/README.md",
]
+++

## Problem

A new contributor opening the repo today finds a `README.md` that predates the
constellation runtime and doesn't mention the multi-repo fleet view, the
entanglement lifecycle, the merge-gate, the supervisor, or any of the work
shipped in the last four nebulas. They have no map for navigating the docs/
directory either — `audit-2026-06-08.md` sits next to `safety.md` with no
indication of which is current and which is historical.

This phase produces the **onboarding surface**: the four documents a person
reads to figure out what Quasar is, what its core concepts are called, how
the layered architecture fits together, and which docs to read next.

## Solution

### `README.md` (root) — rewrite

The current root README is outdated (still describes Quasar as a single-task
coder-reviewer loop). Rewrite to reflect what Quasar actually is today:

1. **One-paragraph elevator pitch** — what Quasar does, what it operates on,
   what makes it different from a CLI wrapper around `claude -p`.
2. **Install** — Go build, the `quasar` binary, prerequisites (`claude`, `git`,
   `gh`).
3. **Quick start** — register a repo, write a nebula, watch it run in the
   fleet view.
4. **Core concepts** (one sentence each): nebula, phase, constellation, star,
   skill, sensor, fabric, entanglement. Link each to glossary.
5. **CLI reference** (one-line each): `init`, `doctor`, `validate`, `fleet`,
   `repo`, `sensor`, `nebula apply`, `gc`, `lint`. Each links to the relevant
   doc section.
6. **Where to read next** — pointers to docs/architecture.md (the big
   picture), docs/glossary.md (the vocabulary), docs/README.md (the index).
7. **License + contribution pointer** — link to CLAUDE.md for the developer
   handbook.

### `docs/architecture.md` — new

The Big Picture doc. Single Mermaid diagram + structured prose explaining
each layer:

```
┌─ Operator surface ────────────────────────────────┐
│  CLI commands · fleet TUI · cockpit TUI           │
└──────────────────────┬────────────────────────────┘
                       │
┌─ Orchestration ──────┴────────────────────────────┐
│  Nebula apply (legacy)   ·   Constellation runtime │
│  (Engine, WorkerGroup)   ·   (Runtime, Supervisor) │
└──────────────────────┬────────────────────────────┘
                       │
┌─ Coordination + safety ───────────────────────────┐
│  Fabric (SQLite)  ·  Entanglements  ·  Gitops     │
│  Bus events       ·  PhaseTracker   ·  Pre-commit │
└──────────────────────┬────────────────────────────┘
                       │
┌─ Effectors ──────────┴────────────────────────────┐
│  Claude CLI (agent.Invoker)  ·  Git  ·  Gh        │
└───────────────────────────────────────────────────┘
```

For each layer, prose covers:
- **Operator surface**: how a person interacts (TUI vs CLI), where stderr
  goes, how the cockpit/fleet TUIs differ. Reference `cmd/fleet.go`,
  `cmd/tui.go`, `internal/tui/fleet/model.go`.
- **Orchestration**: the two parallel orchestrators today (legacy nebula
  Engine + new Constellation Runtime). Explain which one runs in which
  context and why both exist (fleet view uses Constellation Runtime via the
  supervisor; `quasar nebula apply` uses the legacy Engine). Reference
  `internal/nebula/engine.go`, `internal/constellations/runtime.go`,
  `internal/constellations/supervisor.go`.
- **Coordination + safety**: every shared substrate: the fabric DB, the
  entanglement lifecycle, the bus, the file_claims table (note dead in
  audit), the gitops perimeter. Reference `internal/fabric/`,
  `internal/gitops/`, `internal/bus/`.
- **Effectors**: how a star ultimately reaches an LLM and how a commit
  reaches the worktree. Reference `internal/claude/claude.go`,
  `internal/gitops/commit.go`.

End with three flow walkthroughs (numbered steps + file:line for each step):

1. **A user manually runs `quasar nebula apply <path>`** — what touches what,
   in order, through to commit and push.
2. **A sensor produces a draft nebula** — github sensor's Poll → Event →
   SeedNebula → nebulas table → fleet view shows it.
3. **An operator approves a nebula from the fleet view** — fleet.Approve →
   trigger_queue → Supervisor.Tick → RuntimeCacheFirer → Runtime.Fire →
   coder-reviewer constellation → commit → master-review → PR.

### `docs/glossary.md` — new

Alphabetical list. Each entry: one-sentence definition + the canonical
code location + a "see also" pointer to the doc that covers it in depth.

Minimum entries (add more as needed during writing):

- **Architect (star)** — the LLM agent that decomposes a seed nebula into
  phases. Lives in `internal/stars/defaults/architect-star.md`. See
  docs/sensors.md → "How a draft becomes a nebula".
- **Bus** — the typed publish-subscribe event channel decoupling event
  producers from consumers. `internal/bus/bus.go`. See docs/runtime.md.
- **Coder (star)** — the LLM agent that writes the diff for a single phase
  inside the coder-reviewer constellation.
- **Constellation** — a declarative workflow DAG written in TOML.
  `internal/artifacts/defaults/constellations/`. See docs/constellations.md.
- **Coder-reviewer constellation** — the inner loop that drives one phase to
  green build. See docs/constellations.md → "The default constellations".
- **Cycle** — one iteration of a back-edge in a constellation. `State.Cycle`
  in `internal/constellations/state.go`. See docs/runtime.md → "Back-edges
  and cycle counting".
- **Entanglement** — a producer's declared, in_flight, or terminal claim
  on a symbol that other phases coordinate around. `internal/fabric/
  entanglement_store.go`. See docs/entanglements.md.
- **Fabric** — the SQLite-backed persistence layer. `internal/fabric/`.
  See docs/fabric.md.
- **Fire** — the verb for "instantiate a constellation run." `Runtime.Fire`
  in `internal/constellations/runtime.go`. See docs/runtime.md.
- **Fleet view** — the multi-repo TUI showing awaiting-approval drafts,
  in-flight runs, and recent terminal nebulas. `internal/tui/fleet/`. See
  docs/multi-repo.md.
- **Master reviewer (star)** — the LLM agent that judges a completed
  nebula's diff and decides ship/fix/escalate.
- **Nebula** — a multi-phase task specification (a manifest plus one or
  more phase files). Maps to a row in `nebulas` table. See docs/glossary.md
  entries for phase, manifest, etc.
- **Phase** — one file inside a nebula, ultimately one row in `phases`
  table.
- **Reviewer (star)** — the LLM agent inside the coder-reviewer
  constellation that judges a single phase's diff.
- **Sensor** — a poll-driven adapter that turns external signals (GitHub
  issues, Slack mentions, cron ticks) into seed nebulas. `internal/sensors/`.
  See docs/sensors.md.
- **Skill** — a reusable markdown+TOML prompt fragment + tool grant that
  composes into a star. See docs/constellations.md.
- **Star** — an LLM agent character defined as markdown+TOML. See
  docs/constellations.md.
- **Step** — one node firing in a constellation run. `Runtime.Step`.
- **Supervisor** — the goroutine that drains `trigger_queue` and fires
  constellation runs in the fleet view's lifetime.
  `internal/constellations/supervisor.go`.
- **Trigger** — a row in `trigger_queue` that says "fire this constellation
  on this nebula." Inserted by fleet.Approve or by sensors.
- **Worktree** — the per-cycle git worktree the coder writes diffs into.

### `docs/README.md` — new (index)

The map of the docs/ directory. Organized into:

1. **Start here** — README.md (root), architecture.md, glossary.md.
2. **Core mechanics** — constellations.md, runtime.md, fabric.md.
3. **Coordination + safety** — entanglements.md, multi-repo.md, safety.md.
4. **Operations** — cli.md, gc.md, configuration.md.
5. **Extending Quasar** — sensors.md, sensor-authoring.md.
6. **Contributor handbook** — development.md, CLAUDE.md.
7. **Historical** — audit-2026-06-08.md (link with note: "snapshot of
   the codebase state on 2026-06-08; structural fixes from this audit have
   shipped"), constellation-runtime-followup.md (link with note).

Each entry is one line: `[title](path) — one-sentence summary`.

## Files

- `README.md` (rewrite) — top-level entry point
- `docs/README.md` (new) — docs index
- `docs/architecture.md` (new) — layered system overview + flow walkthroughs
- `docs/glossary.md` (new) — vocabulary index

## Acceptance Criteria

- [ ] `README.md` is current — no references to retired interfaces
  (`TicketSource`, `integrations` package); no claims about features not yet
  shipped; install + quick-start instructions work end-to-end against the
  current binary
- [ ] Every "Core concepts" entry in README.md links to its glossary entry
- [ ] `docs/architecture.md` includes the layered Mermaid diagram (renders
  in GitHub Markdown), prose for each layer with at least one file:line
  citation per layer, and all three flow walkthroughs
- [ ] All file:line citations resolve — the line numbers exist and the cited
  symbols are at those lines
- [ ] `docs/glossary.md` covers every term used elsewhere in this phase's
  output; "see also" pointers to docs that don't exist yet are explicitly
  marked "(planned in this nebula)" so they're not broken on initial commit
- [ ] `docs/README.md` lists every file under `docs/` (including audit-…
  and constellation-runtime-followup) with current/historical classification
- [ ] `bash scripts/lint.sh` exits 0 (no Go changes in this phase)
