+++
id = "telemetry"
title = "Emit telemetry events for routing decisions"
type = "feature"
priority = 3
depends_on = ["resolve-integration"]
+++

## Problem

When auto-routing is active, operators need visibility into which model each phase was routed to and why. Without telemetry, debugging unexpected cost or quality outcomes requires reading code. The existing `internal/telemetry` package emits structured JSONL events for agent lifecycle and task state -- routing decisions should be recorded the same way.

## Solution

### New Event Kind

Add a new telemetry event kind constant in `internal/telemetry/telemetry.go`:

```go
KindModelRouted = "model_routed"
```

### Event Data Structure

Define the data payload for routing events:

```go
// ModelRoutedData is the Data payload for KindModelRouted events.
type ModelRoutedData struct {
    PhaseID         string             `json:"phase_id"`
    TierName        string             `json:"tier"`
    Model           string             `json:"model"`
    ComplexityScore float64            `json:"complexity_score"`
    Signals         map[string]float64 `json:"signals"` // raw signal values
    Contributions   map[string]float64 `json:"contributions"` // weighted contributions
    Overridden      bool               `json:"overridden"` // true if an explicit override was present
}
```

When an explicit model override is present (phase or nebula level), still emit the event with `Overridden: true` and the score that *would* have been used. This gives operators full visibility into what auto-routing would have done.

### Emission Point

In `internal/nebula/worker_exec.go`, after calling `ResolveExecution`, emit the routing event if the `RoutingContext` is non-nil and routing is enabled:

```go
if rc != nil && rc.Routing.Enabled && wg.Metrics != nil && wg.Metrics.Telemetry != nil {
    signals := BuildComplexitySignals(phase, rc.DAG)
    result := ScoreComplexity(signals)
    wg.Metrics.Telemetry.Emit(telemetry.Event{
        Kind:   telemetry.KindModelRouted,
        TaskID: phaseID,
        Data: telemetry.ModelRoutedData{
            PhaseID:         phaseID,
            TierName:        exec.RoutedTier,
            Model:           exec.Model,
            ComplexityScore: result.Score,
            Signals:         signalsToMap(result.Signals),
            Contributions:   result.Contributions,
            Overridden:      exec.RoutedTier == "" && exec.Model != "",
        },
    })
}
```

### Helper

```go
// signalsToMap converts ComplexitySignals to a map for telemetry serialization.
func signalsToMap(s ComplexitySignals) map[string]float64 {
    return map[string]float64{
        "scope_count":  float64(s.ScopeCount),
        "body_length":  float64(s.BodyLength),
        "depth_count":  float64(s.DepthCount),
        "type_weight":  typeWeight(s.TaskType),
    }
}
```

### Telemetry Access

The `WorkerGroup` already has a `Metrics *Metrics` field. Verify that `Metrics` exposes (or can expose) the `*telemetry.Emitter`. If `Metrics` does not currently hold an emitter reference, add one:

```go
// In internal/nebula/metrics.go or wherever Metrics is defined
type Metrics struct {
    // ... existing fields ...
    Telemetry *telemetry.Emitter // nil = no telemetry
}
```

## Files

- `internal/telemetry/telemetry.go` -- add `KindModelRouted` constant and `ModelRoutedData` struct
- `internal/nebula/worker_exec.go` -- emit `KindModelRouted` event after `ResolveExecution`
- `internal/nebula/complexity.go` -- add `signalsToMap` helper (or export the fields directly)
- `internal/nebula/metrics.go` -- add `Telemetry` field to `Metrics` if not present
- `internal/telemetry/telemetry_test.go` -- test that `ModelRoutedData` serializes to the expected JSON shape

## Acceptance Criteria

- [ ] `KindModelRouted` events appear in the JSONL telemetry stream for every phase when routing is enabled
- [ ] The event includes the complexity score, all signal values, contributions, tier name, and model
- [ ] When an explicit override is present, `overridden` is `true` and the would-have-been score is still recorded
- [ ] A nil `*telemetry.Emitter` does not panic (nil-safe `Emit`)
- [ ] `go test ./internal/telemetry/...` and `go test ./internal/nebula/...` pass
