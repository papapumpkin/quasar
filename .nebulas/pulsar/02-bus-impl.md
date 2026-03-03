+++
id = "bus-impl"
title = "Channel-based bus implementation with fan-out and backpressure"
type = "task"
priority = 2
depends_on = ["bus-interface"]
scope = ["internal/bus/**"]
+++

## Problem

The bus interface defined in phase `bus-interface` needs a concrete implementation. The implementation must support:

- **Fan-out**: each published event is delivered to every active subscriber.
- **Backpressure**: if a subscriber's buffer fills up (e.g. a slow TUI), `Publish` blocks rather than dropping events. This prevents event loss during bursts from parallel workers.
- **Per-subscriber buffering**: each subscriber has an independent channel with configurable depth, isolating fast consumers from slow ones.
- **Concurrent publishers**: multiple worker goroutines publish simultaneously; the bus must be goroutine-safe.
- **Graceful shutdown**: `Close()` drains remaining events, closes all subscriber channels, and subsequent `Publish` calls return `ErrBusClosed`.

No external dependencies — use stdlib channels and `sync` primitives only.

## Solution

Create `internal/bus/memory.go` with a channel-based `MemoryBus` implementation.

### Core structure

```go
// MemoryBus is a channel-based Bus implementation with fan-out delivery
// and per-subscriber backpressure. Safe for concurrent use.
type MemoryBus struct {
    mu      sync.RWMutex
    subs    map[uint64]*memorySub
    nextID  uint64
    closed  bool
}

type memorySub struct {
    name   string
    ch     chan Event
    id     uint64
    bus    *MemoryBus
    once   sync.Once
}

const defaultBufSize = 64
```

### NewMemoryBus constructor

```go
// NewMemoryBus creates a new in-process event bus.
func NewMemoryBus() *MemoryBus {
    return &MemoryBus{
        subs: make(map[uint64]*memorySub),
    }
}
```

### Publish — fan-out with backpressure

```go
func (b *MemoryBus) Publish(ctx context.Context, ev Event) error {
    b.mu.RLock()
    if b.closed {
        b.mu.RUnlock()
        return ErrBusClosed
    }
    // Snapshot subscribers under read lock.
    targets := make([]*memorySub, 0, len(b.subs))
    for _, s := range b.subs {
        targets = append(targets, s)
    }
    b.mu.RUnlock()

    // Deliver to each subscriber. If a subscriber's buffer is full,
    // block until space is available or the context is cancelled.
    for _, s := range targets {
        select {
        case s.ch <- ev:
        case <-ctx.Done():
            return ctx.Err()
        }
    }
    return nil
}
```

The key design: the read lock is held only for the subscriber snapshot, not for the channel sends. This means subscribers can be added/removed concurrently with publishing, and slow subscribers block only the publisher, not other subscribers (each gets its own channel).

### Subscribe — per-subscriber buffering

```go
func (b *MemoryBus) Subscribe(name string, bufSize int) Subscription {
    if bufSize <= 0 {
        bufSize = defaultBufSize
    }

    b.mu.Lock()
    defer b.mu.Unlock()

    id := b.nextID
    b.nextID++

    s := &memorySub{
        name: name,
        ch:   make(chan Event, bufSize),
        id:   id,
        bus:  b,
    }
    b.subs[id] = s
    return s
}
```

### Subscription methods

```go
func (s *memorySub) Events() <-chan Event {
    return s.ch
}

func (s *memorySub) Unsubscribe() {
    s.once.Do(func() {
        s.bus.mu.Lock()
        delete(s.bus.subs, s.id)
        s.bus.mu.Unlock()
        // Drain any remaining events to unblock publishers.
        go func() {
            for range s.ch {
            }
        }()
    })
}
```

### Close — graceful shutdown

```go
func (b *MemoryBus) Close() error {
    b.mu.Lock()
    defer b.mu.Unlock()
    if b.closed {
        return nil
    }
    b.closed = true
    for _, s := range b.subs {
        close(s.ch)
    }
    b.subs = nil
    return nil
}
```

### Tests

Create `internal/bus/memory_test.go` with table-driven tests:

1. **TestPublishSubscribe**: subscribe, publish one event, receive it, verify Kind and fields.
2. **TestFanOut**: two subscribers, publish one event, both receive it.
3. **TestBackpressure**: subscriber with bufSize=1, publish 2 events without reading, verify second Publish blocks until read.
4. **TestUnsubscribe**: subscriber unsubscribes, subsequent publishes do not block.
5. **TestClosedBus**: Close(), then Publish returns `ErrBusClosed`.
6. **TestSubscribeAfterClose**: Subscribe after Close returns a subscription whose channel is immediately closeable.
7. **TestConcurrentPublish**: spawn 10 goroutines each publishing 100 events, verify all received by a single subscriber.
8. **TestContextCancellation**: cancelled context causes Publish to return context error when subscriber is full.

## Files

- `internal/bus/memory.go` — `MemoryBus` struct, `memorySub` struct, `NewMemoryBus`, `Publish`, `Subscribe`, `Close`, `Events`, `Unsubscribe`
- `internal/bus/memory_test.go` — comprehensive test suite covering fan-out, backpressure, unsubscribe, close, concurrency, context cancellation

## Acceptance Criteria

- [ ] `go test ./internal/bus/...` passes with all 8+ test cases
- [ ] `go vet ./internal/bus/...` passes
- [ ] `MemoryBus` implements `Bus` interface (compile-time check via `var _ Bus = (*MemoryBus)(nil)`)
- [ ] `memorySub` implements `Subscription` interface (compile-time check)
- [ ] Publish blocks when a subscriber's buffer is full (backpressure, not drop)
- [ ] Publish respects context cancellation when blocked
- [ ] Close shuts down all subscriber channels
- [ ] Unsubscribe removes the subscriber without blocking other subscribers
- [ ] No data races under `go test -race ./internal/bus/...`
- [ ] No external dependencies beyond stdlib
