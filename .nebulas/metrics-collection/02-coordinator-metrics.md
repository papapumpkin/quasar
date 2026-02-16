+++
id = "coordinator-metrics"
title = "Instrument ChannelCoordinator with metrics hooks"
type = "feature"
priority = 2
depends_on = ["metrics-types"]
scope = ["internal/nebula/channel_coordinator.go"]
+++

## Problem

The ChannelCoordinator handles lock acquisition, broadcast, and queue operations but records nothing about their performance. Lock wait times and conflict counts are critical signals for the adaptive concurrency controller.

## Solution

Add an optional `*Metrics` field to `ChannelCoordinator`. When non-nil, record:

### Lock metrics
- Time between `Lock()` call and lock acquisition → `RecordLockWait(phaseID, duration)`
- Lock contention events (when a lock blocks because paths overlap with an existing lock)

### Broadcast metrics
- Count of broadcast events per phase
- Subscriber notification latency (time to fan out to all subscribers)

### Queue metrics
- Queue depth at enqueue/dequeue time
- Wait time in dequeue (time between phase becoming ready and worker picking it up)

### Implementation approach

Wrap the existing Lock/Broadcast/Enqueue/Dequeue methods with timing instrumentation. The Metrics pointer is checked before each record call — nil means no-op, zero overhead.

```go
type ChannelCoordinator struct {
    // ... existing fields ...
    Metrics *Metrics // optional, nil = no collection
}
```

This does NOT change the Coordinator interface — metrics are an implementation concern of ChannelCoordinator, not a contract requirement.

## Files to Modify

- `internal/nebula/channel_coordinator.go` — Add Metrics field, instrument Lock/Broadcast/Enqueue/Dequeue

## Acceptance Criteria

- [ ] Lock wait time recorded when Metrics is non-nil
- [ ] No behavioral change when Metrics is nil
- [ ] No additional allocations on the hot path when Metrics is nil
- [ ] `go test ./internal/nebula/...` passes (existing tests unaffected)
- [ ] `go vet ./...` passes
