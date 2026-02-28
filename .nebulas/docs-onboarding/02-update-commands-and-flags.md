+++
id = "update-commands-and-flags"
title = "Update commands table and flag tables to match CLI"
type = "task"
priority = 1
depends_on = []
scope = ["README.md"]
allow_scope_overlap = true
+++

## Problem

The README's commands table and flag tables are incomplete. Many commands and flags exist in the CLI but are not documented.

### Missing commands

The commands table lists 7 commands but the CLI has 20+. Missing:

| Command | Description |
|---------|-------------|
| `cockpit` | Interactive TUI home screen (auto-launched when `.nebulas/` exists) |
| `nebula status` | Display metrics and run history for a nebula |
| `fabric` | Coordination substrate commands (claim, release, read, diff, post, entanglements, archive, purge) |
| `discovery` | Post an agent discovery to fabric |
| `pulse emit` | Emit a pulse (note, decision, failure, reviewer_feedback) |
| `pulse list` | List pulses from fabric |
| `telemetry` | View JSONL telemetry events |
| `archive` | Top-level alias for `fabric archive` |

### Missing `run` flags

The `run` flags table is missing:

| Flag | Description | Default |
|------|-------------|---------|
| `--no-tui` | Disable TUI even on a TTY (use stderr printer) | false |
| `--no-splash` | Skip the startup splash animation | false |
| `--project-context` | Scan and inject project context into agent prompts | false |
| `--max-context-tokens N` | Token budget for injected context | 10000 |

### Missing `nebula apply` flags

Missing from the `nebula apply` flags table:

| Flag | Description | Default |
|------|-------------|---------|
| `--no-tui` | Disable TUI even on a TTY | false |
| `--no-splash` | Skip the startup splash animation | false |
| `--max-context-tokens N` | Token budget for injected context | 10000 |

### Missing `nebula plan` flags

The `nebula plan` command has flags not documented anywhere:

| Flag | Description | Default |
|------|-------------|---------|
| `--json` | Output the plan as JSON to stdout | false |
| `--save` | Save plan to `<nebula-dir>/<name>.plan.json` | false |
| `--diff` | Diff against a previously saved plan | false |
| `--no-color` | Disable ANSI colors in output | false |

## Solution

1. Update the top-level commands table to include all commands. Group them into sections: **Core**, **Nebula**, **Coordination** (fabric, discovery, pulse), **Observability** (telemetry). This keeps the table scannable without overwhelming a new user.

2. Update the `run` flags table to include all current flags.

3. Update the `nebula apply` flags table.

4. Add a `nebula plan` flags table (currently there's no flags table for `plan`).

5. For the `fabric`, `discovery`, `pulse`, and `telemetry` commands, don't add exhaustive flag tables in the README — they're advanced features. Just list them in the commands table with brief descriptions. Detailed docs can come later.

## Files

- `README.md` — update the Commands section, `run` Flags section, `nebula apply` Flags section, add `nebula plan` Flags

## Acceptance Criteria

- [ ] All CLI commands appear in the commands table
- [ ] `run` flags table includes `--no-tui`, `--no-splash`, `--project-context`, `--max-context-tokens`
- [ ] `nebula apply` flags table includes `--no-tui`, `--no-splash`, `--max-context-tokens`
- [ ] `nebula plan` flags are documented
- [ ] Advanced commands (fabric, discovery, pulse, telemetry) are listed but not over-documented
