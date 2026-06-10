package cockpit

import (
	"strconv"
	"sync"
)

// Event is a named state-change notification published to subscribed SSE handlers.
type Event struct {
	Topic string
	Type  string
	Data  map[string]any
}

// subscriber holds a single connected client's channel and topic filter.
type subscriber struct {
	topics     map[string]bool
	ch         chan Event
	needResync bool
}

// Notifier is a buffered, per-subscriber fan-out broadcaster. A slow subscriber
// whose channel buffer is full has the oldest queued event dropped and receives a
// synthetic "resync" event instead of blocking publishers.
type Notifier struct {
	mu   sync.Mutex
	subs map[string]*subscriber
	buf  int
	seq  int
}

// NewNotifier returns a Notifier that allocates per-subscriber channels of size
// buffer. If buffer is less than 1, it defaults to 64.
func NewNotifier(buffer int) *Notifier {
	if buffer < 1 {
		buffer = 64
	}
	return &Notifier{subs: map[string]*subscriber{}, buf: buffer}
}

// Subscribe registers interest in the given topics and returns an id, a read-only
// channel that receives matching events, and a cancel func that unregisters the
// subscriber and closes the channel.
func (n *Notifier) Subscribe(topics []string) (id string, ch <-chan Event, cancel func()) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.seq++
	id = strconv.Itoa(n.seq)
	tset := make(map[string]bool, len(topics))
	for _, t := range topics {
		tset[t] = true
	}
	s := &subscriber{topics: tset, ch: make(chan Event, n.buf)}
	n.subs[id] = s
	return id, s.ch, func() {
		n.mu.Lock()
		defer n.mu.Unlock()
		if _, ok := n.subs[id]; ok {
			delete(n.subs, id)
			close(s.ch)
		}
	}
}

// Publish sends e to every subscriber whose topic set includes e.Topic.
func (n *Notifier) Publish(e Event) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, s := range n.subs {
		if !s.topics[e.Topic] {
			continue
		}
		n.deliver(s, e)
	}
}

// deliver is non-blocking. On a full buffer it drops the oldest event and marks
// the subscriber for a single resync, which is sent ahead of the new event.
// Must be called with n.mu held.
func (n *Notifier) deliver(s *subscriber, e Event) {
	if s.needResync {
		e = Event{Topic: e.Topic, Type: "resync"}
		s.needResync = false
	}
	select {
	case s.ch <- e:
	default:
		// Buffer full: drain the oldest event.
		select {
		case <-s.ch:
		default:
		}
		s.needResync = true
		// Send a resync in place of the dropped event.
		select {
		case s.ch <- Event{Topic: e.Topic, Type: "resync"}:
		default:
		}
	}
}

// count returns the number of active subscribers. Used in tests.
func (n *Notifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.subs)
}
