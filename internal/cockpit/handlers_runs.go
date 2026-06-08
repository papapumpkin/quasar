package cockpit

import (
	"net/http"
	"strings"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// runDTO is the list-view JSON projection of a constellation run.
type runDTO struct {
	ID                string `json:"id"`
	RepoPath          string `json:"repo_path"`
	NebulaID          string `json:"nebula_id,omitempty"`
	ConstellationName string `json:"constellation_name"`
	State             string `json:"state"`
	CurrentNode       string `json:"current_node,omitempty"`
	Cycle             int    `json:"cycle"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

// invocationDTO is the JSON projection of one star/builtin firing in a run.
type invocationDTO struct {
	Seq      int     `json:"seq"`
	Node     string  `json:"node"`
	StarName string  `json:"star_name,omitempty"`
	State    string  `json:"state"`
	Cycle    int     `json:"cycle"`
	CostUSD  float64 `json:"cost_usd"`
	Preview  string  `json:"rationale_preview,omitempty"`
}

// runDetailDTO is the run-detail JSON: the run plus its full step trace.
type runDetailDTO struct {
	runDTO
	Invocations []invocationDTO `json:"invocations"`
}

// toRunDTO projects a run row onto the list wire shape.
func toRunDTO(r *fabric.RunRow) runDTO {
	return runDTO{
		ID: r.ID, RepoPath: r.RepoPath, NebulaID: r.NebulaID,
		ConstellationName: r.ConstellationName, State: r.State,
		CurrentNode: r.CurrentNode, Cycle: r.Cycle,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// handleListRuns serves GET /runs?state=. An empty state lists running runs,
// matching the TUI's default in-flight view.
func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "running"
	}
	runs, err := s.runs.ListByState(r.Context(), state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]runDTO, 0, len(runs))
	for _, run := range runs {
		out = append(out, toRunDTO(run))
	}
	s.writeJSON(w, http.StatusOK, out)
}

// handleGetRun serves GET /runs/{id}: the run plus its star_invocation trace.
func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	run, err := s.runs.GetRun(ctx, id)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	invs, err := s.runs.InvocationsForRun(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	detail := runDetailDTO{runDTO: toRunDTO(run), Invocations: make([]invocationDTO, 0, len(invs))}
	for _, inv := range invs {
		detail.Invocations = append(detail.Invocations, invocationDTO{
			Seq: inv.Seq, Node: inv.Node, StarName: inv.StarName, State: inv.State,
			Cycle: inv.Cycle, CostUSD: inv.CostUSD, Preview: inv.RationalePreview,
		})
	}
	s.writeJSON(w, http.StatusOK, detail)
}

// tailDTO is the JSON returned by the tail endpoint.
type tailDTO struct {
	RunID string   `json:"run_id"`
	Node  string   `json:"node"`
	Lines []string `json:"lines"`
}

// handleTail serves GET /runs/{id}/tail?node=&lines=: the most recent stdout
// lines of an in-flight invocation. It tails the rationale preview of the
// matching node's latest invocation (best-effort; full stdout streaming is a
// v2 concern). node defaults to the run's current node; lines defaults to 50.
func (s *Server) handleTail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	run, err := s.runs.GetRun(ctx, id)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	q := r.URL.Query()
	node := q.Get("node")
	if node == "" {
		node = run.CurrentNode
	}
	maxLines := parseLimit(q.Get("lines"))
	if maxLines == 0 {
		maxLines = 50
	}

	invs, err := s.runs.InvocationsForRun(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	lines := tailLines(latestPreviewForNode(invs, node), maxLines)
	s.writeJSON(w, http.StatusOK, tailDTO{RunID: id, Node: node, Lines: lines})
}

// latestPreviewForNode returns the rationale preview of the last invocation for
// node, or "" when none match. Invocations are ordered by seq, so the final
// match is the most recent.
func latestPreviewForNode(invs []*fabric.StarInvocationRow, node string) string {
	preview := ""
	for _, inv := range invs {
		if node == "" || inv.Node == node {
			preview = inv.RationalePreview
		}
	}
	return preview
}

// tailLines splits text into lines and returns at most the last n of them,
// dropping a trailing empty line from a final newline.
func tailLines(text string, n int) []string {
	if text == "" {
		return []string{}
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// handlePause serves POST /runs/{id}/pause.
func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	s.runtimeAction(w, r, "paused", func(runID string) error {
		return s.runtime.Pause(r.Context(), runID)
	})
}

// handleResume serves POST /runs/{id}/resume.
func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	s.runtimeAction(w, r, "resumed", func(runID string) error {
		return s.runtime.Resume(r.Context(), runID)
	})
}

// handleKill serves POST /runs/{id}/kill.
func (s *Server) handleKill(w http.ResponseWriter, r *http.Request) {
	s.runtimeAction(w, r, "killed", func(runID string) error {
		return s.runtime.Kill(r.Context(), runID)
	})
}

// runtimeAction is the shared body of pause/resume/kill: it forwards to the
// RuntimeController (503 when none is wired), then publishes a runs delta.
func (s *Server) runtimeAction(w http.ResponseWriter, r *http.Request, verb string, act func(string) error) {
	if s.runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime control not available")
		return
	}
	id := r.PathValue("id")
	if err := act(id); err != nil {
		s.writeStoreError(w, err)
		return
	}
	s.notifier.Publish(Event{Topic: topicRuns, Type: "run_" + verb,
		Data: map[string]string{"run_id": id}})
	s.writeJSON(w, http.StatusOK, map[string]string{"run_id": id, "state": verb})
}
