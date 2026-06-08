package cockpit

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// Nebula status values the cockpit reasons about. Sensor drafts land as
// StatusAwaitingApproval; approve/reject move them to the corresponding state.
const (
	statusDraft            = "draft"
	statusAwaitingApproval = "awaiting_approval"
	statusApproved         = "approved"
	statusRejected         = "rejected"
	statusRunning          = "running"
	statusDone             = "done"
	statusFailed           = "failed"
)

// WS event types and topics published when a nebula's status changes.
const (
	topicFleet              = "fleet"
	topicRuns               = "runs"
	eventNebulaStatusChange = "nebula_status_changed"
)

// repoDTO is the JSON projection of a registered repo.
type repoDTO struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// nebulaDTO is the list/summary JSON projection of a nebula.
type nebulaDTO struct {
	ID          string `json:"id"`
	RepoPath    string `json:"repo_path"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	SourceName  string `json:"source_name,omitempty"`
	SourceID    string `json:"source_id,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// laneDTO groups a repo's nebulas into the three fleet lanes mirrored from the
// TUI fleet view.
type laneDTO struct {
	RepoPath        string      `json:"repo_path"`
	RepoName        string      `json:"repo_name"`
	AwaitingApprove []nebulaDTO `json:"awaiting_approval"`
	InFlight        []nebulaDTO `json:"in_flight"`
	Recent          []nebulaDTO `json:"recent"`
}

// fleetDTO is the full fleet snapshot: one lane group per repo.
type fleetDTO struct {
	Repos []laneDTO `json:"repos"`
}

// toNebulaDTO projects a store summary onto the wire shape.
func toNebulaDTO(n *fabric.NebulaSummary) nebulaDTO {
	return nebulaDTO{
		ID: n.ID, RepoPath: n.RepoPath, Name: n.Name, Description: n.Description,
		Status: n.Status, SourceName: n.SourceName, SourceID: n.SourceID,
		CreatedAt: n.CreatedAt.Unix(), UpdatedAt: n.UpdatedAt.Unix(),
	}
}

// lane returns which fleet lane a status belongs to: "awaiting", "inflight",
// "recent", or "" to omit it from the snapshot.
func lane(status string) string {
	switch status {
	case statusDraft, statusAwaitingApproval:
		return "awaiting"
	case statusApproved, statusRunning:
		return "inflight"
	case statusDone, statusFailed, statusRejected:
		return "recent"
	default:
		return ""
	}
}

// handleListRepos serves GET /repos: all active registered repos.
func (s *Server) handleListRepos(w http.ResponseWriter, r *http.Request) {
	rps, err := s.repos.List(r.Context(), "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]repoDTO, 0, len(rps))
	for _, rp := range rps {
		out = append(out, repoDTO{Path: rp.Path, Name: rp.Name, Status: rp.Status})
	}
	s.writeJSON(w, http.StatusOK, out)
}

// handleFleet serves GET /fleet: the full snapshot of every repo's lanes.
func (s *Server) handleFleet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rps, err := s.repos.List(ctx, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	fleet := fleetDTO{Repos: make([]laneDTO, 0, len(rps))}
	for _, rp := range rps {
		nebs, err := s.nebulas.List(ctx, fabric.ListFilter{RepoPath: rp.Path})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		group := laneDTO{
			RepoPath: rp.Path, RepoName: rp.Name,
			AwaitingApprove: []nebulaDTO{}, InFlight: []nebulaDTO{}, Recent: []nebulaDTO{},
		}
		for _, n := range nebs {
			dto := toNebulaDTO(n)
			switch lane(n.Status) {
			case "awaiting":
				group.AwaitingApprove = append(group.AwaitingApprove, dto)
			case "inflight":
				group.InFlight = append(group.InFlight, dto)
			case "recent":
				group.Recent = append(group.Recent, dto)
			}
		}
		fleet.Repos = append(fleet.Repos, group)
	}
	s.writeJSON(w, http.StatusOK, fleet)
}

// handleListNebulas serves GET /nebulas?repo=&status=&limit=.
func (s *Server) handleListNebulas(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := fabric.ListFilter{RepoPath: q.Get("repo"), Status: q.Get("status")}
	nebs, err := s.nebulas.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if limit := parseLimit(q.Get("limit")); limit > 0 && limit < len(nebs) {
		nebs = nebs[:limit]
	}
	out := make([]nebulaDTO, 0, len(nebs))
	for _, n := range nebs {
		out = append(out, toNebulaDTO(n))
	}
	s.writeJSON(w, http.StatusOK, out)
}

// handleGetNebula serves GET /nebulas/{id}: full nebula incl. phase metadata.
func (s *Server) handleGetNebula(w http.ResponseWriter, r *http.Request) {
	n, err := s.nebulas.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, n)
}

// phaseDTO is the JSON projection of a nebula phase (body included).
type phaseDTO struct {
	ID     string `json:"id"`
	Seq    int    `json:"seq"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Body   string `json:"body"`
}

// handleNebulaPhases serves GET /nebulas/{id}/phases: the ordered phase list.
func (s *Server) handleNebulaPhases(w http.ResponseWriter, r *http.Request) {
	n, err := s.nebulas.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	out := make([]phaseDTO, 0, len(n.Phases))
	for _, p := range n.Phases {
		out = append(out, phaseDTO{ID: p.ID, Seq: p.Seq, Title: p.Title, Status: p.Status, Body: p.Body})
	}
	s.writeJSON(w, http.StatusOK, out)
}

// handleApprove serves POST /nebulas/{id}/approve: set status to approved so
// the architect is enqueued, then publish a fleet delta.
func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	s.setNebulaStatus(w, r, statusApproved)
}

// rejectBody is the JSON request body for reject.
type rejectBody struct {
	Reason string `json:"reason"`
}

// handleReject serves POST /nebulas/{id}/reject: set status to rejected. The
// optional { reason } body is accepted but not yet persisted (v2).
func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	// Tolerate an empty body; only reject malformed non-empty JSON.
	if r.ContentLength != 0 {
		var body rejectBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
			return
		}
	}
	s.setNebulaStatus(w, r, statusRejected)
}

// handleUndelete serves POST /nebulas/{id}/undelete within the GC grace window.
func (s *Server) handleUndelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.nebulas.Undelete(r.Context(), id); err != nil {
		s.writeStoreError(w, err)
		return
	}
	s.notifier.Publish(Event{Topic: topicFleet, Type: eventNebulaStatusChange,
		Data: map[string]string{"id": id, "status": "undeleted"}})
	s.writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": "undeleted"})
}

// setNebulaStatus applies a status transition and publishes a fleet delta. It
// is the shared body of approve/reject so the publish stays consistent.
func (s *Server) setNebulaStatus(w http.ResponseWriter, r *http.Request, status string) {
	id := r.PathValue("id")
	if err := s.nebulas.SetStatus(r.Context(), id, status); err != nil {
		s.writeStoreError(w, err)
		return
	}
	s.notifier.Publish(Event{Topic: topicFleet, Type: eventNebulaStatusChange,
		Data: map[string]string{"id": id, "status": status}})
	s.writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": status})
}

// parseLimit parses a non-negative limit query value; invalid or absent yields 0
// (no limit).
func parseLimit(raw string) int {
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
