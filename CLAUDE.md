# Quasar — Development Guidelines

## Build & Test

```bash
go build -o quasar .          # build binary
go test ./...                  # run all tests
go test ./internal/loop/...    # run loop tests only
go vet ./...                   # static analysis
```

## Project Structure

```
cmd/          CLI commands (Cobra). Each file = one command.
internal/
  agent/      Agent types, roles, and the Invoker interface
  beads/      Beads CLI wrapper (Client interface + CLI impl)
  claude/     Claude CLI invoker (satisfies agent.Invoker)
  config/     Viper-based config loading (.quasar.yaml / env QUASAR_*)
  loop/       Core coder-reviewer loop and state machine
  nebula/     Multi-task orchestration (parse, validate, plan, apply)
  ui/         Stderr-based UI printer (ANSI colors)
```

## Go Conventions

### Interfaces & Dependencies
- Define interfaces where they are consumed, not where they are implemented.
- `Loop.Invoker` is `agent.Invoker`; `Loop.Beads` is `beads.Client`. Follow this pattern.
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
- Mock interfaces for unit tests. Follow `beads.Client` mock pattern.
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
gate = ""                 # gate mode: trust | review | approve | watch

[context]
repo = "github.com/papapumpkin/quasar"
working_dir = "."
goals = ["Goal 1", "Goal 2"]
constraints = ["Constraint 1"]

[dependencies]
requires_beads = []    # bead IDs that must be closed first
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
| `gate` | no | Override gate mode for this phase |
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
