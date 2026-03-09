package web

import (
	"bytes"
	"fmt"
	"html"
	"net/http"
)

// handleGateList renders all pending gate prompts as a full page.
func (s *Server) handleGateList(w http.ResponseWriter, _ *http.Request) {
	if s.gater == nil {
		http.Error(w, "gate prompts not available", http.StatusServiceUnavailable)
		return
	}

	pending := s.gater.Pending()

	var buf bytes.Buffer
	if err := s.pageTmpls["gate_list.html"].ExecuteTemplate(&buf, "gate_list.html", pending); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// handleGateResolve accepts a gate decision via form POST and sends the
// action to the waiting worker goroutine. Returns the resolved confirmation
// HTML fragment for HTMX swap.
func (s *Server) handleGateResolve(w http.ResponseWriter, r *http.Request) {
	if s.gater == nil {
		http.Error(w, "gate prompts not available", http.StatusServiceUnavailable)
		return
	}

	phaseID := r.PathValue("id")
	action := r.FormValue("action")

	if action == "" {
		http.Error(w, "missing action", http.StatusBadRequest)
		return
	}

	if err := s.gater.Resolve(phaseID, action); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Return a confirmation fragment that replaces the gate form via HTMX swap.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<div id="gate-%s" class="gate-resolved">Decision: %s</div>`,
		html.EscapeString(phaseID), html.EscapeString(action))
}
