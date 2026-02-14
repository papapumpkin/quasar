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
git clone https://github.com/aaronsalm/quasar.git
cd quasar
go build -o quasar .
```

Optionally install to your `$GOPATH/bin`:

```bash
go install .
```

## Quick Start

1. Verify dependencies are available:

   ```bash
   quasar validate
   ```

2. Start the interactive REPL:

   ```bash
   quasar run
   ```

3. Type a task at the `quasar>` prompt — e.g., "Add input validation to the login handler."

## Commands

| Command    | Description                                    |
|------------|------------------------------------------------|
| `run`      | Start the interactive coder-reviewer REPL      |
| `validate` | Check that `claude` and `beads` CLIs are found |
| `version`  | Print the version number                       |

### `run` Flags

| Flag                     | Description                                 | Default        |
|--------------------------|---------------------------------------------|----------------|
| `--max-cycles N`         | Maximum coder-reviewer cycles               | 3              |
| `--max-budget N`         | Maximum total spend in USD                  | 5.00           |
| `--coder-prompt-file F`  | File containing a custom coder system prompt| (built-in)     |
| `--reviewer-prompt-file F`| File containing a custom reviewer system prompt| (built-in)  |
| `--auto`                 | Run a single task non-interactively and exit | false          |
| `-v, --verbose`          | Show debug output (CLI commands, versions)  | false          |
| `--config FILE`          | Path to config file                         | `.quasar.yaml` |

### Interactive Commands

Inside the `quasar>` REPL:

| Input            | Action                         |
|------------------|--------------------------------|
| *(any text)*     | Start a coder-reviewer cycle   |
| `help`           | Show available commands        |
| `status`         | Show current config settings   |
| `quit` / `exit`  | Exit Quasar                    |

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

# Debug output
verbose: false
```

### Config Precedence

Settings are resolved in this order (highest priority first):

1. **CLI flags** — `--max-cycles`, `--max-budget`, `--verbose`, etc.
2. **Environment variables** — prefixed with `QUASAR_` (e.g., `QUASAR_MAX_BUDGET_USD=10`)
3. **Config file** — `.quasar.yaml` in the current directory or home directory
4. **Defaults** — built-in values shown above

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

## License

MIT
