package bus

import (
	"context"
	"sync"
)

// Compile-time interface checks.
var (
	_ Bus          = (*MemoryBus)(nil)
	_ Subscription = (*memorySub)(nil)
)

// defaultBufSize is the per-subscriber channel buffer depth used when
// Subscribe is called with bufSize <= 0.
const defaultBufSize = 64

// MemoryBus is a channel-based Bus implementation with fan-out delivery
// and per-subscriber backpressure. Safe for concurrent use.
type MemoryBus struct {
	mu     sync.RWMutex
	subs   map[uint64]*memorySub
	nextID uint64
	closed bool
}

// memorySub is a single subscriber attached to a MemoryBus.
type memorySub struct {
	name string
	ch   chan Event
	id   uint64
	bus  *MemoryBus
	once sync.Once
}

// NewMemoryBus creates a new in-process event bus.
func NewMemoryBus() *MemoryBus {
	return &MemoryBus{
		subs: make(map[uint64]*memorySub),
	}
}

// Publish sends ev to every active subscriber. If any subscriber's buffer
// is full, Publish blocks until space is available or ctx is cancelled.
// Returns ErrBusClosed if the bus has been closed.
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

// Subscribe returns a new Subscription that receives all events published
// after this call. bufSize controls the per-subscriber channel depth; a
// value <= 0 uses defaultBufSize.
func (b *MemoryBus) Subscribe(name string, bufSize int) Subscription {
	if bufSize <= 0 {
		bufSize = defaultBufSize
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		// Return a subscription with a closed channel so callers can
		// range-loop without blocking.
		ch := make(chan Event)
		close(ch)
		return &memorySub{name: name, ch: ch, bus: b}
	}

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

// Close shuts down the bus, closing all subscriber channels. Subsequent
// Publish calls return ErrBusClosed. Close is idempotent.
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

// Events returns the receive-only channel carrying events for this subscriber.
func (s *memorySub) Events() <-chan Event {
	return s.ch
}

// Unsubscribe detaches this subscriber from the bus and drains any buffered
// events to unblock pending publishers.
func (s *memorySub) Unsubscribe() {
	s.once.Do(func() {
		s.bus.mu.Lock()
		delete(s.bus.subs, s.id)
		s.bus.mu.Unlock()
		// Drain remaining events so any blocked Publish calls can proceed.
		go func() {
			for range s.ch {
			}
		}()
	})
}
