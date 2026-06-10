package cockpit

import (
	"context"
	"net/http"
	"time"
)

// handleFleet loads the current fleet state and renders the Mission Control
// dashboard page via the injected PageRenderer. On a DB error it returns 500.
// If a GitHubBadger is wired in, PR-state badges are enriched best-effort with
// a per-call timeout so a slow or absent gh cannot hang the page.
func (s *Server) handleFleet(w http.ResponseWriter, r *http.Request) {
	f, err := LoadFleet(r.Context(), s.db)
	if err != nil {
		s.logf("cockpit: LoadFleet: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if s.github != nil {
		s.enrichPRStates(r.Context(), &f)
	}
	if err := s.renderPage(r.Context(), w, f); err != nil {
		s.logf("cockpit: render fleet page: %v", err)
	}
}

// prBadgeTimeout is the per-PR-status-call deadline. The 30s ghBadger cache
// makes steady-state lookups instant; this guards only the first cold fetch.
const prBadgeTimeout = 1500 * time.Millisecond

// enrichPRStates calls s.github.PRStatus for every NebulaCard in Awaiting and
// Recent that carries a PR number. Each call is independently time-bounded.
// Errors are logged and left as empty PRState so they never fail the page.
func (s *Server) enrichPRStates(ctx context.Context, f *Fleet) {
	for li := range f.Repos {
		lane := &f.Repos[li]
		for ci := range lane.Awaiting {
			s.fetchPRState(ctx, lane.Path, &lane.Awaiting[ci])
		}
		for ci := range lane.Recent {
			s.fetchPRState(ctx, lane.Path, &lane.Recent[ci])
		}
	}
}

// fetchPRState fetches the live PR state for a single card, updating
// card.PRState in place. No-op when PRNumber is zero.
func (s *Server) fetchPRState(ctx context.Context, repoPath string, card *NebulaCard) {
	if card.PRNumber <= 0 {
		return
	}
	tctx, cancel := context.WithTimeout(ctx, prBadgeTimeout)
	defer cancel()
	state, err := s.github.PRStatus(tctx, repoPath, card.PRNumber)
	if err != nil {
		s.logf("cockpit: PR badge %s#%d: %v", repoPath, card.PRNumber, err)
		return
	}
	card.PRState = state
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
