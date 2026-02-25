<div align="center">
<pre>
                      .·::··::··::·.                    .·::··::··::·.
                  .::··::::::::::::··::.            .::··::::::::::::··::.
              .::··::::.    ··::||::··  ··::    ::··  ··::||::··    .::::··::.
           .::··:::.   ..··::::\||/::··    ··::··    ··::\||/::::··..   .:::··::.
        .::··:::.  ..··::::..   \||/  ..::··  ··::..  \||/   ..::::··..  .:::··::.
      .::··::. ..··::::..  ··::. \|/ .::··::····::··::. \|/ .::··  ..::::··.. .::··::.
    .::··::. .··::::. .··::.. ·:. | .:·::··:.  .::··::·. | .:· ..::··. .::::··. .::··::.
   .::·::. .··:::.  ··::.. ··::. | .::··  ::··::  ··::. | .::·· ..::··  .:::··. .::·::.
  .::·::. .··:::.  ··::. .··::.. | ..::··. .::··. .::··.. | ..::··. .::··  .:::··. .::·::.
  ::·::. .··:::.  ··::..··::. .  | . .::··:..::··:..::··. | . .::··..::··  .:::··. .::·::
  :·::..··:::.  ··::..··::..··:. | .::··..::····::..::··. | .:·..::··..::··  .:::··..::·:
  :·::.··:::. ··::..··::. ··::.. | ..::··. .:·::·:. .::··.. | ..::·· .::··..::·· .:::··.::·:
  :·:.··:::. ··::..··::..··::..--<b>-@-</b>--..::··..::::..::··..--<b>-@-</b>--..::··..::··..::·· .:::··.:·:
  :·::.··:::. ··::..··::. ··::.. | ..::··. .:·::·:. .::··.. | ..::·· .::··..::·· .:::··.::·:
  :·::..··:::.  ··::..··::..··:. | .::··..::····::..::··. | .:·..::··..::··  .:::··..::·:
  ::·::. .··:::.  ··::..··::. .  | . .::··:..::··:..::··. | . .::··..::··  .:::··. .::·::
  .::·::. .··:::.  ··::. .··::.. | ..::··. .::··. .::··.. | ..::··. .::··  .:::··. .::·::.
   .::·::. .··:::.  ··::.. ··::. | .::··  ::··::  ··::. | .::·· ..::··  .:::··. .::·::.
    .::··::. .··::::. .··::.. ·:. | .:·::··:.  .::··::·. | .:· ..::··. .::::··. .::··::.
      .::··::. ..··::::..  ··::. /|\ .::··::····::··::. /|\ .::··  ..::::··.. .::··::.
        .::··:::.  ..··::::..   /||\  ..::··  ··::..  /||\   ..::::··..  .:::··::.
           .::··:::.   ..··::::/||\ ::··    ··::··    ··::/||\::::··..   .:::··::.
              .::··::::.    ··::||::··  ··::    ::··  ··::||::··    .::::··::.
                  .::··::::::::::::··::.            .::··::::::::::::··::.
                      .·::··::··::·.                    .·::··::··::·.

                                   Q    U    A    S    A    R
</pre>
</div>

# Quasar

Dual-agent AI coding coordinator that cycles a coder and reviewer until the reviewer approves.

## What It Does

Quasar coordinates two AI agents — a **coder** and a **reviewer** — that iterate on a coding task in a loop. The coder implements the requested changes, then the reviewer reads the actual source files to verify correctness, security, and code quality. If the reviewer finds issues, they're sent back to the coder for another pass. Each task is tracked as a [Beads](https://github.com/aaronsalm/beads) issue, with review findings recorded as child issues.

## Prerequisites

- [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) (`claude`) — must be installed and authenticated
- [Beads CLI](https://github.com/aaronsalm/beads) (`beads`) — for task/issue tracking
- [Go](https://go.dev/) 1.25+ — to build from source

## Install

```bash
git clone https://github.com/papapumpkin/quasar.git
cd quasar
go build -o quasar .
```

Optionally install to your `$GOPATH/bin`:

```bash
go install .
```

## Quick Start

First, verify that all dependencies are installed:

```bash
quasar validate
```

### Single Task (REPL Mode)

1. Start the interactive REPL:

   ```bash
   quasar run
   ```

2. Type a task at the `quasar>` prompt — e.g., "Add input validation to the login handler."

### Multi-Task (Nebula Mode)

1. Create a nebula directory with a `nebula.toml` manifest and one `.md` file per task (see [Nebula Blueprints](#nebula-blueprints) below for the full format):

   ```
   .nebulas/my-nebula/
   ├── nebula.toml
   ├── implement-feature.md
   └── write-tests.md
   ```

2. Validate the nebula:

   ```bash
   quasar nebula validate .nebulas/my-nebula/
   ```

3. Apply and run:

   ```bash
   quasar nebula apply .nebulas/my-nebula/ --auto
   ```

4. Or launch the cockpit TUI to manage nebulas interactively:

   ```bash
   quasar cockpit
   ```

Running `quasar` with no subcommand in a directory containing `.nebulas/` auto-launches the cockpit.

## Commands

### Core

| Command              | Description                                    |
|----------------------|------------------------------------------------|
| `run`                | Start the interactive coder-reviewer REPL      |
| `cockpit`            | Launch the interactive TUI home screen         |
| `validate`           | Check that `claude` and `beads` CLIs are found |
| `version`            | Print the version number                       |

Running `quasar` with no subcommand auto-launches the cockpit when a `.nebulas/` directory exists in the working directory.

### Nebula

| Command              | Description                                      |
|----------------------|--------------------------------------------------|
| `nebula validate`    | Validate a nebula blueprint directory             |
| `nebula plan`        | Preview the execution plan for a nebula           |
| `nebula apply`       | Create/update beads and optionally run workers    |
| `nebula show`        | Display current nebula state                      |
| `nebula status`      | Display metrics and run history for a nebula      |

### Coordination (Fabric)

These commands interact with the shared coordination database used by concurrent agents. See individual `--help` output for full flag details.

| Command                 | Description                                         |
|-------------------------|-----------------------------------------------------|
| `fabric claim`          | Acquire exclusive ownership of a file path          |
| `fabric release`        | Release file claims (single file or all)            |
| `fabric read`           | Read fabric state for the current task              |
| `fabric post`           | Post entanglements (exported interfaces) to fabric  |
| `fabric diff`           | Show changes since last fabric snapshot             |
| `fabric entanglements`  | List all entanglements across phases                |
| `fabric archive`        | Snapshot and purge fabric state for an epoch        |
| `fabric purge`          | Discard all fabric state without archiving          |
| `discovery`             | Post an agent discovery (conflict, ambiguity, etc.) |
| `pulse emit`            | Emit a pulse (note, decision, failure, feedback)    |
| `pulse list`            | List pulses from the fabric                         |
| `archive`               | Top-level alias for `fabric archive`                |

### Observability

| Command              | Description                                       |
|----------------------|---------------------------------------------------|
| `telemetry`          | View JSONL telemetry events for a nebula epoch    |

### `run` Flags

| Flag                      | Description                                          | Default        |
|---------------------------|------------------------------------------------------|----------------|
| `--max-cycles N`          | Maximum coder-reviewer cycles                        | 3              |
| `--max-budget N`          | Maximum total spend in USD                           | 5.00           |
| `--coder-prompt-file F`   | File containing a custom coder system prompt         | (built-in)     |
| `--reviewer-prompt-file F`| File containing a custom reviewer system prompt      | (built-in)     |
| `--auto`                  | Run a single task non-interactively and exit         | false          |
| `--no-tui`                | Disable TUI even on a TTY (use stderr printer)       | false          |
| `--no-splash`             | Skip the startup splash animation                    | false          |
| `--project-context`       | Scan and inject project context into agent prompts   | false          |
| `--max-context-tokens N`  | Token budget for injected context                    | 10000          |
| `-v, --verbose`           | Show debug output (CLI commands, versions)           | false          |
| `--config FILE`           | Path to config file                                  | `.quasar.yaml` |

### Interactive Commands

Inside the `quasar>` REPL:

| Input            | Action                         |
|------------------|--------------------------------|
| *(any text)*     | Start a coder-reviewer cycle   |
| `help`           | Show available commands        |
| `status`         | Show current config settings   |
| `quit` / `exit`  | Exit Quasar                    |

## Cockpit (TUI)

The cockpit is a terminal dashboard for browsing and running nebulas interactively. It auto-discovers nebula directories under `.nebulas/`, shows phase tables with live status, cost tracking, and cycle progress, and lets you drill down into individual phase timelines and agent output.

### Launching

```bash
quasar cockpit             # explicit launch
quasar                     # auto-launches when .nebulas/ exists in the cwd
quasar cockpit --dir path  # scan a different directory
```

### Navigation

| Key              | Action                                         |
|------------------|-------------------------------------------------|
| `Tab`            | Cycle through tabs (phases, graph, board, log)  |
| `Enter`          | Drill down into a phase or cycle                |
| `Esc`            | Back up one level                               |
| `j/k` or arrows  | Move selection up/down                          |
| `d`              | Toggle diff view for the selected phase         |
| `p`              | Pause/resume execution                          |
| `s`              | Stop workers gracefully                         |
| `q`              | Quit                                            |

### Flags

| Flag              | Description                              |
|-------------------|------------------------------------------|
| `--dir`           | Directory to scan for `.nebulas/`        |
| `--no-splash`     | Skip the startup splash animation        |
| `--max-workers N` | Maximum concurrent workers               |
| `--no-tui`        | Disable TUI (use plain stderr output)    |

Pass `--no-tui` to `nebula apply` when running in CI or piping output.

## Configuration

Create a `.quasar.yaml` in your project root (or home directory):

```yaml
# Path to CLI binaries
claude_path: claude
beads_path: beads

# Working directory for agent invocations
work_dir: "."

# Safety limits
max_review_cycles: 3
max_budget_usd: 5.0

# Claude model (empty = CLI default)
model: ""

# Custom system prompts (inline)
coder_system_prompt: ""
reviewer_system_prompt: ""

# Lint commands run after each coder pass
lint_commands:
  - "go vet ./..."
  - "go fmt ./..."

# Debug output
verbose: false
```

### Config Precedence

Settings are resolved in this order (highest priority first):

1. **CLI flags** — `--max-cycles`, `--max-budget`, `--verbose`, etc.
2. **Environment variables** — prefixed with `QUASAR_` (e.g., `QUASAR_MAX_BUDGET_USD=10`)
3. **Config file** — `.quasar.yaml` in the current directory or home directory
4. **Defaults** — built-in values shown above

### Runtime Directory

Quasar creates a `.quasar/` directory in your project root for runtime state. This includes:

- `fabric.db` — coordination database used during multi-task (nebula) execution
- `neutrons/` — archived fabric snapshots
- `telemetry/` — execution telemetry logs

This directory is created automatically when needed. You may want to add `.quasar/` to your `.gitignore`.

## How It Works

```
You type a task
       │
       ▼
┌─────────────┐
│ Create bead │  (task tracked in Beads)
└──────┬──────┘
       │
       ▼
┌─────────────┐     ┌──────────────┐
│   Coder     │────▶│   Reviewer   │
│ (claude -p) │     │  (claude -p) │
└─────────────┘     └──────┬───────┘
       ▲                   │
       │            ┌──────┴──────┐
       │            │  APPROVED?  │
       │            └──────┬──────┘
       │           yes/         \no
       │           ▼             ▼
       │     ┌──────────┐  ┌──────────────┐
       │     │   Done   │  │ Parse issues │
       │     │(close    │  │ (create child│
       │     │ bead)    │  │  beads)      │
       │     └──────────┘  └──────┬───────┘
       │                          │
       └──────────────────────────┘
              next cycle
```

**Step by step:**

1. **Create bead** — a Beads issue is created for the task
2. **Coder runs** — `claude -p` with the task (or previous findings) as input
3. **Reviewer runs** — `claude -p` reads the actual source files to verify the coder's work
4. **Parse review** — if `APPROVED:`, the bead is closed; if `ISSUE:` blocks are found, child beads are created and the coder gets another pass
5. **Repeat** — up to `max_review_cycles` times or until budget is exhausted

Each agent gets a per-invocation budget of `max_budget_usd / (2 * max_review_cycles)`.

## Project Structure

```
cmd/              CLI commands (Cobra): run, validate, version, nebula, fabric, discovery, pulse, telemetry, tui
internal/
  agent/          Agent types, roles, and the Invoker interface
  ansi/           ANSI escape code constants for terminal styling
  beads/          Beads CLI wrapper (Client interface + CLI impl)
  claude/         Claude CLI invoker (satisfies agent.Invoker)
  config/         Viper-based config loading (.quasar.yaml / env QUASAR_*)
  dag/            Directed acyclic graph engine (topological sort, cycle detection)
  fabric/         SQLite coordination store (entanglements, claims, discoveries, pulses)
  filter/         Deterministic pre-reviewer checks (build, vet, lint, test)
  loop/           Core coder-reviewer loop and state machine
  nebula/         Multi-task orchestration (parse, validate, plan, apply)
  neutron/        Epoch archival and stale state cleanup for fabrics
  snapshot/       Project snapshot scanner for prompt context injection
  telemetry/      JSONL event stream for state transitions
  tui/            BubbleTea interactive terminal UI (cockpit dashboard)
  tycho/          DAG scheduler for nebula orchestration
  ui/             Stderr-based UI printer (ANSI colors)
```

## Safety

Quasar has two built-in safeguards to prevent runaway costs:

- **Max cycles** (`--max-cycles`, default 3) — the loop stops after this many coder-reviewer rounds, even if issues remain
- **Budget cap** (`--max-budget`, default $5.00) — total spend across all agent invocations is tracked and the loop stops if the cap is reached

Both can be configured per-run via flags, environment variables, or `.quasar.yaml`.

## Custom Prompts

Override the built-in system prompts by pointing to text files:

```bash
quasar run --coder-prompt-file ./prompts/coder.txt --reviewer-prompt-file ./prompts/reviewer.txt
```

Or set them inline in `.quasar.yaml`:

```yaml
coder_system_prompt: "You are a Go expert. Follow the project's error handling patterns..."
reviewer_system_prompt: "Focus on security and test coverage..."
```

File-based prompts (`--*-prompt-file` flags) take precedence over config values.

## Auto Mode

Run a single task non-interactively — useful for scripting and CI:

```bash
# Pass task as argument
quasar run --auto "Add rate limiting to the API endpoint"

# Or pipe from stdin
echo "Fix the nil pointer in handler.go" | quasar run --auto
```

The process exits with code 0 on approval, non-zero on failure or max cycles reached.

## Nebula Blueprints

Nebula is a structured multi-task blueprint system inspired by OpenTofu's plan/apply lifecycle. Define a set of related tasks in a directory, and Quasar will create beads, resolve dependencies, and execute them with the coder-reviewer loop.

### File Format

A nebula is a directory containing:

- **`nebula.toml`** — Manifest with project name, description, and default settings
- **`*.md`** — One file per task, with TOML frontmatter between `+++` delimiters
- **`nebula.state.toml`** — Auto-generated execution state (created by `nebula apply`)

```
my-nebula/
├── nebula.toml
├── add-auth.md
├── write-tests.md
└── update-docs.md
```

**`nebula.toml`:**

```toml
[nebula]
name = "auth-feature"
description = "Add authentication to the API"

[defaults]
type = "task"
priority = 2
labels = ["auth"]

[execution]
max_workers = 2           # Concurrent workers for this nebula
max_review_cycles = 3     # Default review cycles per task
max_budget_usd = 5.0      # Default per-task budget
model = ""                # Model override (empty = use global config)

[context]
repo = "github.com/example/myproject"
working_dir = "."
goals = [
    "Add authentication to all API endpoints",
    "Ensure all new code has tests",
]
constraints = [
    "Do not break existing public API contracts",
    "Use JWT, not session-based auth",
]

[dependencies]
requires_beads = []        # Bead IDs that must be closed before apply
requires_nebulae = []      # Other nebula names that must be fully done
```

**Task file (`add-auth.md`):**

```
+++
id = "add-auth"
title = "Add JWT authentication"
type = "feature"
priority = 1
depends_on = []
max_review_cycles = 5     # Override: more iterations for complex work
max_budget_usd = 10.0     # Override: higher budget
model = "claude-opus-4-6" # Override: use a specific model
+++

Implement JWT-based authentication for all API endpoints...
```

### Frontmatter Fields

| Field                 | Required | Description                                              |
|-----------------------|----------|----------------------------------------------------------|
| `id`                  | yes      | Unique identifier within the nebula                      |
| `title`               | yes      | Short description                                        |
| `type`                | no       | `task`, `bug`, `feature` (inherits from `[defaults]`)    |
| `priority`            | no       | Integer, 1=highest (inherits from `[defaults]`)          |
| `depends_on`          | no       | Array of phase IDs this phase depends on                 |
| `labels`              | no       | Array of string labels                                   |
| `assignee`            | no       | Assignee override                                        |
| `max_review_cycles`   | no       | Override per-phase cycle limit                           |
| `max_budget_usd`      | no       | Override per-phase budget                                |
| `model`               | no       | Override model for this phase                            |
| `gate`                | no       | Override gate mode for this phase                        |
| `blocks`              | no       | Reverse deps: inject as dependency of listed phases      |
| `scope`               | no       | Glob patterns for owned files/dirs                       |
| `allow_scope_overlap` | no       | Permit scope overlap with other phases                   |

### Config Cascade (Nebula)

Execution settings are resolved per-task with the following precedence (highest wins, zero/empty values are skipped):

1. **CLI flags** — `--max-workers`, `--max-cycles`, etc.
2. **Task frontmatter** — `max_review_cycles`, `max_budget_usd`, `model`, `gate` in `+++` block
3. **Nebula `[execution]`** — defaults for all tasks in this nebula
4. **Global config** — `.quasar.yaml` / `QUASAR_*` env
5. **Built-in defaults** — cycles=3, budget=$5.00

### Nebula Context

The `[context]` section provides project-level information that is automatically injected into coder and reviewer prompts. Goals and constraints help agents understand the project's intent without repeating context in every task file.

### External Dependencies

The `[dependencies]` section declares prerequisites that must be met before `nebula apply` will proceed:

- **`requires_beads`** — list of bead IDs that must be in `closed` status
- **`requires_nebulae`** — list of other nebula names whose state files must show all tasks done

### CLI Commands

| Command                      | Description                                      |
|------------------------------|--------------------------------------------------|
| `nebula validate <path>`     | Validate structure, frontmatter, and dependencies |
| `nebula plan <path>`         | Preview the execution plan for a nebula          |
| `nebula apply <path>`        | Create/update beads from the blueprint           |
| `nebula show <path>`         | Display current nebula state                     |
| `nebula status <path>`       | Display metrics and run history                  |

### `nebula plan` Flags

| Flag             | Description                                        | Default |
|------------------|----------------------------------------------------|---------|
| `--json`         | Output the plan as JSON to stdout                  | false   |
| `--save`         | Save the plan to `<nebula-dir>/<name>.plan.json`   | false   |
| `--diff`         | Diff against a previously saved plan               | false   |
| `--no-color`     | Disable ANSI colors in output                      | false   |

### `nebula apply` Flags

| Flag                    | Description                                                  | Default |
|-------------------------|--------------------------------------------------------------|---------|
| `--auto`                | Start workers to execute ready tasks after applying          | false   |
| `--watch`               | Watch for task file changes during execution (with `--auto`) | false   |
| `--max-workers N`       | Maximum concurrent workers (with `--auto`)                   | 1       |
| `--no-tui`              | Disable TUI even on a TTY (use stderr output)                | false   |
| `--no-splash`           | Skip the startup splash animation                            | false   |
| `--max-context-tokens N`| Token budget for injected context                            | 10000   |

### In-Flight Editing

When `--auto --watch` is enabled, Quasar monitors the nebula directory for task file changes using `fsnotify`. If you edit a task's `.md` file while its worker is running:

1. The worker detects the change and pauses
2. A checkpoint prompt captures the coder's current progress
3. The updated task description is loaded
4. The worker resumes with a resumption prompt that includes both the checkpoint and the new task body

This allows you to refine task descriptions mid-execution without losing work.

### Reviewer Reports

After reviewing each task, the reviewer generates a structured report alongside the `APPROVED:` or `ISSUE:` blocks:

```
REPORT:
SATISFACTION: high|medium|low
RISK: high|medium|low
NEEDS_HUMAN_REVIEW: yes|no
SUMMARY: One-sentence assessment of the work.
```

Reports are stored in the nebula state file and posted as bead comments. Use `nebula show` to view reports for completed tasks.

### State File

When `nebula apply` runs, Quasar writes a `nebula.state.toml` file inside the nebula directory. This file tracks the execution state of each phase — its status, associated bead ID, cost, and reviewer reports. Both `nebula show` and `nebula status` read from this file to display current progress. The state file is updated as phases complete and should not be edited by hand.

### Example

The `examples/dogfood-nebula/` directory contains a working nebula that tests Quasar on its own codebase:

```bash
quasar nebula validate examples/dogfood-nebula/
quasar nebula plan examples/dogfood-nebula/
quasar nebula apply examples/dogfood-nebula/ --auto --max-workers 2
```

### Jet (Future)

Jet is the planned temporal orchestration layer for running nebula tasks at scale — named after the focused, directed relativistic outflows of a quasar. It will support distributed execution via Temporal workflows with Kubernetes deployment. Not yet implemented.

## Review Format

The reviewer's output must end with one of:

**Approval:**
```
APPROVED: Changes look correct and follow project conventions.
```

**Issues (one or more blocks):**
```
ISSUE:
SEVERITY: critical
DESCRIPTION: SQL query uses string concatenation instead of parameterized queries.

ISSUE:
SEVERITY: minor
DESCRIPTION: Missing error check on file close.
```

Severity levels: `critical`, `major`, `minor`. If omitted, defaults to `major`.

**Report (always included after APPROVED or ISSUE blocks):**
```
REPORT:
SATISFACTION: high
RISK: low
NEEDS_HUMAN_REVIEW: no
SUMMARY: Clean implementation with proper error handling.
```

## License

MIT
