+++
id = "channel-coordinator"
title = "Implement in-process Go channel Coordinator"
type = "feature"
priority = 2
depends_on = ["coordinator-interface"]
scope = ["internal/nebula/channel_coordinator.go"]
+++

## Problem

We need a concrete Coordinator implementation for single-machine use that requires zero infrastructure.

## Solution

Create `internal/nebula/channel_coordinator.go` implementing `Coordinator` with Go channels and sync primitives.

### Design

```go
// ChannelCoordinator implements Coordinator using Go channels for in-process use.
type ChannelCoordinator struct {
    queue       chan string           // Buffered work queue
    broadcast   chan ChangeEvent      // Fan-out broadcast
    subscribers []chan ChangeEvent    // Active subscriber channels
    locks       map[string]struct{}  // Currently locked paths
    mu          sync.Mutex
    done        chan struct{}
}

func NewChannelCoordinator(bufferSize int) *ChannelCoordinator
```

### Key behaviors

- **Enqueue/Dequeue**: Buffered channel. Dequeue blocks via select with ctx.Done().
- **Broadcast/Subscribe**: Fan-out pattern. Broadcast sends to all subscriber channels. Subscribe creates a new channel and registers it.
- **Lock**: Mutex-guarded map. If any requested path (or a prefix of it) is already locked, block on a condition variable until released. The unlock function removes the paths and broadcasts on the condition.
- **Close**: Close done channel, drain subscribers.

### Lock overlap detection

Lock should check for prefix-based overlap (reuse `scopesOverlap` logic from validate.go or extract to a shared utility). If a worker locks `internal/api/`, another worker requesting `internal/api/middleware/` should block.

## Files to Modify

- `internal/nebula/channel_coordinator.go` — New file: ChannelCoordinator implementation

## Acceptance Criteria

- [ ] Implements all Coordinator methods
- [ ] Enqueue/Dequeue work correctly with buffered channel
- [ ] Broadcast fans out to all subscribers
- [ ] Lock blocks when paths overlap, releases correctly
- [ ] Close is clean — no goroutine leaks
- [ ] Unit tests for each method
- [ ] `go test ./internal/nebula/...` passes
