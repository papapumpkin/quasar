package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/nebula"
)

func TestHandlePhaseDetail_PendingPhase(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "test-nebula"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "phase-a", Title: "Phase Alpha"},
		},
	}

	srv := testServer(t, neb, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/phase/phase-a", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "phase-a") {
		t.Error("expected phase ID in output")
	}
	if !strings.Contains(body, "Phase Alpha") {
		t.Error("expected phase title in output")
	}
	if !strings.Contains(body, "pending") {
		t.Error("expected pending status")
	}
	if !strings.Contains(body, "No cycles yet") {
		t.Error("expected empty cycle state message")
	}
	if !strings.Contains(body, "text/html") {
		ct := rr.Header().Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Errorf("expected text/html content type, got %q", ct)
		}
	}
}

func TestHandlePhaseDetail_NotFound(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "test-nebula"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "phase-a", Title: "Phase Alpha"},
		},
	}

	srv := testServer(t, neb, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/phase/nonexistent", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestHandlePhaseDetail_NilNebula(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/phase/any-phase", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestHandlePhaseDetail_WithAccumulatedData(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "detail-test"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "phase-a", Title: "Phase Alpha"},
		},
	}

	srv := testServer(t, neb, nil)

	// Populate accumulator with cycle data.
	events := []Event{
		makeEvent("phase.task.started", "phase-a", eventPayload{Phase: "phase-a", Title: "Phase Alpha"}),
		makeEvent("phase.cycle.start", "phase-a", eventPayload{Phase: "phase-a", Cycle: 1}),
		makeEvent("phase.agent.start", "phase-a", eventPayload{Phase: "phase-a", Role: "coder"}),
		makeEvent("phase.agent.done", "phase-a", eventPayload{Phase: "phase-a", Role: "coder", CostUSD: 0.1234, DurationMs: 5000}),
		makeEvent("phase.agent.start", "phase-a", eventPayload{Phase: "phase-a", Role: "reviewer"}),
		makeEvent("phase.agent.done", "phase-a", eventPayload{Phase: "phase-a", Role: "reviewer", CostUSD: 0.0567, DurationMs: 2000}),
		makeEvent("phase.cycle.summary", "phase-a", eventPayload{
			Phase:   "phase-a",
			Message: "All good",
			Summary: &summaryPayload{Cycle: 1, Approved: true},
		}),
	}
	for _, ev := range events {
		srv.accumulator.handle(ev)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/phase/phase-a", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// Verify phase header.
	if !strings.Contains(body, "phase-a") {
		t.Error("expected phase ID in output")
	}
	if !strings.Contains(body, "detail-test") {
		t.Error("expected nebula name in output")
	}

	// Verify cycle content.
	if !strings.Contains(body, "Cycle 1") {
		t.Error("expected cycle number in output")
	}
	if !strings.Contains(body, "coder") {
		t.Error("expected coder role in output")
	}
	if !strings.Contains(body, "reviewer") {
		t.Error("expected reviewer role in output")
	}

	// Verify cost formatting.
	if !strings.Contains(body, "$0.1234") {
		t.Error("expected formatted coder cost")
	}

	// Verify duration.
	if !strings.Contains(body, "5000ms") {
		t.Error("expected coder duration")
	}

	// Verify summary.
	if !strings.Contains(body, "satisfied") {
		t.Error("expected satisfaction indicator")
	}
}

func TestHandlePhaseDetail_AgentOutput(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "output-test"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "p1", Title: "Output Phase"},
		},
	}

	srv := testServer(t, neb, nil)

	events := []Event{
		makeEvent("phase.task.started", "p1", eventPayload{Phase: "p1", Title: "Output Phase"}),
		makeEvent("phase.cycle.start", "p1", eventPayload{Phase: "p1", Cycle: 1}),
		makeEvent("phase.agent.start", "p1", eventPayload{Phase: "p1", Role: "coder"}),
		makeEvent("phase.agent.output", "p1", eventPayload{Phase: "p1", Role: "coder", Cycle: 1, Output: "hello from coder"}),
		makeEvent("phase.agent.done", "p1", eventPayload{Phase: "p1", Role: "coder", CostUSD: 0.01}),
	}
	for _, ev := range events {
		srv.accumulator.handle(ev)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/phase/p1", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "hello from coder") {
		t.Error("expected agent output in page")
	}
}

func TestFindPhaseSpec(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Phases: []nebula.PhaseSpec{
			{ID: "a", Title: "Alpha"},
			{ID: "b", Title: "Beta"},
		},
	}

	tests := []struct {
		name  string
		id    string
		found bool
	}{
		{"existing", "a", true},
		{"existing second", "b", true},
		{"missing", "c", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := findPhaseSpec(neb, tt.id)
			if (got != nil) != tt.found {
				t.Errorf("findPhaseSpec(%q) found=%v, want %v", tt.id, got != nil, tt.found)
			}
		})
	}

	// Nil nebula should return nil.
	if got := findPhaseSpec(nil, "a"); got != nil {
		t.Error("expected nil for nil nebula")
	}
}

func TestHandlePhaseDetail_BackLink(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "nav-test"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "p1", Title: "Phase One"},
		},
	}

	srv := testServer(t, neb, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/phase/p1", nil)
	srv.mux.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, `href="/"`) {
		t.Error("expected back link to dashboard")
	}
}

// makeEventJSON is a test helper that creates JSON from an eventPayload.
func makeEventJSON(payload eventPayload) string {
	data, _ := json.Marshal(payload)
	return string(data)
}
