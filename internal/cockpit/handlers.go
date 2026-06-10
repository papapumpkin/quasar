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

// handleApprove approves the nebula identified by the {id} path segment.
// Replaced in Task 10.
//
// TODO(task 10): wire to s.rt.Approve.
func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

// handleReject rejects the nebula identified by the {id} path segment.
// Replaced in Task 10.
//
// TODO(task 10): wire to s.rt.Reject.
func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
