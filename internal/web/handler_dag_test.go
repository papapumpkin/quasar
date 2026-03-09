package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/nebula"
)

func TestHandleDAG_BasicSVG(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "dag-test"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "phase-a", Title: "Phase Alpha"},
			{ID: "phase-b", Title: "Phase Beta", DependsOn: []string{"phase-a"}},
			{ID: "phase-c", Title: "Phase Gamma"},
		},
	}
	state := &nebula.State{Phases: make(map[string]*nebula.PhaseState)}

	srv := testServer(t, neb, state)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dag", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html, got %q", ct)
	}

	body := rr.Body.String()

	// Verify SVG element is present.
	if !strings.Contains(body, "<svg") {
		t.Error("expected SVG element in output")
	}

	// Verify correct number of node rects (3 phases = 3 rects).
	rectCount := strings.Count(body, "class=\"dag-node node--")
	if rectCount != 3 {
		t.Errorf("expected 3 dag-node rects, got %d", rectCount)
	}

	// Verify edges are present (1 dependency = 1 edge).
	edgeCount := strings.Count(body, "class=\"dag-edge")
	if edgeCount != 1 {
		t.Errorf("expected 1 dag-edge, got %d", edgeCount)
	}

	// Verify phase IDs appear in SVG text.
	for _, id := range []string{"phase-a", "phase-b", "phase-c"} {
		if !strings.Contains(body, id) {
			t.Errorf("expected phase ID %q in output", id)
		}
	}

	// Verify arrowhead marker is defined.
	if !strings.Contains(body, "arrowhead") {
		t.Error("expected arrowhead marker definition")
	}

	// Verify links to phase detail pages.
	if !strings.Contains(body, "/phase/phase-a") {
		t.Error("expected link to /phase/phase-a")
	}

	// Verify nebula name appears.
	if !strings.Contains(body, "dag-test") {
		t.Error("expected nebula name in output")
	}
}

func TestHandleDAG_StatusColors(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "status-test"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "done-p", Title: "Done"},
			{ID: "wip-p", Title: "Working"},
			{ID: "fail-p", Title: "Failed"},
		},
	}
	state := &nebula.State{
		Phases: map[string]*nebula.PhaseState{
			"done-p": {Status: nebula.PhaseStatusDone},
			"wip-p":  {Status: nebula.PhaseStatusInProgress},
			"fail-p": {Status: nebula.PhaseStatusFailed},
		},
	}

	srv := testServer(t, neb, state)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dag", nil)
	srv.mux.ServeHTTP(rr, req)

	body := rr.Body.String()

	// Verify status CSS classes appear.
	for _, cls := range []string{"node--done", "node--active", "node--failed"} {
		if !strings.Contains(body, cls) {
			t.Errorf("expected CSS class %q in output", cls)
		}
	}
}

func TestHandleDAG_Diamond(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "diamond"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "root", Title: "Root"},
			{ID: "left", Title: "Left", DependsOn: []string{"root"}},
			{ID: "right", Title: "Right", DependsOn: []string{"root"}},
			{ID: "sink", Title: "Sink", DependsOn: []string{"left", "right"}},
		},
	}
	state := &nebula.State{Phases: make(map[string]*nebula.PhaseState)}

	srv := testServer(t, neb, state)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dag", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// 4 nodes, 4 edges.
	rectCount := strings.Count(body, "class=\"dag-node node--")
	if rectCount != 4 {
		t.Errorf("expected 4 dag-node rects, got %d", rectCount)
	}
	edgeCount := strings.Count(body, "class=\"dag-edge")
	if edgeCount != 4 {
		t.Errorf("expected 4 dag-edges, got %d", edgeCount)
	}
}

func TestHandleDAG_NilNebula(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dag", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, "No phases loaded") {
		t.Error("expected empty state message for nil nebula")
	}
}

func TestHandleDAG_SSEConnect(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "sse-test"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "p1", Title: "Phase One"},
		},
	}
	state := &nebula.State{Phases: make(map[string]*nebula.PhaseState)}

	srv := testServer(t, neb, state)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dag", nil)
	srv.mux.ServeHTTP(rr, req)

	body := rr.Body.String()

	// Verify SSE connection setup for live updates.
	if !strings.Contains(body, `sse-connect="/events"`) {
		t.Error("expected SSE connect attribute")
	}
	if !strings.Contains(body, `sse-swap="dag-update"`) {
		t.Error("expected SSE swap attribute for dag-update")
	}
}

func TestHandleDAG_TitleTruncation(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "trunc-test"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "long", Title: "This is a very long title that should be truncated"},
		},
	}
	state := &nebula.State{Phases: make(map[string]*nebula.PhaseState)}

	srv := testServer(t, neb, state)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dag", nil)
	srv.mux.ServeHTTP(rr, req)

	body := rr.Body.String()

	// Title should be truncated with ellipsis.
	if strings.Contains(body, "This is a very long title that should be truncated") {
		t.Error("expected title to be truncated")
	}
	if !strings.Contains(body, "\u2026") {
		t.Error("expected ellipsis character in truncated title")
	}
}
