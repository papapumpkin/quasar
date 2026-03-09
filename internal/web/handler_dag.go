package web

import (
	"bytes"
	"net/http"
)

// DAGPageData is the template context for the DAG visualization page.
type DAGPageData struct {
	NebulaName string
	Layout     DAGLayout
}

// handleDAG renders an SVG-based dependency graph of the nebula phases.
func (s *Server) handleDAG(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	neb := s.nebula
	st := s.state
	s.mu.RUnlock()

	layout := ComputeDAGLayout(neb, st)

	nebulaName := ""
	if neb != nil {
		nebulaName = neb.Manifest.Nebula.Name
	}

	data := DAGPageData{
		NebulaName: nebulaName,
		Layout:     layout,
	}

	tmpl := s.pageTmpls["dag.html"]
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "dag.html", data); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}
