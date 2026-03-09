package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/nebula"
)

func TestHandleDashboard_ZeroState(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "test-nebula"},
			Execution: nebula.Execution{
				MaxReviewCycles: 5,
				MaxBudgetUSD:    10.0,
			},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "phase-a", Title: "Phase Alpha"},
			{ID: "phase-b", Title: "Phase Beta", DependsOn: []string{"phase-a"}},
			{ID: "phase-c", Title: "Phase Gamma"},
		},
	}
	state := &nebula.State{
		Phases: make(map[string]*nebula.PhaseState),
	}

	srv := testServer(t, neb, state)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// Verify HTML content type.
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content type, got %q", ct)
	}

	// Verify nebula name appears.
	if !strings.Contains(body, "test-nebula") {
		t.Error("expected nebula name in output")
	}

	// Verify all phase IDs are present with correct element IDs.
	for _, id := range []string{"phase-a", "phase-b", "phase-c"} {
		if !strings.Contains(body, `id="phase-`+id+`"`) {
			t.Errorf("expected phase row with id=%q", id)
		}
	}

	// Verify phase links to detail page.
	if !strings.Contains(body, `/phase/phase-a`) {
		t.Error("expected link to phase detail page")
	}

	// Verify progress shows 0/3.
	if !strings.Contains(body, "0/3") {
		t.Error("expected progress text 0/3")
	}

	// Verify cost formatted to 4 decimal places.
	if !strings.Contains(body, "$0.0000") {
		t.Error("expected cost formatted to 4 decimal places")
	}

	// Verify pending status icon (middle dot).
	if !strings.Contains(body, "\u00b7") {
		t.Error("expected pending status icon (middle dot)")
	}

	// Verify cycles format.
	if !strings.Contains(body, "0/5") {
		t.Error("expected cycle count 0/5")
	}
}

func TestHandleDashboard_MixedState(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "mixed-nebula"},
			Execution: nebula.Execution{
				MaxReviewCycles: 5,
				MaxBudgetUSD:    50.0,
			},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "phase-done", Title: "Done Phase"},
			{ID: "phase-wip", Title: "Working Phase"},
			{ID: "phase-fail", Title: "Failed Phase"},
			{ID: "phase-pending", Title: "Pending Phase", DependsOn: []string{"phase-wip"}},
		},
	}
	now := time.Now()
	state := &nebula.State{
		TotalCostUSD: 1.2345,
		Phases: map[string]*nebula.PhaseState{
			"phase-done": {Status: nebula.PhaseStatusDone, UpdatedAt: now},
			"phase-wip":  {Status: nebula.PhaseStatusInProgress, UpdatedAt: now},
			"phase-fail": {Status: nebula.PhaseStatusFailed, UpdatedAt: now},
		},
	}

	srv := testServer(t, neb, state)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// Verify done icon (checkmark).
	if !strings.Contains(body, "\u2713") {
		t.Error("expected done icon (checkmark)")
	}

	// Verify working icon (circled bullet).
	if !strings.Contains(body, "\u25ce") {
		t.Error("expected working icon (circled bullet)")
	}

	// Verify failed icon (ballot X).
	if !strings.Contains(body, "\u2717") {
		t.Error("expected failed icon (ballot X)")
	}

	// Verify total cost.
	if !strings.Contains(body, "$1.2345") {
		t.Error("expected total cost $1.2345")
	}

	// Verify progress shows 1/4 (only "done" counts, not "failed").
	if !strings.Contains(body, "1/4") {
		t.Error("expected progress text 1/4")
	}

	// Verify CSS status classes are present.
	if !strings.Contains(body, "phase-status--done") {
		t.Error("expected phase-status--done class")
	}
	if !strings.Contains(body, "phase-status--in_progress") {
		t.Error("expected phase-status--in_progress class")
	}
	if !strings.Contains(body, "phase-status--failed") {
		t.Error("expected phase-status--failed class")
	}
	if !strings.Contains(body, "phase-status--pending") {
		t.Error("expected phase-status--pending class")
	}
}

func TestHandleDashboard_NotFoundForOtherPaths(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestHandleDashboard_NilNebula(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "No phases loaded") {
		t.Error("expected empty state message")
	}
}

func TestBuildDashboardData(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "data-test"},
			Execution: nebula.Execution{
				MaxReviewCycles: 3,
				MaxBudgetUSD:    25.0,
			},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "a", Title: "Alpha"},
			{ID: "b", Title: "Beta", DependsOn: []string{"a"}},
		},
	}
	state := &nebula.State{
		TotalCostUSD: 0.5,
		Phases: map[string]*nebula.PhaseState{
			"a": {Status: nebula.PhaseStatusDone},
		},
	}

	data := buildDashboardData(neb, state, time.Now().Add(-30*time.Second))

	if data.NebulaName != "data-test" {
		t.Errorf("expected name data-test, got %s", data.NebulaName)
	}
	if data.Total != 2 {
		t.Errorf("expected total 2, got %d", data.Total)
	}
	if data.Completed != 1 {
		t.Errorf("expected completed 1, got %d", data.Completed)
	}
	if data.ProgressPct != 50 {
		t.Errorf("expected 50%%, got %d%%", data.ProgressPct)
	}
	if data.BudgetUSD != 25.0 {
		t.Errorf("expected budget 25.0, got %f", data.BudgetUSD)
	}
	if len(data.Phases) != 2 {
		t.Fatalf("expected 2 phase rows, got %d", len(data.Phases))
	}

	// Phase A should be done.
	if data.Phases[0].Status != "done" {
		t.Errorf("expected phase a status done, got %s", data.Phases[0].Status)
	}
	if data.Phases[0].StatusIcon != "\u2713" {
		t.Errorf("expected checkmark icon for done phase")
	}

	// Phase B should be pending and blocked by A (A is done, so no blocker).
	if data.Phases[1].Status != "pending" {
		t.Errorf("expected phase b status pending, got %s", data.Phases[1].Status)
	}
	if len(data.Phases[1].BlockedBy) != 0 {
		t.Errorf("expected no blockers (a is done), got %v", data.Phases[1].BlockedBy)
	}

	// Cycles should be formatted.
	if data.Phases[0].Cycles != "0/3" {
		t.Errorf("expected cycles 0/3, got %s", data.Phases[0].Cycles)
	}
}

func TestBuildDashboardData_NilNebula(t *testing.T) {
	t.Parallel()

	data := buildDashboardData(nil, nil, time.Time{})
	if data.Total != 0 {
		t.Errorf("expected 0 total for nil nebula, got %d", data.Total)
	}
	if len(data.Phases) != 0 {
		t.Errorf("expected 0 phases for nil nebula, got %d", len(data.Phases))
	}
}

// testServer creates a Server with the given nebula and state for testing.
func testServer(t *testing.T, neb *nebula.Nebula, state *nebula.State) *Server {
	t.Helper()

	srv, err := NewServer(ServerConfig{NebulaDir: "/tmp/test-nebula"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.SetNebula(neb, state)
	return srv
}
