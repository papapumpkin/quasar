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
	mu       sync.RWMutex
	subs     map[uint64]*memorySub
	detached []*memorySub // unsubscribed subs awaiting channel close
	nextID   uint64
	closed   bool
	done     chan struct{}  // closed on Close to unblock in-flight publishes
	inflight sync.WaitGroup // tracks in-flight Publish calls
}

// memorySub is a single subscriber attached to a MemoryBus.
type memorySub struct {
	name  string
	ch    chan Event
	id    uint64
	bus   *MemoryBus
	once  sync.Once
	unsub chan struct{} // closed on Unsubscribe to unblock Publish
}

// NewMemoryBus creates a new in-process event bus.
func NewMemoryBus() *MemoryBus {
	return &MemoryBus{
		subs: make(map[uint64]*memorySub),
		done: make(chan struct{}),
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
	// Register this publish as in-flight while still holding the read lock.
	// This guarantees Close (which needs the write lock) cannot proceed to
	// close channels until we call Done.
	b.inflight.Add(1)
	// Snapshot subscribers under read lock.
	targets := make([]*memorySub, 0, len(b.subs))
	for _, s := range b.subs {
		targets = append(targets, s)
	}
	b.mu.RUnlock()

	defer b.inflight.Done()

	// Deliver to each subscriber. If a subscriber's buffer is full,
	// block until space is available, the subscriber unsubscribes,
	// the context is cancelled, or the bus is closed.
	for _, s := range targets {
		select {
		case s.ch <- ev:
		case <-s.unsub:
			continue
		case <-ctx.Done():
			return ctx.Err()
		case <-b.done:
			return ErrBusClosed
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
		return &memorySub{name: name, ch: ch, bus: b, unsub: make(chan struct{})}
	}

	id := b.nextID
	b.nextID++

	s := &memorySub{
		name:  name,
		ch:    make(chan Event, bufSize),
		id:    id,
		bus:   b,
		unsub: make(chan struct{}),
	}
	b.subs[id] = s
	return s
}

// Close shuts down the bus, closing all subscriber channels. Subsequent
// Publish calls return ErrBusClosed. Close is idempotent.
func (b *MemoryBus) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	subs := b.subs
	detached := b.detached
	b.subs = nil
	b.detached = nil
	b.mu.Unlock()

	// Signal all in-flight Publish calls to stop blocking, then wait
	// for them to finish. This ensures no goroutine is mid-send on
	// any subscriber channel when we close them below.
	close(b.done)
	b.inflight.Wait()

	for _, s := range subs {
		close(s.ch)
	}
	for _, s := range detached {
		close(s.ch)
	}
	return nil
}

// Events returns the receive-only channel carrying events for this subscriber.
func (s *memorySub) Events() <-chan Event {
	return s.ch
}

// Unsubscribe detaches this subscriber from the bus. Any in-flight Publish
// blocked on this subscriber is unblocked immediately. The subscriber's
// channel is closed when the bus shuts down via Close.
func (s *memorySub) Unsubscribe() {
	s.once.Do(func() {
		// Signal any blocked Publish to skip this subscriber.
		close(s.unsub)

		s.bus.mu.Lock()
		// Guard against Unsubscribe after Close (subs is nil).
		if s.bus.subs != nil {
			delete(s.bus.subs, s.id)
			// Track for channel close in Close().
			s.bus.detached = append(s.bus.detached, s)
		}
		s.bus.mu.Unlock()
	})
}
