package web

import (
	"fmt"
	"net/http"
	"strings"
)

// handleSSE streams server-sent events to the connected client. Each event
// from the EventSource is written as a named SSE message (event: + data:).
// The connection stays open until the client disconnects or the server's
// context is cancelled.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Flush headers immediately so the client sees the 200 response
	// and Content-Type before any events arrive.
	flusher.Flush()

	// Create a per-client event channel for drain tracking.
	ch := make(chan Event, 64)
	s.addSSEClient(ch)
	defer s.removeSSEClient(ch)

	// Optional phase filter: ?phase={id} restricts events to a single phase.
	phaseFilter := r.URL.Query().Get("phase")

	// Subscribe to the event source if available. The forwarding goroutine
	// copies events into the per-client channel so that drainSSE can close
	// ch to unblock the handler during graceful shutdown.
	if s.cfg.Source != nil {
		events, cancel := s.cfg.Source.Subscribe(r.Context())
		defer cancel()

		go func() {
			for ev := range events {
				if phaseFilter != "" && ev.PhaseID != phaseFilter {
					continue
				}
				select {
				case ch <- ev:
				default:
					// Drop event if client is slow.
				}
			}
		}()
	}

	// When streaming to the dashboard (no phase filter), translate raw
	// JSON events into HTML fragments via the SSE bridge so HTMX can
	// swap them into the DOM. Phase-filtered streams (detail pages)
	// pass through raw JSON events unchanged.
	bridge := NewSSEBridge(s)
	useBridge := phaseFilter == ""

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			out := ev
			if useBridge {
				translated, err := bridge.TranslateEvent(ev)
				if err != nil {
					// Skip malformed events without breaking the connection.
					continue
				}
				out = translated
			}
			data := escapeSSEData(out.Data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", out.Type, data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// addSSEClient registers a client channel for SSE drain tracking.
func (s *Server) addSSEClient(ch chan Event) {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	s.sseClients[ch] = struct{}{}
}

// removeSSEClient unregisters a client channel.
func (s *Server) removeSSEClient(ch chan Event) {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	delete(s.sseClients, ch)
}

// broadcast sends an event to all connected SSE clients. Events are
// dropped for slow clients whose channel buffers are full.
func (s *Server) broadcast(ev Event) {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	for ch := range s.sseClients {
		select {
		case ch <- ev:
		default:
			// Drop event if client is slow.
		}
	}
}

// broadcastGatePrompt renders the gate form partial and pushes it to all
// connected SSE clients as a "gate-prompt" event.
func (s *Server) broadcastGatePrompt(req *GateRequest) {
	var buf strings.Builder
	if err := s.pageTmpls["gate_list.html"].ExecuteTemplate(&buf, "gate-form", req); err != nil {
		fmt.Fprintf(&buf, `<div class="gate-error">failed to render gate form: %s</div>`, err.Error())
	}
	s.broadcast(Event{
		Type: "gate-prompt",
		Data: buf.String(),
	})
}

// drainSSE closes all active SSE client channels to unblock handlers
// during graceful shutdown. Safe to call multiple times.
func (s *Server) drainSSE() {
	s.sseCloseOnce.Do(func() {
		s.sseMu.Lock()
		defer s.sseMu.Unlock()
		for ch := range s.sseClients {
			close(ch)
			delete(s.sseClients, ch)
		}
	})
}
