package web

import (
	"context"
	"fmt"

	"github.com/papapumpkin/quasar/internal/bus"
)

// Compile-time interface check.
var _ EventSource = (*BusAdapter)(nil)

// BusAdapter bridges a bus.Bus to the web-local EventSource interface.
// Each Subscribe call creates a new bus subscription and translates
// bus.Event values into web.Event values suitable for SSE streaming.
type BusAdapter struct {
	bus bus.Bus
}

// NewBusAdapter creates an EventSource backed by the given event bus.
func NewBusAdapter(b bus.Bus) *BusAdapter {
	return &BusAdapter{bus: b}
}

// Subscribe implements EventSource. It creates a bus subscription and
// returns a channel of web Events. The cancel function unsubscribes
// from the bus and closes the channel when the forwarding goroutine
// finishes.
func (a *BusAdapter) Subscribe(ctx context.Context) (<-chan Event, func()) {
	sub := a.bus.Subscribe("web-sse", 128)
	ch := make(chan Event, 64)

	done := make(chan struct{})
	go func() {
		defer close(ch)
		defer close(done)
		for {
			select {
			case ev, ok := <-sub.Events():
				if !ok {
					return
				}
				webEv := busEventToWebEvent(ev)
				select {
				case ch <- webEv:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	cancel := func() {
		sub.Unsubscribe()
	}
	return ch, cancel
}

// busEventToWebEvent converts a bus.Event to a web.Event for SSE streaming.
// The event Type is the bus Kind string; the Data is a human-readable
// summary. Downstream phases will add JSON serialisation.
func busEventToWebEvent(ev bus.Event) Event {
	return Event{
		Type: string(ev.Kind),
		Data: fmt.Sprintf(`{"phase":"%s","message":"%s"}`, ev.PhaseID, escapeJSON(ev.Message)),
	}
}

// escapeJSON performs minimal JSON string escaping for embedded values.
func escapeJSON(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}
