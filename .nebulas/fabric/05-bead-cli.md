+++
id = "bead-cli"
title = "Migrate beads to fabric SQLite with CLI"
type = "feature"
priority = 2
depends_on = ["schema-evolution"]
scope = ["cmd/bead.go"]
+++

## Problem

Beads (agent working memory) currently live in Dolt via the external beads CLI. The design calls for migrating bead storage into the fabric SQLite database so all coordination state lives in one place. Agents need `quasar bead add` and `quasar bead list` subcommands.

## Solution

### CLI subcommands

**`quasar bead add --kind <kind> "<content>"`**

- Posts a `Bead` to the fabric via `Fabric.AddBead`
- `--kind` is required: one of `note`, `decision`, `failure`, `reviewer_feedback`
- Content is the positional argument (the quoted string)
- `--task` (or `QUASAR_TASK_ID` env): the task posting the bead
- `--db` (or `QUASAR_FABRIC_DB` env): fabric database path

On success, prints the bead ID to stdout and exits 0.

**`quasar bead list [--task <task_id>]`**

- Lists beads, optionally filtered by task ID
- If `--task` given: calls `BeadsFor(taskID)`
- Otherwise: lists beads for the current task (from `QUASAR_TASK_ID` env)
- Output format — one bead per block:
  ```
  [2024-01-15 14:32:01] decision
  switched approach because the interface type was too broad

  [2024-01-15 14:35:22] note
  important: this function has a subtle nil case on empty slices
  ```
- Timestamps in UTC, kind on the header line, content below

### Integration notes

This phase only builds the CLI and fabric storage. The existing `internal/beads` package (Dolt-based `beads.Client`) continues to work for the loop's task tracking (bead create/close/comment for issue management). The fabric beads are a separate concept — agent scratch memory, not issue tracking.

The two systems coexist:
- `beads.Client` — issue/task lifecycle (create issue, close issue, add comment)
- `Fabric.AddBead/BeadsFor` — agent working memory (notes, decisions, failures, reviewer feedback)

No migration of existing Dolt data is needed. The fabric beads start empty for each epoch.

## Files

- `cmd/bead.go` — `quasar bead` command group with `add` and `list` subcommands

## Acceptance Criteria

- [ ] `quasar bead add --kind decision "reasoning..."` stores a bead and prints its ID
- [ ] `quasar bead list --task <id>` shows beads for a specific task
- [ ] Invalid bead kinds are rejected with a clear error
- [ ] Output format is human-readable with timestamps
- [ ] Existing `beads.Client` (Dolt) is unaffected — no changes to `internal/beads/`
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` clean
