package web

import "context"

// Event represents a single server-sent event with a named type and data payload.
// The Type field maps to the SSE "event:" line, and Data maps to the "data:" line.
type Event struct {
	// Type is the SSE event name (e.g. "phase-status", "progress", "agent-done").
	// HTMX SSE extension uses this to dispatch swap targets.
	Type string

	// Data is the JSON-encoded payload for this event.
	Data string
}

// EventSource provides a stream of typed events for SSE broadcasting.
// Implemented by an adapter that bridges the Pulsar event bus to the
// web-specific Event type. Defined here (at the consumer) per project
// convention.
type EventSource interface {
	// Subscribe returns a channel that receives Event values and a cancel
	// function that unsubscribes and closes the channel. The channel is
	// also closed when ctx is done.
	Subscribe(ctx context.Context) (events <-chan Event, cancel func())
}
