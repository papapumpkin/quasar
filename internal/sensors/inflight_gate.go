package sensors

import "sync"

// inflightGate is a FIFO-fair counting gate. submit fires immediately while
// slots remain and otherwise queues; release frees a slot and drains the next
// queued action. It models the per-(repo, sensor) cap on concurrent in-flight
// constellation triggers — slots are freed by an external completion signal
// (ReleaseInflight), not by the trigger callback returning, because a
// constellation outlives the call that launches it.
type inflightGate struct {
	max   int
	mu    sync.Mutex
	count int
	queue []func()
}

func newInflightGate(max int) *inflightGate {
	if max <= 0 {
		max = defaultMaxInflight
	}
	return &inflightGate{max: max}
}

// submit fires f immediately and returns true when a slot is free; otherwise it
// queues f and returns false. f runs outside the lock so a slow trigger cannot
// block the gate.
func (g *inflightGate) submit(f func()) bool {
	g.mu.Lock()
	if g.count < g.max {
		g.count++
		g.mu.Unlock()
		f()
		return true
	}
	g.queue = append(g.queue, f)
	g.mu.Unlock()
	return false
}

// release frees one slot. If actions are queued, the oldest is dequeued and
// fired under a freshly-claimed slot (net in-flight count unchanged), preserving
// FIFO order.
func (g *inflightGate) release() {
	g.mu.Lock()
	if len(g.queue) > 0 {
		next := g.queue[0]
		g.queue = g.queue[1:]
		g.mu.Unlock()
		next()
		return
	}
	if g.count > 0 {
		g.count--
	}
	g.mu.Unlock()
}
