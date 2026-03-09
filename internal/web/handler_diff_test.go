package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/nebula"
)

const testDiff = `diff --git a/main.go b/main.go
index abc1234..def5678 100644
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main

+import "os"
 func main() {}
`

func TestHandleDiff_Success(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "diff-test"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "p1", Title: "Phase One"},
		},
	}

	srv := testServer(t, neb, nil)

	// Populate accumulator with a cycle that has a coder diff.
	srv.accumulator.handle(makeEvent("phase.task.started", "p1",
		eventPayload{Phase: "p1", Title: "Phase One"}))
	srv.accumulator.handle(makeEvent("phase.cycle.start", "p1",
		eventPayload{Phase: "p1", Cycle: 1}))
	srv.accumulator.handle(makeEvent("phase.agent.start", "p1",
		eventPayload{Phase: "p1", Role: "coder"}))

	// Inject diff directly into the accumulator's phase detail.
	detail := srv.accumulator.Get("p1")
	if detail == nil || len(detail.Cycles) == 0 || len(detail.Cycles[0].Agents) == 0 {
		t.Fatal("accumulator did not create expected phase detail")
	}
	detail.Cycles[0].Agents[0].Diff = testDiff
	detail.Cycles[0].Agents[0].Done = true

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/phase/p1/diff/1", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()

	// Verify diff stat.
	if !strings.Contains(body, "1 file(s) changed") {
		t.Error("expected file count in diff stat")
	}

	// Verify file path.
	if !strings.Contains(body, "main.go") {
		t.Error("expected file path in diff output")
	}

	// Verify CSS classes for line types.
	if !strings.Contains(body, "diff-line--add") {
		t.Error("expected add line CSS class")
	}
	if !strings.Contains(body, "diff-line--context") {
		t.Error("expected context line CSS class")
	}

	// Verify page structure.
	if !strings.Contains(body, "diff-test") {
		t.Error("expected nebula name in page")
	}
	if !strings.Contains(body, "p1") {
		t.Error("expected phase ID in page")
	}

	// Verify content type.
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html, got %q", ct)
	}
}

func TestHandleDiff_InvalidCycle(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "test"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "p1", Title: "Phase One"},
		},
	}

	srv := testServer(t, neb, nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/phase/p1/diff/abc", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-numeric cycle, got %d", rr.Code)
	}
}

func TestHandleDiff_CycleOutOfRange(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "test"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "p1", Title: "Phase One"},
		},
	}

	srv := testServer(t, neb, nil)

	// Create one cycle.
	srv.accumulator.handle(makeEvent("phase.task.started", "p1",
		eventPayload{Phase: "p1", Title: "Phase One"}))
	srv.accumulator.handle(makeEvent("phase.cycle.start", "p1",
		eventPayload{Phase: "p1", Cycle: 1}))

	// Request cycle 5 which doesn't exist.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/phase/p1/diff/5", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for out-of-range cycle, got %d", rr.Code)
	}
}

func TestHandleDiff_NoDiffAvailable(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "test"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "p1", Title: "Phase One"},
		},
	}

	srv := testServer(t, neb, nil)

	// Create a cycle with a coder agent but no diff.
	srv.accumulator.handle(makeEvent("phase.task.started", "p1",
		eventPayload{Phase: "p1", Title: "Phase One"}))
	srv.accumulator.handle(makeEvent("phase.cycle.start", "p1",
		eventPayload{Phase: "p1", Cycle: 1}))
	srv.accumulator.handle(makeEvent("phase.agent.start", "p1",
		eventPayload{Phase: "p1", Role: "coder"}))
	srv.accumulator.handle(makeEvent("phase.agent.done", "p1",
		eventPayload{Phase: "p1", Role: "coder", CostUSD: 0.01}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/phase/p1/diff/1", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing diff, got %d", rr.Code)
	}
}

func TestHandleDiff_UnknownPhase(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/phase/nonexistent/diff/1", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown phase, got %d", rr.Code)
	}
}

func TestHandleDiff_ZeroCycle(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "test"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "p1", Title: "Phase One"},
		},
	}

	srv := testServer(t, neb, nil)

	srv.accumulator.handle(makeEvent("phase.task.started", "p1",
		eventPayload{Phase: "p1", Title: "Phase One"}))
	srv.accumulator.handle(makeEvent("phase.cycle.start", "p1",
		eventPayload{Phase: "p1", Cycle: 1}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/phase/p1/diff/0", nil)
	srv.mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cycle 0, got %d", rr.Code)
	}
}

func TestHandleDiff_CollapsibleFileSection(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "test"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "p1", Title: "Phase One"},
		},
	}

	srv := testServer(t, neb, nil)

	srv.accumulator.handle(makeEvent("phase.task.started", "p1",
		eventPayload{Phase: "p1", Title: "Phase One"}))
	srv.accumulator.handle(makeEvent("phase.cycle.start", "p1",
		eventPayload{Phase: "p1", Cycle: 1}))
	srv.accumulator.handle(makeEvent("phase.agent.start", "p1",
		eventPayload{Phase: "p1", Role: "coder"}))

	detail := srv.accumulator.Get("p1")
	detail.Cycles[0].Agents[0].Diff = testDiff
	detail.Cycles[0].Agents[0].Done = true

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/phase/p1/diff/1", nil)
	srv.mux.ServeHTTP(rr, req)

	body := rr.Body.String()

	// File should have <details> element.
	if !strings.Contains(body, "<details") {
		t.Error("expected collapsible <details> element")
	}
	// Small file should be open by default.
	if !strings.Contains(body, "open") {
		t.Error("expected small file to be open by default")
	}
}

func TestHandleDiff_LineNumberGutter(t *testing.T) {
	t.Parallel()

	neb := &nebula.Nebula{
		Manifest: nebula.Manifest{
			Nebula: nebula.Info{Name: "test"},
		},
		Phases: []nebula.PhaseSpec{
			{ID: "p1", Title: "Phase One"},
		},
	}

	srv := testServer(t, neb, nil)

	srv.accumulator.handle(makeEvent("phase.task.started", "p1",
		eventPayload{Phase: "p1", Title: "Phase One"}))
	srv.accumulator.handle(makeEvent("phase.cycle.start", "p1",
		eventPayload{Phase: "p1", Cycle: 1}))
	srv.accumulator.handle(makeEvent("phase.agent.start", "p1",
		eventPayload{Phase: "p1", Role: "coder"}))

	detail := srv.accumulator.Get("p1")
	detail.Cycles[0].Agents[0].Diff = testDiff
	detail.Cycles[0].Agents[0].Done = true

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/phase/p1/diff/1", nil)
	srv.mux.ServeHTTP(rr, req)

	body := rr.Body.String()

	// Verify gutter columns exist.
	if !strings.Contains(body, "old-num") {
		t.Error("expected old line number gutter column")
	}
	if !strings.Contains(body, "new-num") {
		t.Error("expected new line number gutter column")
	}
}
