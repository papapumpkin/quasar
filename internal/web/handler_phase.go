package web

import (
	"bytes"
	"net/http"

	"github.com/papapumpkin/quasar/internal/nebula"
)

// PhaseDetailData is the template context for the phase detail page.
type PhaseDetailData struct {
	Phase      *PhaseDetail
	NebulaName string
}

// handlePhaseDetail renders the per-phase detail page showing cycle timeline,
// agent entries, and reviewer assessments.
func (s *Server) handlePhaseDetail(w http.ResponseWriter, r *http.Request) {
	phaseID := r.PathValue("id")

	s.mu.RLock()
	neb := s.nebula
	s.mu.RUnlock()

	// Try accumulated event data first.
	var detail *PhaseDetail
	if s.accumulator != nil {
		detail = s.accumulator.Get(phaseID)
	}

	if detail == nil {
		// Phase may exist in the nebula spec but no events have arrived yet.
		spec := findPhaseSpec(neb, phaseID)
		if spec == nil {
			http.NotFound(w, r)
			return
		}
		detail = &PhaseDetail{
			ID:     spec.ID,
			Title:  spec.Title,
			Status: "pending",
		}
	}

	nebulaName := ""
	if neb != nil {
		nebulaName = neb.Manifest.Nebula.Name
	}

	data := PhaseDetailData{
		Phase:      detail,
		NebulaName: nebulaName,
	}

	var buf bytes.Buffer
	if err := s.pageTmpls["phase_detail.html"].ExecuteTemplate(&buf, "phase_detail.html", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// findPhaseSpec looks up a PhaseSpec by ID in the nebula. Returns nil if not found.
func findPhaseSpec(neb *nebula.Nebula, id string) *nebula.PhaseSpec {
	if neb == nil {
		return nil
	}
	for i := range neb.Phases {
		if neb.Phases[i].ID == id {
			return &neb.Phases[i]
		}
	}
	return nil
}
