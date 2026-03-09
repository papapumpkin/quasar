package web

import (
	"bytes"
	"net/http"
	"strconv"
)

// handleDiff renders a syntax-highlighted diff view for a specific phase cycle.
// It extracts the coder agent's raw diff from the accumulated phase data and
// renders it as an HTML page with file-by-file diff sections.
func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	phaseID := r.PathValue("id")
	cycleStr := r.PathValue("cycle")

	cycle, err := strconv.Atoi(cycleStr)
	if err != nil {
		http.Error(w, "invalid cycle number", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	neb := s.nebula
	s.mu.RUnlock()

	// Look up accumulated phase detail.
	var detail *PhaseDetail
	if s.accumulator != nil {
		detail = s.accumulator.Get(phaseID)
	}

	if detail == nil || cycle < 1 || cycle > len(detail.Cycles) {
		http.NotFound(w, r)
		return
	}

	// Find the coder agent's diff for this cycle.
	var rawDiff string
	for _, agent := range detail.Cycles[cycle-1].Agents {
		if agent.Role == "coder" && agent.Diff != "" {
			rawDiff = agent.Diff
			break
		}
	}
	if rawDiff == "" {
		http.Error(w, "no diff available for this cycle", http.StatusNotFound)
		return
	}

	data := RenderDiffHTML(rawDiff)
	data.NebulaName = ""
	if neb != nil {
		data.NebulaName = neb.Manifest.Nebula.Name
	}
	data.PhaseID = phaseID
	data.Cycle = cycle

	var buf bytes.Buffer
	if err := s.pageTmpls["diff.html"].ExecuteTemplate(&buf, "diff.html", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}
