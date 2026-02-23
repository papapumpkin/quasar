+++
id = "pulse-cli"
title = "Pulse system — shared execution context"
type = "feature"
priority = 2
depends_on = ["schema-evolution"]
scope = ["cmd/pulse.go", "internal/fabric/pulse.go"]
+++

## Problem

When quasars run concurrently, they make observations and decisions that downstream or parallel tasks need to know about — "this function has a subtle nil case on empty slices", "switched to cursor-based pagination because offset was too slow", "reviewer flagged that this interface needs context.Context on all methods." This context currently vanishes after each agent invocation.

The fabric has a `beads` table (from the schema-evolution phase) intended for this purpose, but the name "beads" collides with the existing Dolt-based issue tracker (`bd` CLI). The canonical name for this concept is **pulse** — quasars emit pulses into the fabric, and the fabric propagates them to other quasars as shared execution context.

## Solution

### Rename and type

Rename the Go types from `Bead` to `Pulse`. The SQLite table can be migrated from `beads` to `pulses` with `ALTER TABLE beads RENAME TO pulses`.

```go
// Pulse is a structured context emission from a quasar during execution.
// Pulses propagate through the fabric so concurrent and downstream quasars
// share execution context without direct communication.
type Pulse struct {
    ID        int64
    TaskID    string
    Content   string
    Kind      string    // note, decision, failure, reviewer_feedback
    CreatedAt time.Time
}

const (
    PulseNote             = "note"
    PulseDecision         = "decision"
    PulseFailure          = "failure"
    PulseReviewerFeedback = "reviewer_feedback"
)
```

### Fabric interface update

Rename the methods on the `Fabric` interface:

```go
    // Pulses — shared execution context
    EmitPulse(ctx context.Context, p Pulse) error
    PulsesFor(ctx context.Context, taskID string) ([]Pulse, error)
```

Update `SQLiteFabric` to use the renamed table and methods. The existing `AddBead`/`BeadsFor` implementations become `EmitPulse`/`PulsesFor` — same SQL, different names and table.

### FabricSnapshot update

Add pulses to the snapshot so they can be injected into agent context:

```go
type FabricSnapshot struct {
    Entanglements         []Entanglement
    Claims                map[string]string
    Completed             []string
    InProgress            []string
    UnresolvedDiscoveries []Discovery
    Pulses                []Pulse  // from completed/in-progress upstream tasks
}
```

Update `RenderSnapshot` to include a "Shared Context (Pulses)" section when pulses are present:

```
## Shared Context

[parse-config] decision: switched to cursor-based pagination because offset perf degrades past 10k rows
[auth-middleware] note: ValidateToken needs context.Context — reviewer feedback from cycle 2
```

### CLI subcommands

**`quasar pulse emit --kind <kind> "<content>"`**
- Emits a pulse to the fabric via `Fabric.EmitPulse`
- `--kind` required: `note`, `decision`, `failure`, `reviewer_feedback`
- Content is the positional argument
- `--task` (or `QUASAR_TASK_ID` env): source task
- `--db` (or `QUASAR_FABRIC_DB` env): fabric database path
- Prints pulse ID to stdout on success

**`quasar pulse list [--task <task_id>]`**
- Lists pulses, optionally filtered by task
- Output format:
  ```
  [14:32:01] decision (parse-config)
  switched to cursor-based pagination because offset perf degrades past 10k rows

  [14:35:22] note (auth-middleware)
  ValidateToken needs context.Context — reviewer feedback from cycle 2
  ```

### Telemetry integration

When a pulse is emitted, the telemetry emitter records a `pulse_emitted` event. This flows through the telemetry bridge into the cockpit scratchpad automatically (no extra wiring needed — the cockpit-wiring phase handles telemetry→scratchpad).

## Files

- `internal/fabric/pulse.go` — `Pulse` type, constants, `Fabric` interface method renames
- `internal/fabric/sqlite.go` — Rename table, update method implementations
- `internal/fabric/snapshot.go` — Add `Pulses` to `FabricSnapshot`, update `RenderSnapshot`
- `cmd/pulse.go` — `quasar pulse` command group with `emit` and `list` subcommands

## Acceptance Criteria

- [ ] SQLite table renamed from `beads` to `pulses`
- [ ] `Pulse` type replaces `Bead` throughout `internal/fabric`
- [ ] `EmitPulse` / `PulsesFor` replace `AddBead` / `BeadsFor` on the Fabric interface
- [ ] `FabricSnapshot` includes pulses from upstream tasks
- [ ] `RenderSnapshot` renders pulses as shared context
- [ ] `quasar pulse emit --kind decision "..."` stores a pulse and prints its ID
- [ ] `quasar pulse list --task <id>` shows pulses for a task
- [ ] Invalid pulse kinds are rejected
- [ ] `go build ./...` succeeds
- [ ] `go test ./internal/fabric/...` passes
- [ ] `go vet ./...` clean
