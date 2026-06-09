# Quasar — Development Guidelines

## Build & Test

```bash
go build -o quasar .          # build binary
go test ./...                  # run all tests
go test ./internal/loop/...    # run loop tests only
go vet ./...                   # static analysis
quasar init                    # scaffold a .quasar.yaml (auto-detects language + GitHub remote)
quasar doctor                  # diagnose config, integrations, credentials, git, pre-commit
```

`[pre_commit]` in `.quasar.yaml` lists formatter/linter commands (gofmt, prettier,
ruff format, cargo fmt, …) run in the worktree before every Quasar commit.

## Project Structure

```
cmd/          CLI commands (Cobra). Each file = one command.
internal/
  agent/         Agent types, roles, and the Invoker interface
  claude/        Claude CLI invoker (satisfies agent.Invoker)
  config/        Viper-based config loading (.quasar.yaml / env QUASAR_*)
  sensors/       Poll-driven external integrations (Sensor interface, replacing
                 the legacy TicketSource); github/ adapter; reach adapters only via
                 sensors.Default()
  gitops/        Vanilla-git output safety perimeter; every git write is routed here
  artifacts/     Embedded default stars/skills/constellations + per-repo override loader
  constellations/ Constellation engine: Step/Fire walk, operators, budget + cycle
                 guard, and nested-constellation dispatch (master-review calls
                 coder-reviewer as a real inner loop via NodeConstellation)
  fabric/        SQLite persistence (constellation_runs, star_invocations, nebulas)
  loop/          Legacy in-process coder-reviewer loop (still backs `quasar run`
                 and its tests). Superseded by the coder-reviewer constellation now
                 that NodeConstellation dispatches; deleting it + collapsing into a
                 thin internal/runner/ shim is the remaining follow-up.
  nebula/        Multi-task orchestration (parse, validate, plan, apply)
  ui/            Stderr-based UI printer (ANSI colors)
```

The coder-reviewer pair runs as a declarative constellation
(`internal/artifacts/defaults/constellations/coder-reviewer.toml`): the coder
writes a diff, the runtime commits it, the reviewer judges it (parsed by the
`reviewer_decision` builtin), and a back-edge revises up to `[meta].max_cycles`
times before the run fails. The inner cycle cap is enforced by the same runtime
back-edge counter as every other loop — no Go constant, no special-casing.

The outer master-review constellation
(`internal/artifacts/defaults/constellations/master-review.toml`) wraps the
above as a nested constellation: a `fix` verdict dispatches the coder-reviewer
constellation synchronously via `NodeConstellation`, and a back-edge to
`review` re-judges the freshly-applied fix. `meta.max_cycles` (default 3)
bounds the outer loop. This wiring landed in the 2026-06-08 audit; the
previous PLACEHOLDER routed within-cap fixes to `_awaiting_human`.

## Sensors & Safety

- **`Sensor`** (poll-driven) interface lives in `internal/sensors/`. Adapters
  register from `init()` and are looked up by name via `sensors.Default()` —
  never imported directly by the cmd layer. (The legacy `TicketSource`
  interface and `internal/integrations/` package were retired in the
  rename-integrations-to-sensors phase.)
- **Output safety perimeter**: all git *writes* are confined to the
  `internal/gitops/` wrapper (vanilla git), which only permits pushes to
  `quasar/*` refs and rejects destructive ops. `gh` is confined to the GitHub
  sensor for issue reading only. See [docs/safety.md](docs/safety.md).
- **Adding a sensor** (e.g. Linear, Slack, cron): follow the walkthrough in
  [docs/authoring-a-sensor.md](docs/authoring-a-sensor.md) when it ships
  (planned in `additional-sensors`); meantime, model after
  `internal/sensors/github/`.
- **Secrets** are never inlined in `.quasar.yaml`: use `token_env` or
  `token_file`. An inline `token:` is a config-load error. The
  `<key>_ssm` form is reserved for the SSM resolver shipped by
  `deployment-cookbook`.

## Go Conventions

### Interfaces & Dependencies
- Define interfaces where they are consumed, not where they are implemented.
- `Loop.Invoker` is `agent.Invoker`. Follow this pattern (interface defined where consumed).
- Use constructor functions to inject dependencies. No global mutable state.
- Prefer small, purpose-specific interfaces (1-3 methods) over large ones.

### Error Handling
- Always handle errors explicitly. Never use `_ = expr` for error returns.
- Non-fatal errors (bead comments, status updates) should be logged, not discarded.
- Use wrapped errors with context: `fmt.Errorf("failed to create bead: %w", err)`.
- Sentinel errors as package-level vars: `var ErrMaxCycles = errors.New("max cycles reached")`.

### Functions
- Keep functions short and focused (~20 lines). Extract helpers for distinct phases.
- One function, one responsibility.
- Use `strings.Builder` for multi-part string construction (already done in prompt builders).

### Testing
- Use stdlib `testing` only. No external test frameworks.
- Table-driven tests with `t.Run` for subtests. Use `t.Parallel()` where safe.
- Use `strings.Contains` from stdlib, not custom helpers.
- Mock interfaces for unit tests.
- Name test functions `TestFunctionName` with subtests via `t.Run("case name", ...)`.

### Output & UI
- All human-readable output goes to **stderr** via `ui.Printer`.
- **stdout** is reserved for structured/machine-readable data only (e.g., `version` command).
- Use `fmt.Fprintf(os.Stderr, ...)` for debug/verbose output.

### Context Propagation
- All methods that do I/O (subprocess calls, network) must accept `context.Context` as first parameter.
- Propagate context through the call chain for cancellation support.
- Use `exec.CommandContext(ctx, ...)` for subprocess execution.

### Documentation
- Every exported type, interface, function, and method gets a GoDoc comment.
- Follow `// Name does X.` convention.
- Don't document the obvious — focus on non-trivial behavior and design decisions.

### Style
- Run `go fmt` and `go vet` before committing.
- Prefer stdlib over third-party libraries where feasible.
- Sentinel values and constants at top of file, types next, then functions.
- Group imports: stdlib, then external, then internal (enforced by `goimports`).

## Config Precedence

1. CLI flags (highest)
2. Environment variables (`QUASAR_*`)
3. `.quasar.yaml` config file
4. Built-in defaults (lowest)

## Nebula Authoring

When prompted to write a nebula, do not write any code unless explicitly instructed to. Only produce the nebula manifest and phase files.

Nebulas are multi-phase task specifications in `.nebulas/<name>/`. Each nebula has a manifest (`nebula.toml`) and one or more phase files (`*.md`).

### Manifest (`nebula.toml`)

```toml
[nebula]
name = "my-nebula"
description = "What this nebula accomplishes"

[defaults]
type = "task"        # default phase type: task | bug | feature
priority = 2         # default priority (1=highest)
labels = ["quasar"]  # default labels applied to phases
assignee = ""        # default assignee

[execution]
max_workers = 1           # concurrent workers
max_review_cycles = 5     # max coder-reviewer cycles per phase
max_budget_usd = 50.0     # budget cap
model = ""                # model override (empty = default)
# gate = "trust" (default and only mode going forward — the manual review/
# approve/watch modes are being removed from the codebase; never author with
# a non-trust gate)

[context]
repo = "github.com/papapumpkin/quasar"
working_dir = "."
goals = ["Goal 1", "Goal 2"]
constraints = ["Constraint 1"]

[dependencies]
requires_beads = []    # legacy field — beads has been removed; ignored if present
requires_nebulae = []  # nebula names that must complete first
```

### Phase Files (`*.md`)

Each phase file **must** start with `+++` TOML frontmatter delimiters:

```markdown
+++
id = "phase-id"
title = "Human-readable title"
type = "task"
priority = 2
depends_on = ["other-phase-id"]
+++

## Problem

Description of what needs to change and why.

## Solution

How to solve it, including code snippets if useful.

## Files

- `path/to/file.go` — what to do

## Acceptance Criteria

- [ ] Criterion 1
- [ ] Criterion 2
```

**Frontmatter fields** (between `+++` delimiters):

| Field | Required | Description |
|-------|----------|-------------|
| `id` | yes | Unique identifier within the nebula |
| `title` | yes | Short description |
| `type` | no | `task`, `bug`, `feature` (inherits from `[defaults]`) |
| `priority` | no | Integer, 1=highest (inherits from `[defaults]`) |
| `depends_on` | no | Array of phase IDs this phase depends on |
| `labels` | no | Array of string labels |
| `assignee` | no | Assignee override |
| `max_review_cycles` | no | Override per-phase cycle limit |
| `max_budget_usd` | no | Override per-phase budget |
| `model` | no | Override model for this phase |
| `blocks` | no | Reverse deps: inject as dependency of listed phases |
| `scope` | no | Glob patterns for owned files/dirs |
| `allow_scope_overlap` | no | Permit scope overlap with other phases |

### Validation

```bash
./quasar nebula validate .nebulas/my-nebula    # check for errors
./quasar nebula apply .nebulas/my-nebula --auto # run all phases
```

## Git

- GitHub org is `papapumpkin`
- Commit messages: imperative mood, concise summary
