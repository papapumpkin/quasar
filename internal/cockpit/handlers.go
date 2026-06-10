package cockpit

import (
	"net/http"
)

// handleFleet loads the current fleet state and renders the Mission Control
// dashboard page via the injected PageRenderer. On a DB error it returns 500.
func (s *Server) handleFleet(w http.ResponseWriter, r *http.Request) {
	f, err := LoadFleet(r.Context(), s.db)
	if err != nil {
		s.logf("cockpit: LoadFleet: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if err := s.renderPage(r.Context(), w, f); err != nil {
		s.logf("cockpit: render fleet page: %v", err)
	}
}

// handleRunDetail loads a single constellation run and its star-invocation step
// trace, then renders the run-detail page via the injected RunDetailRenderer.
// Returns 400 when the id path value is missing, 404 when the run is not
// found, and 500 on a DB or render error.
func (s *Server) handleRunDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing run id", http.StatusBadRequest)
		return
	}
	d, found, err := LoadRunDetail(r.Context(), s.db, id)
	if err != nil {
		s.logf("cockpit: LoadRunDetail %s: %v", id, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	if err := s.renderRunDetail(r.Context(), w, d); err != nil {
		s.logf("cockpit: render run detail %s: %v", id, err)
	}
}

// handleNebulaDetail loads a single nebula with its phases and constellation
// runs, then renders the nebula-detail page via the injected
// NebulaDetailRenderer. Returns 400 when the id path value is missing, 404
// when the nebula is not found, and 500 on a DB or render error.
func (s *Server) handleNebulaDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing nebula id", http.StatusBadRequest)
		return
	}
	d, found, err := LoadNebulaDetail(r.Context(), s.db, id)
	if err != nil {
		s.logf("cockpit: LoadNebulaDetail %s: %v", id, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "nebula not found", http.StatusNotFound)
		return
	}
	if err := s.renderNebulaDetail(r.Context(), w, d); err != nil {
		s.logf("cockpit: render nebula detail %s: %v", id, err)
	}
}

// handleApprove approves the nebula identified by the {id} path segment via the
// injected RuntimeActions, then publishes a lane-change event so every connected
// operator's board updates. The board refresh is delivered over SSE, so the POST
// itself returns 204.
func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing nebula id", http.StatusBadRequest)
		return
	}
	if s.rt == nil {
		http.Error(w, "actions unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.rt.Approve(r.Context(), id); err != nil {
		s.logf("cockpit: approve %s: %v", id, err)
		http.Error(w, "approve failed", http.StatusInternalServerError)
		return
	}
	s.notifier.Publish(Event{Topic: "fleet", Type: "nebula_status_changed",
		Data: map[string]any{"id": id, "status": "approved"}})
	w.WriteHeader(http.StatusNoContent)
}

// handleReject rejects the nebula identified by the {id} path segment, with an
// optional `reason` form value, then publishes a lane-change event.
func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing nebula id", http.StatusBadRequest)
		return
	}
	if s.rt == nil {
		http.Error(w, "actions unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.rt.Reject(r.Context(), id, r.FormValue("reason")); err != nil {
		s.logf("cockpit: reject %s: %v", id, err)
		http.Error(w, "reject failed", http.StatusInternalServerError)
		return
	}
	s.notifier.Publish(Event{Topic: "fleet", Type: "nebula_status_changed",
		Data: map[string]any{"id": id, "status": "rejected"}})
	w.WriteHeader(http.StatusNoContent)
}
