package cockpit

import "sync"

// Event is a single delta pushed to subscribers. It mirrors the WebSocket wire
// envelope: a Topic ("fleet" or "runs"), a Type discriminator
// ("nebula_status_changed", "step_started", …), and an opaque Data payload that
// is JSON-encoded on the wire.
type Event struct {
	Topic string `json:"topic"`
	Type  string `json:"type"`
	Data  any    `json:"data,omitempty"`
}

// ResyncType is the event Type emitted to a subscriber whose buffer overflowed.
// The client re-fetches /api/v1/fleet (and /runs) to catch up on the events it
// missed rather than acting on a partial delta stream.
const ResyncType = "resync"

// subBuffer is the per-subscriber channel depth. A subscriber that falls this
// far behind has its oldest events dropped in favor of a single resync hint.
const subBuffer = 64

// Notifier broadcasts delta events to subscribers. The runtime, scheduler, and
// TUI call Publish when state changes; WebSocket clients (and the TUI fleet
// view) are subscribers. It is the single source of truth for live updates so
// every consumer sees the same stream with no double-counting.
//
// Each subscriber has a bounded buffer: a slow client never blocks a publisher.
// When a subscriber's buffer is full, its oldest event is dropped and a single
// resync hint is queued in its place, telling the client to re-fetch the full
// snapshot.
type Notifier struct {
	mu     sync.Mutex
	nextID int
	subs   map[int]*subscriber
}

// subscriber is one registered listener: the topics it cares about, its bounded
// delivery channel, and a latched flag so repeated overflow enqueues at most one
// resync hint until the client drains.
type subscriber struct {
	topics  map[string]bool
	ch      chan Event
	dropped bool // at least one event was dropped since the client last caught up
}

// NewNotifier constructs an empty Notifier ready for Subscribe/Publish.
func NewNotifier() *Notifier {
	return &Notifier{subs: make(map[int]*subscriber)}
}

// Subscribe registers a listener for the given topics and returns its event
// channel plus an unsubscribe function. An empty topics slice subscribes to all
// topics. The unsubscribe function is idempotent and closes the channel.
func (n *Notifier) Subscribe(topics []string) (<-chan Event, func()) {
	set := make(map[string]bool, len(topics))
	for _, t := range topics {
		set[t] = true
	}

	sub := &subscriber{topics: set, ch: make(chan Event, subBuffer)}

	n.mu.Lock()
	id := n.nextID
	n.nextID++
	n.subs[id] = sub
	n.mu.Unlock()

	var once sync.Once
	unsub := func() {
		once.Do(func() {
			n.mu.Lock()
			if _, ok := n.subs[id]; ok {
				delete(n.subs, id)
				close(sub.ch)
			}
			n.mu.Unlock()
		})
	}
	return sub.ch, unsub
}

// Publish delivers event to every subscriber registered for its topic. Delivery
// is non-blocking: a subscriber whose buffer is full has its oldest event
// dropped and is flagged to receive a resync hint, so a slow client degrades to
// "catch up via snapshot" rather than stalling the publisher.
func (n *Notifier) Publish(event Event) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, sub := range n.subs {
		if !sub.wants(event.Topic) {
			continue
		}
		n.deliver(sub, event)
	}
}

// deliver enqueues event on sub without blocking. On overflow it drops the
// oldest buffered event to make room and latches a pending resync hint.
func (n *Notifier) deliver(sub *subscriber, event Event) {
	select {
	case sub.ch <- event:
		// The buffer had room: the client is keeping up, so clear the overflow
		// flag and re-arm resync detection for a future overflow.
		sub.dropped = false
		return
	default:
	}

	// Buffer full: drop the oldest event to make room and remember that this
	// subscriber fell behind.
	select {
	case <-sub.ch:
		sub.dropped = true
	default:
	}

	// Once a drop has happened, the client can no longer trust the delta stream,
	// so enqueue a resync hint as the newest event. Doing this on every overflow
	// (not just the first) guarantees a resync survives in the buffer even as
	// later overflows drop older entries; the client re-fetches the snapshot.
	toSend := event
	if sub.dropped {
		toSend = Event{Topic: event.Topic, Type: ResyncType}
	}

	select {
	case sub.ch <- toSend:
	default:
		// Still full after a drop (a concurrent drain raced); skip silently —
		// dropped stays latched so the next Publish re-attempts the resync.
	}
}

// wants reports whether the subscriber is interested in topic. A subscriber
// with no explicit topics receives every topic.
func (s *subscriber) wants(topic string) bool {
	if len(s.topics) == 0 {
		return true
	}
	return s.topics[topic]
}
