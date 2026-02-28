+++
id = "fabric-cli"
title = "quasar fabric CLI subcommands"
type = "feature"
priority = 1
depends_on = ["schema-evolution"]
scope = ["cmd/fabric.go", "cmd/fabric_read.go", "cmd/fabric_entanglements.go", "cmd/fabric_post.go", "cmd/fabric_claim.go", "cmd/fabric_diff.go"]
+++

## Problem

Quasars (worker agents) need a CLI interface to interact with the fabric. They should never touch SQLite directly. The design specifies a `quasar fabric` command group with subcommands for reading state, posting entanglements, managing claims, and viewing diffs.

## Solution

Create a Cobra command group `quasar fabric` with subcommands. All subcommands open the fabric SQLite database from the working directory (default: `.quasar/fabric.db`, configurable via `--db` flag or `QUASAR_FABRIC_DB` env).

### Subcommands

**`quasar fabric read`** — Full fabric state as structured text.
- Calls `AllEntanglements`, `AllDiscoveries`, file claims, task states
- Renders via `RenderSnapshot` (extended version showing everything)
- Output to stdout (machine-consumable by agents)

**`quasar fabric entanglements [--task <id>]`** — Entanglements relevant to a specific task.
- If `--task` given: calls `EntanglementsFor(taskID)`
- Otherwise: calls `AllEntanglements()`
- Renders grouped by producer, showing status (pending/fulfilled/disputed)

**`quasar fabric post --from-file <path> --exports`** — Extract and post exported interfaces from Go source.
- Uses the existing `Publisher.extractGoSymbols` logic
- Reads the file, parses with `go/parser`, extracts exported symbols
- Posts each as an entanglement with producer = current task (from `--task` flag or `QUASAR_TASK_ID` env)

**`quasar fabric post --interface "<signature>"`** — Manually declare an interface entanglement.
- Creates a single entanglement with the given signature string
- Requires `--task` for producer ID

**`quasar fabric claim --file <path>`** — Acquire a file claim.
- Calls `ClaimFile(filepath, taskID)`
- Exits 0 on success, exits 1 with error message if already claimed (showing current owner)

**`quasar fabric release --file <path>`** — Release a file claim.
- Calls `ReleaseClaims` for the specific file
- Or release all claims for a task with `--all --task <id>`

**`quasar fabric diff --since <timestamp>`** — Show fabric changes since a timestamp.
- Queries entanglements and discoveries created/updated after the given timestamp
- Useful for agents to see what changed while they were working

**`quasar fabric archive --epoch <id> --output <path>`** — Snapshot and purge (placeholder for neutron phase).
- This subcommand is registered but implementation is deferred to the neutron-archival phase
- Prints "not yet implemented" for now

**`quasar fabric purge --epoch <id>`** — Discard state for abandoned epoch (placeholder).
- Same — registered, deferred

### Common flags

All subcommands accept:
- `--db <path>` — fabric database path (default `.quasar/fabric.db`)
- `--task <id>` — current task ID (also reads `QUASAR_TASK_ID` env)

Register flags with Viper for env var binding: `QUASAR_FABRIC_DB`, `QUASAR_TASK_ID`.

### Output format

All output goes to stdout in structured text suitable for LLM consumption. Use the same formatting conventions as `RenderSnapshot` — clear section headers, indentation, no ANSI colors (agents read plain text).

## Files

- `cmd/fabric.go` — Root `quasar fabric` command group
- `cmd/fabric_read.go` — `read` subcommand
- `cmd/fabric_entanglements.go` — `entanglements` subcommand
- `cmd/fabric_post.go` — `post` subcommand (both `--from-file` and `--interface` modes)
- `cmd/fabric_claim.go` — `claim` and `release` subcommands
- `cmd/fabric_diff.go` — `diff` subcommand


Note: `archive` and `purge` subcommands are registered as placeholders in `cmd/fabric.go` but their full implementation is deferred to the neutron-archival phase (`cmd/fabric_archive.go`).

## Acceptance Criteria

- [ ] `quasar fabric read` outputs full fabric state
- [ ] `quasar fabric entanglements` lists entanglements, optionally filtered by task
- [ ] `quasar fabric post --from-file` extracts Go exports and posts as entanglements
- [ ] `quasar fabric post --interface` posts a manual entanglement declaration
- [ ] `quasar fabric claim --file` acquires a claim, fails if already claimed
- [ ] `quasar fabric release --file` releases a specific claim
- [ ] `quasar fabric diff --since` shows recent fabric changes
- [ ] `--db` and `--task` flags work, with Viper env var binding
- [ ] Output is plain text, no ANSI — suitable for agent consumption
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` clean
