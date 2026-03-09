package web

import (
	"context"
	"encoding/json"
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

	go func() {
		defer close(ch)
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
// The event Type is the bus Kind string; the Data is a JSON object with
// phase and message fields.
func busEventToWebEvent(ev bus.Event) Event {
	return Event{
		Type: string(ev.Kind),
		Data: fmt.Sprintf(`{"phase":%s,"message":%s}`, jsonString(ev.PhaseID), jsonString(ev.Message)),
	}
}

// jsonString returns s as a JSON-encoded string literal (with surrounding
// quotes). Uses encoding/json.Marshal so all control characters are escaped
// correctly per the JSON spec.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
