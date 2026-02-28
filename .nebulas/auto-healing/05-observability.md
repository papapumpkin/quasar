+++
id = "observability"
title = "Telemetry events, TUI messages, hails, and fabric state for healing lifecycle"
type = "feature"
priority = 2
depends_on = ["dag-insertion"]
labels = ["quasar", "auto-healing", "reliability"]
scope = ["internal/nebula/healing.go", "internal/nebula/worker.go", "internal/telemetry/telemetry.go", "internal/tui/msg.go", "internal/fabric/fabric.go"]
allow_scope_overlap = true
+++

## Problem

The healing pipeline introduces a new class of runtime events — failure diagnosis, architect invocation, DAG rewiring, remediation execution — that are invisible to operators. Without observability:

1. The TUI shows no indication that healing was attempted or why
2. Telemetry logs have no events for healing lifecycle, making post-mortem analysis impossible
3. Fabric state does not reflect the "healing" intermediate state, so external dashboards see a gap between "failed" and the remediation phase appearing
4. Hails are not raised when healing is attempted, so operators in `review` gate mode are unaware

## Solution

### Telemetry events

Add new event kinds to `internal/telemetry/telemetry.go`:

```go
const (
    KindHealingStart   = "healing.start"    // emitted when failure analysis begins
    KindHealingSkipped = "healing.skipped"  // emitted when diagnosis is unhealable or policy rejects
    KindHealingPlan    = "healing.plan"     // emitted when architect returns a remediation phase
    KindHealingInsert  = "healing.insert"   // emitted when remediation phase is hot-inserted into DAG
    KindHealingDone    = "healing.done"     // emitted when remediation phase completes (success or failure)
)
```

Each event's `Data` field carries structured context:

- `KindHealingStart`: `{"phase_id": "...", "failure_kind": "max_cycles", "cycles_used": 5, "budget_spent": 12.50}`
- `KindHealingSkipped`: `{"phase_id": "...", "reason": "unhealable" | "policy_disabled" | "max_attempts" | "no_budget"}`
- `KindHealingPlan`: `{"phase_id": "...", "remediation_id": "heal-...", "title": "...", "architect_cost_usd": 0.50}`
- `KindHealingInsert`: `{"phase_id": "...", "remediation_id": "heal-...", "rewired_dependents": ["phase-3", "phase-4"]}`
- `KindHealingDone`: `{"remediation_id": "heal-...", "success": true, "cost_usd": 3.20}`

Emit these via the existing `telemetry.Emitter.Emit` method from `WorkerGroup.attemptHealing` and the remediation phase's result handler.

### TUI messages

Add a new Bubble Tea message type in `internal/tui/msg.go`:

```go
// MsgHealingAttempt is sent when the auto-healing pipeline activates for a failed phase.
type MsgHealingAttempt struct {
    FailedPhaseID    string
    FailureKind      string
    RemediationID    string
    RemediationTitle string
}
```

The existing `MsgPhaseHotAdded` already handles display of hot-inserted phases. `MsgHealingAttempt` adds a distinct visual treatment — e.g., a warning-colored line like:

```
  ⚕  Healing phase-2 (max_cycles) → heal-phase-2: "Fix recursive type assertion"
```

Send `MsgHealingAttempt` from `WorkerGroup.attemptHealing` via the same `OnProgress` / TUI program channel used for other messages.

### Fabric state transitions

Add a new fabric state constant in `internal/fabric/fabric.go`:

```go
const StateHealing = "healing"
```

When healing begins for a phase, call:

```go
wg.Fabric.SetPhaseState(ctx, phaseID, fabric.StateHealing)
```

This signals to external dashboards and the fabric poller that the phase is in a transient remediation state, not terminally failed. When healing completes (remediation inserted), the original phase stays in `StateFailed` and the new remediation phase enters `StateQueued`.

### Hail for human awareness

In `review` gate mode, raise a hail when healing activates so the operator knows automated remediation is in progress:

```go
hail := loop.Hail{
    PhaseID:    phaseID,
    Kind:       loop.HailKindInfo, // or a new HailKindHealing
    Summary:    fmt.Sprintf("Auto-healing activated for %s (%s)", phaseID, diag.Kind),
    Detail:     diag.Summary + "\n\nRemediation phase: " + remediationSpec.ID,
    CreatedAt:  time.Now(),
}
```

Post via `wg.OnHail` callback (if set). The hail is informational — it does not block execution. In `approve` gate mode, the remediation phase will go through the normal gate approval before executing.

### Healing summary in nebula report

After all phases (including remediation phases) complete, include a healing summary section in the final nebula report. Add a helper:

```go
// HealingSummary returns a formatted report of all healing attempts during the run.
func HealingSummary(attempts map[string]int, results map[string]bool) string
```

This is called from the existing nebula completion reporting path and appended to the output.

## Files

- `internal/telemetry/telemetry.go` — add `KindHealingStart`, `KindHealingSkipped`, `KindHealingPlan`, `KindHealingInsert`, `KindHealingDone` constants
- `internal/tui/msg.go` — add `MsgHealingAttempt` struct
- `internal/fabric/fabric.go` — add `StateHealing` constant
- `internal/nebula/healing.go` — add `HealingSummary` function; emit telemetry events from `attemptHealing` call sites
- `internal/nebula/worker.go` — emit TUI messages, hails, and telemetry at each healing lifecycle point; call `Fabric.SetPhaseState` with `StateHealing`
- `internal/nebula/healing_test.go` — test `HealingSummary` formatting; verify telemetry event kinds are emitted (mock emitter)

## Acceptance Criteria

- [ ] All five `KindHealing*` telemetry events are emitted at the correct lifecycle points
- [ ] `MsgHealingAttempt` TUI message is sent and contains the failed phase ID, failure kind, and remediation title
- [ ] `fabric.StateHealing` is set on the failed phase before architect invocation
- [ ] A hail is posted when healing activates (informational, non-blocking)
- [ ] `HealingSummary` produces a readable report listing each healed phase, attempt count, and outcome
- [ ] Healing-skipped events include the specific reason (unhealable, policy, budget, max attempts)
- [ ] `go test ./internal/nebula/...` and `go test ./internal/telemetry/...` pass
- [ ] `go vet ./...` clean
