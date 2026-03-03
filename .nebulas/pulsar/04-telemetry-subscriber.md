+++
id = "telemetry-subscriber"
title = "Telemetry emitter as a bus subscriber"
type = "task"
priority = 2
depends_on = ["bus-impl"]
scope = ["internal/telemetry/**"]
+++

## Problem

The telemetry system (`internal/telemetry/telemetry.go`) currently works via direct `Emitter.Emit()` calls from the metrics collector (`internal/nebula/metrics.go`). The `Metrics` struct holds a `*telemetry.Emitter` and calls `Emit()` at specific lifecycle points: epoch start/done, agent start/done, cycle start/done, entanglement posted, claim acquired/released, discovery posted/resolved, filter results, and healing events.

This means:
- Telemetry is tightly coupled to the metrics collector — adding telemetry from a new source requires modifying the collector.
- The existing `telemetry.Event` kind vocabulary (`KindEpochStart`, `KindAgentDone`, etc.) is separate from the bus event vocabulary, requiring manual mapping.
- There is no way to add telemetry for events that the metrics collector doesn't know about (e.g. gate prompts, refactors, hot adds).

With the bus, telemetry becomes a subscriber that receives all bus events and writes the relevant ones as JSONL.

## Solution

Create `internal/telemetry/bus_subscriber.go` containing a `BusSubscriber` that subscribes to the bus, maps bus events to `telemetry.Event` structs, and writes them via the existing `Emitter`.

### BusSubscriber struct

```go
// BusSubscriber subscribes to a bus and writes matching events to a
// telemetry JSONL file via an Emitter. It runs a background goroutine.
type BusSubscriber struct {
    emitter *Emitter
    sub     bus.Subscription
    epochID string
    done    chan struct{}
}

// NewBusSubscriber creates a telemetry subscriber. The epochID is included
// in every emitted event for correlation. Call Start to begin processing.
func NewBusSubscriber(emitter *Emitter, b bus.Bus, epochID string) *BusSubscriber {
    return &BusSubscriber{
        emitter: emitter,
        sub:     b.Subscribe("telemetry", 128),
        epochID: epochID,
        done:    make(chan struct{}),
    }
}
```

### Start/Stop lifecycle

```go
// Start begins the background event processing goroutine.
func (s *BusSubscriber) Start() {
    go s.run()
}

// Stop unsubscribes from the bus and waits for the processing goroutine
// to drain remaining events and exit.
func (s *BusSubscriber) Stop() {
    s.sub.Unsubscribe()
    <-s.done
}

func (s *BusSubscriber) run() {
    defer close(s.done)
    for ev := range s.sub.Events() {
        telEv, ok := s.mapEvent(ev)
        if !ok {
            continue
        }
        // Best-effort emit; telemetry should not block execution.
        _ = s.emitter.Emit(telEv)
    }
}
```

### Event mapping

Map bus events to the existing telemetry event kinds. Not all bus events need telemetry — only the ones that correspond to the existing `Kind*` constants plus new kinds for events that were previously unmapped.

```go
func (s *BusSubscriber) mapEvent(ev bus.Event) (Event, bool) {
    var kind string
    var data any

    switch ev.Kind {
    case bus.KindPhaseAgentStart, bus.KindAgentStart:
        kind = KindAgentStart
        data = map[string]any{
            "role":     ev.Role,
            "phase_id": ev.PhaseID,
        }
    case bus.KindPhaseAgentDone, bus.KindAgentDone:
        kind = KindAgentDone
        data = map[string]any{
            "role":        ev.Role,
            "cost_usd":    ev.CostUSD,
            "duration_ms": ev.DurationMs,
            "tokens":      ev.Tokens,
            "phase_id":    ev.PhaseID,
        }
    case bus.KindPhaseCycleStart, bus.KindCycleStart:
        kind = KindCycleStart
        data = map[string]any{
            "cycle":      ev.Cycle,
            "max_cycles": ev.MaxCycles,
            "phase_id":   ev.PhaseID,
        }
    case bus.KindPhaseCycleSummary, bus.KindCycleSummary:
        kind = KindCycleDone
        var summaryData any
        if ev.CycleSummary != nil {
            summaryData = map[string]any{
                "cycle":       ev.CycleSummary.Cycle,
                "cost_usd":    ev.CycleSummary.CostUSD,
                "approved":    ev.CycleSummary.Approved,
                "issue_count": ev.CycleSummary.IssueCount,
                "phase_id":    ev.PhaseID,
            }
        }
        data = summaryData
    case bus.KindPhaseTaskStarted, bus.KindTaskStarted:
        kind = KindEpochStart
        data = map[string]any{
            "bead_id":  ev.BeadID,
            "title":    ev.Title,
            "phase_id": ev.PhaseID,
        }
    case bus.KindPhaseTaskComplete, bus.KindTaskComplete:
        kind = KindEpochDone
        data = map[string]any{
            "bead_id":    ev.BeadID,
            "total_cost": ev.CostUSD,
            "phase_id":   ev.PhaseID,
        }
    case bus.KindEntanglementUpdate:
        kind = KindEntanglementPosted
        data = nil // entanglement data is opaque
    case bus.KindDiscoveryPosted:
        kind = KindDiscoveryPosted
        data = map[string]any{
            "phase_id": ev.PhaseID,
        }
    case bus.KindHealingAttempt:
        if ev.Healing == nil {
            return Event{}, false
        }
        kind = KindHealingStart
        data = map[string]any{
            "failed_phase_id":    ev.Healing.FailedPhaseID,
            "failure_kind":       ev.Healing.FailureKind,
            "remediation_id":     ev.Healing.RemediationID,
            "remediation_title":  ev.Healing.RemediationTitle,
        }
    case bus.KindNebulaDone:
        kind = KindEpochDone
        var errMsg string
        if ev.DoneResults != nil && ev.DoneResults.Err != nil {
            errMsg = ev.DoneResults.Err.Error()
        }
        data = map[string]any{
            "error": errMsg,
        }
    default:
        // Events without telemetry mapping are silently skipped.
        return Event{}, false
    }

    return Event{
        Timestamp: ev.Timestamp,
        Kind:      kind,
        EpochID:   s.epochID,
        TaskID:    ev.PhaseID,
        Data:      data,
    }, true
}
```

### Tests

Create `internal/telemetry/bus_subscriber_test.go`:

1. **TestBusSubscriberMapsAgentStart**: publish `KindPhaseAgentStart`, verify emitted event has `KindAgentStart` and correct data.
2. **TestBusSubscriberMapsAgentDone**: publish `KindPhaseAgentDone`, verify cost/duration/tokens in data.
3. **TestBusSubscriberMapsCycleSummary**: publish `KindPhaseCycleSummary` with payload, verify `KindCycleDone`.
4. **TestBusSubscriberSkipsUnmapped**: publish `KindPhaseInfo`, verify no event emitted.
5. **TestBusSubscriberStop**: verify Stop drains and exits cleanly.

Use a test helper that creates a `MemoryBus`, an `Emitter` writing to a temp file, and a `BusSubscriber`. After publishing and stopping, read the temp file and unmarshal JSONL lines.

## Files

- `internal/telemetry/bus_subscriber.go` — `BusSubscriber` struct, `NewBusSubscriber`, `Start`, `Stop`, `run`, `mapEvent`
- `internal/telemetry/bus_subscriber_test.go` — test suite

## Acceptance Criteria

- [ ] `go build ./internal/telemetry/...` compiles
- [ ] `go vet ./internal/telemetry/...` passes
- [ ] `go test ./internal/telemetry/...` passes with all new tests
- [ ] Bus subscriber maps `KindPhaseAgentStart` to `KindAgentStart`
- [ ] Bus subscriber maps `KindPhaseAgentDone` to `KindAgentDone` with cost/duration/tokens
- [ ] Bus subscriber maps `KindPhaseCycleStart` to `KindCycleStart`
- [ ] Bus subscriber maps `KindPhaseCycleSummary` to `KindCycleDone`
- [ ] Bus subscriber maps `KindPhaseTaskStarted` to `KindEpochStart`
- [ ] Bus subscriber maps `KindHealingAttempt` to `KindHealingStart`
- [ ] Events without a telemetry mapping are silently skipped (not errored)
- [ ] JSONL output is structurally identical to existing `Emitter.Emit` output
- [ ] `Stop()` drains remaining events before returning (no event loss)
- [ ] No data races under `go test -race ./internal/telemetry/...`
