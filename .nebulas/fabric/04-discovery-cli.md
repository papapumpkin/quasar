+++
id = "discovery-cli"
title = "quasar discovery CLI and hail mechanism"
type = "feature"
priority = 2
depends_on = ["schema-evolution"]
scope = ["cmd/discovery.go", "internal/fabric/discovery.go"]
+++

## Problem

Agents need a way to surface issues during execution — entanglement disputes, missing dependencies, file conflicts, requirements ambiguities, and budget alerts. These discoveries must be recorded in the fabric and, when appropriate, escalated as hails for human attention in the cockpit.

## Solution

### CLI subcommand

**`quasar discovery --kind <kind> --detail "<text>" [--affects <task_id>]`**

- Posts a `Discovery` to the fabric via `Fabric.PostDiscovery`
- `--kind` is required: one of `entanglement_dispute`, `missing_dependency`, `file_conflict`, `requirements_ambiguity`, `budget_alert`
- `--detail` is required: free-text explanation
- `--affects` is optional: specific task_id affected (NULL = broadcast to all)
- `--task` (or `QUASAR_TASK_ID` env): the source task posting the discovery
- `--db` (or `QUASAR_FABRIC_DB` env): fabric database path

On success, prints the discovery ID to stdout and exits 0.
On validation failure (bad kind, missing detail), prints error to stderr and exits 1.

### Hail mechanism

Not all discoveries are hails. Define the escalation rules in `internal/fabric/discovery.go`:

```go
// IsHail returns true if this discovery should surface as a human interrupt.
func (d Discovery) IsHail() bool {
    // Budget alerts are informational, not blocking
    return d.Kind != DiscoveryBudgetAlert
}
```

All discovery kinds except `budget_alert` pause the affected tasks and surface as hails. This aligns with the design principle: "only surface things where human judgment adds information the system doesn't have."

### Discovery helper functions

```go
// PendingHails returns unresolved discoveries that require human attention.
func PendingHails(ctx context.Context, f Fabric) ([]Discovery, error) {
    all, err := f.UnresolvedDiscoveries(ctx)
    if err != nil {
        return nil, err
    }
    var hails []Discovery
    for _, d := range all {
        if d.IsHail() {
            hails = append(hails, d)
        }
    }
    return hails, nil
}
```

### Integration with WorkerGroup

When a discovery is posted:
1. If `IsHail()`, the affected task transitions to `StateBlocked`
2. The discovery is emitted as a TUI message (for cockpit surfacing — wiring done in cockpit nebula)
3. The human resolves the hail → `ResolveDiscovery(id)` → blocked task re-evaluates

The WorkerGroup integration is lightweight here — just add a `checkDiscoveries` helper that Tycho (phase 7) will call during scheduling. For now, the CLI subcommand posts to the fabric and the rest is wired later.

## Files

- `cmd/discovery.go` — `quasar discovery` Cobra command with kind/detail/affects/task flags
- `internal/fabric/discovery.go` — `IsHail()` method, `PendingHails()` helper, discovery kind validation

## Acceptance Criteria

- [ ] `quasar discovery --kind entanglement_dispute --detail "..."` posts a discovery and prints its ID
- [ ] Invalid kinds are rejected with a clear error message
- [ ] Missing `--detail` is rejected
- [ ] `IsHail()` returns false only for `budget_alert`
- [ ] `PendingHails()` filters unresolved discoveries to hail-worthy ones
- [ ] Discovery kind constants are validated at the CLI layer
- [ ] `go build ./...` succeeds
- [ ] `go test ./internal/fabric/...` passes
- [ ] `go vet ./...` clean
