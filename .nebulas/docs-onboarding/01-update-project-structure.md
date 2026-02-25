+++
id = "update-project-structure"
title = "Update project structure listing in README"
type = "task"
priority = 1
depends_on = []
scope = ["README.md"]
allow_scope_overlap = true
+++

## Problem

The README's project structure section lists 7 internal packages but the codebase now has 17. Ten packages are completely missing from the listing:

**Currently documented:**
```
internal/
  agent/      Agent types, roles, and the Invoker interface
  beads/      Beads CLI wrapper (Client interface + CLI impl)
  claude/     Claude CLI invoker (satisfies agent.Invoker)
  config/     Viper-based config loading (.quasar.yaml / env QUASAR_*)
  loop/       Core coder-reviewer loop and state machine
  nebula/     Multi-task orchestration (parse, validate, plan, apply)
  ui/         Stderr-based UI printer (ANSI colors)
```

**Missing packages:**
- `ansi/` — ANSI escape code constants for terminal styling
- `dag/` — Directed acyclic graph engine (topological sort, cycle detection, wave scheduling)
- `fabric/` — SQLite coordination substrate (entanglements, claims, discoveries, pulses)
- `filter/` — Pre-reviewer deterministic checks (build, vet, lint, test, claims)
- `neutron/` — Epoch archival (standalone SQLite snapshots of fabric state)
- `snapshot/` — Project snapshot scanner for prompt context injection
- `telemetry/` — JSONL event stream for state transitions
- `tui/` — BubbleTea interactive terminal UI (cockpit dashboard)
- `tycho/` — DAG scheduler for nebula orchestration (extracted from WorkerGroup)

Also missing from the listing: `cmd/` descriptions are sparse — just says "CLI commands (Cobra). Each file = one command" but there are now commands for `fabric`, `discovery`, `pulse`, `telemetry`, and `cockpit` in addition to the original `run`, `validate`, `version`, and `nebula` subcommands.

The `board/` package exists as a legacy stub (renamed to `fabric`) and should not be listed.

## Solution

Replace the project structure block in the README with the full current listing. Keep descriptions to one line each, matching the style of the existing entries. Group logically.

## Files

- `README.md` — update the project structure code block

## Acceptance Criteria

- [ ] All 16 active internal packages are listed (exclude `board/` which is a legacy stub)
- [ ] Each entry has a concise one-line description matching existing style
- [ ] The `cmd/` description is updated to reflect the broader command set
- [ ] Packages are listed in alphabetical order
