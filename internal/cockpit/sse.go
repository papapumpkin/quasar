package cockpit

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// sseHeartbeat is how often the SSE handler writes a keepalive comment so idle
// connections (and intermediary proxies) don't time out.
const sseHeartbeat = 20 * time.Second

// handleSSE streams live updates to a connected browser as Datastar SSE frames.
// It subscribes to the notifier for the fleet and runs topics and, per event,
// pushes either a run-card fragment merge (live in-flight updates, matched by
// the card's element id) or a reload directive (when a nebula changes lanes).
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	_, ch, cancel := s.notifier.Subscribe([]string{"fleet", "runs"})
	defer cancel()

	ticker := time.NewTicker(sseHeartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case e, ok := <-ch:
			if !ok {
				return
			}
			s.renderEvent(r.Context(), w, e)
			flusher.Flush()
		}
	}
}

// renderEvent translates a notifier Event into a Datastar SSE frame on w.
// A step_started/step_completed re-renders just the affected run card (a live,
// in-place merge); a lane change or resync reloads the whole board.
func (s *Server) renderEvent(ctx context.Context, w http.ResponseWriter, e Event) {
	switch e.Type {
	case "step_started", "step_completed":
		runID, _ := e.Data["run_id"].(string)
		if runID == "" {
			return
		}
		rc, found, err := loadRun(ctx, s.db, runID)
		if err != nil {
			s.logf("cockpit sse: load run %s: %v", runID, err)
			return
		}
		if !found {
			return
		}
		var buf bytes.Buffer
		if err := s.renderRun(ctx, &buf, rc); err != nil {
			s.logf("cockpit sse: render run %s: %v", runID, err)
			return
		}
		writeMergeFragments(w, buf.String())
	case "nebula_status_changed", "nebula_seeded", "resync":
		writeReload(w)
	}
}

// writeMergeFragments writes a Datastar `datastar-merge-fragments` event. The
// fragment's root element id (run-<id>) is how Datastar locates and replaces the
// existing card in place. Multi-line HTML is split across `data:` lines so the
// whole fragment is one SSE event.
func writeMergeFragments(w http.ResponseWriter, html string) {
	_, _ = fmt.Fprint(w, "event: datastar-merge-fragments\n")
	for i, line := range strings.Split(html, "\n") {
		if i == 0 {
			_, _ = fmt.Fprintf(w, "data: fragments %s\n", line)
		} else {
			_, _ = fmt.Fprintf(w, "data: %s\n", line)
		}
	}
	_, _ = fmt.Fprint(w, "\n")
}

// writeReload writes a Datastar `datastar-execute-script` event that reloads the
// page. Used for lane changes and resync, where re-rendering the whole board is
// simpler and correct (the card moves between lanes).
func writeReload(w http.ResponseWriter) {
	_, _ = fmt.Fprint(w, "event: datastar-execute-script\n")
	_, _ = fmt.Fprint(w, "data: script window.location.reload()\n\n")
}
