package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/nebula"
)

func TestHandleGateResolve_Success(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)
	gater := srv.Gater()

	req := &GateRequest{
		PhaseID:    "resolve-phase",
		PhaseTitle: "Resolve Test",
		Options: []GateOption{
			{Label: "Accept", Action: "accept"},
		},
		ResponseCh: make(chan nebula.GateAction, 1),
		CreatedAt:  time.Now(),
	}
	gater.Enqueue(req)

	form := url.Values{"action": {"accept"}}
	httpReq := httptest.NewRequest(http.MethodPost, "/gate/resolve-phase", strings.NewReader(form.Encode()))
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	srv.mux.ServeHTTP(rr, httpReq)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, "gate-resolved") {
		t.Error("expected gate-resolved class in response")
	}
	if !strings.Contains(body, "Decision: accept") {
		t.Error("expected 'Decision: accept' in response")
	}

	// Verify response channel received the action.
	select {
	case action := <-req.ResponseCh:
		if action != nebula.GateActionAccept {
			t.Errorf("expected accept, got %s", action)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response on channel")
	}
}

func TestHandleGateResolve_MissingAction(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)

	httpReq := httptest.NewRequest(http.MethodPost, "/gate/some-phase", nil)
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	srv.mux.ServeHTTP(rr, httpReq)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestHandleGateResolve_UnknownPhase(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)

	form := url.Values{"action": {"accept"}}
	httpReq := httptest.NewRequest(http.MethodPost, "/gate/nonexistent", strings.NewReader(form.Encode()))
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	srv.mux.ServeHTTP(rr, httpReq)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestHandleGateResolve_DoubleResolve(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)
	gater := srv.Gater()

	req := &GateRequest{
		PhaseID:    "double-phase",
		ResponseCh: make(chan nebula.GateAction, 1),
		CreatedAt:  time.Now(),
	}
	gater.Enqueue(req)

	// First resolve succeeds.
	form := url.Values{"action": {"accept"}}
	httpReq := httptest.NewRequest(http.MethodPost, "/gate/double-phase", strings.NewReader(form.Encode()))
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, httpReq)

	if rr.Code != http.StatusOK {
		t.Fatalf("first resolve: expected 200, got %d", rr.Code)
	}

	// Second resolve should return 404.
	httpReq2 := httptest.NewRequest(http.MethodPost, "/gate/double-phase", strings.NewReader(form.Encode()))
	httpReq2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr2 := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr2, httpReq2)

	if rr2.Code != http.StatusNotFound {
		t.Errorf("second resolve: expected 404, got %d", rr2.Code)
	}
}

func TestHandleGateList_Empty(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)

	httpReq := httptest.NewRequest(http.MethodGet, "/gates", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, httpReq)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, "No pending gates") {
		t.Error("expected empty state message")
	}
}

func TestHandleGateList_WithPending(t *testing.T) {
	t.Parallel()

	srv := testServer(t, nil, nil)
	gater := srv.Gater()

	gater.Enqueue(&GateRequest{
		PhaseID:      "gate-a",
		PhaseTitle:   "Gate Alpha",
		Satisfaction: "high",
		Risk:         "low",
		CostUSD:      0.5678,
		ReviewCycles: 2,
		Options: []GateOption{
			{Label: "Accept", Action: "accept"},
			{Label: "Reject", Action: "reject"},
		},
		ResponseCh: make(chan nebula.GateAction, 1),
		CreatedAt:  time.Now(),
	})

	httpReq := httptest.NewRequest(http.MethodGet, "/gates", nil)
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, httpReq)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, "gate-a") {
		t.Error("expected phase ID gate-a in response")
	}
	if !strings.Contains(body, "Gate Alpha") {
		t.Error("expected phase title in response")
	}
	if !strings.Contains(body, "$0.5678") {
		t.Error("expected cost in response")
	}
	if !strings.Contains(body, "2 cycle(s)") {
		t.Error("expected cycle count in response")
	}
}
