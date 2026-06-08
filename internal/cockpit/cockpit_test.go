package cockpit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/repos"
)

// --- fakes ---

type fakeRepos struct{ repos []*repos.Repo }

func (f *fakeRepos) List(context.Context, string) ([]*repos.Repo, error) { return f.repos, nil }

type fakeNebulas struct {
	summaries  []*fabric.NebulaSummary
	nebula     *fabric.Nebula
	statusErr  error
	lastStatus string
	lastID     string
}

func (f *fakeNebulas) List(context.Context, fabric.ListFilter) ([]*fabric.NebulaSummary, error) {
	return f.summaries, nil
}
func (f *fakeNebulas) Get(_ context.Context, id string) (*fabric.Nebula, error) {
	if f.nebula == nil {
		return nil, fabric.ErrNebulaNotFound
	}
	return f.nebula, nil
}
func (f *fakeNebulas) SetStatus(_ context.Context, id, status string) error {
	f.lastID, f.lastStatus = id, status
	return f.statusErr
}
func (f *fakeNebulas) Undelete(context.Context, string) error { return nil }

type fakeRuns struct {
	byState map[string][]*fabric.RunRow
	run     *fabric.RunRow
	invs    []*fabric.StarInvocationRow
}

func (f *fakeRuns) ListByState(_ context.Context, state string) ([]*fabric.RunRow, error) {
	return f.byState[state], nil
}
func (f *fakeRuns) GetRun(context.Context, string) (*fabric.RunRow, error) {
	if f.run == nil {
		return nil, fabric.ErrRunNotFound
	}
	return f.run, nil
}
func (f *fakeRuns) InvocationsForRun(context.Context, string) ([]*fabric.StarInvocationRow, error) {
	return f.invs, nil
}

func testServer(t *testing.T, opts Opts) http.Handler {
	t.Helper()
	if opts.Token == "" {
		opts.Token = "secret"
	}
	if opts.Repos == nil {
		opts.Repos = &fakeRepos{}
	}
	if opts.Nebulas == nil {
		opts.Nebulas = &fakeNebulas{}
	}
	if opts.Runs == nil {
		opts.Runs = &fakeRuns{}
	}
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv.Routes()
}

func authReq(t *testing.T, method, url, token string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

// --- Notifier tests ---

func TestNotifier(t *testing.T) {
	t.Parallel()

	t.Run("publish reaches subscribed topic", func(t *testing.T) {
		t.Parallel()
		n := NewNotifier()
		ch, unsub := n.Subscribe([]string{"fleet"})
		defer unsub()

		n.Publish(Event{Topic: "runs", Type: "ignored"})
		n.Publish(Event{Topic: "fleet", Type: "nebula_status_changed"})

		select {
		case ev := <-ch:
			if ev.Type != "nebula_status_changed" {
				t.Fatalf("got %q, want nebula_status_changed (runs event should be filtered)", ev.Type)
			}
		case <-time.After(time.Second):
			t.Fatal("no event delivered")
		}
	})

	t.Run("empty topics subscribes to all", func(t *testing.T) {
		t.Parallel()
		n := NewNotifier()
		ch, unsub := n.Subscribe(nil)
		defer unsub()
		n.Publish(Event{Topic: "anything", Type: "x"})
		if ev := <-ch; ev.Type != "x" {
			t.Fatalf("got %q, want x", ev.Type)
		}
	})

	t.Run("slow subscriber overflow drops oldest and emits resync", func(t *testing.T) {
		t.Parallel()
		n := NewNotifier()
		ch, unsub := n.Subscribe(nil)
		defer unsub()

		// Flood well past the buffer without draining.
		for i := 0; i < subBuffer*3; i++ {
			n.Publish(Event{Topic: "fleet", Type: "delta"})
		}

		sawResync := false
		for {
			select {
			case ev := <-ch:
				if ev.Type == ResyncType {
					sawResync = true
				}
			default:
				if !sawResync {
					t.Fatal("expected a resync hint after overflow, got none")
				}
				return
			}
		}
	})

	t.Run("unsubscribe is idempotent and closes channel", func(t *testing.T) {
		t.Parallel()
		n := NewNotifier()
		ch, unsub := n.Subscribe(nil)
		unsub()
		unsub() // must not panic
		if _, ok := <-ch; ok {
			t.Fatal("channel should be closed after unsubscribe")
		}
	})
}

// --- Auth tests ---

func TestAuth(t *testing.T) {
	t.Parallel()
	h := testServer(t, Opts{Enabled: true, Token: "secret"})

	cases := []struct {
		name, token string
		want        int
	}{
		{"missing token", "", http.StatusUnauthorized},
		{"wrong token", "nope", http.StatusUnauthorized},
		{"correct token", "secret", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, authReq(t, "GET", "/api/v1/repos", tc.token))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// --- Feature-flag test ---

func TestFeatureFlagDisabled404(t *testing.T) {
	t.Parallel()
	srv, err := New(Opts{Enabled: false})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h := srv.Routes()
	for _, path := range []string{"/api/v1/repos", "/api/v1/fleet", "/"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, authReq(t, "GET", path, "secret"))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("path %s: status = %d, want 404", path, rec.Code)
		}
	}
}

// --- Handler tests ---

func TestFleetGroupsByLane(t *testing.T) {
	t.Parallel()
	rps := []*repos.Repo{{Path: "/r", Name: "r", Status: "active"}}
	nebs := []*fabric.NebulaSummary{
		{ID: "a", RepoPath: "/r", Status: statusAwaitingApproval},
		{ID: "b", RepoPath: "/r", Status: statusRunning},
		{ID: "c", RepoPath: "/r", Status: statusDone},
	}
	h := testServer(t, Opts{
		Enabled: true,
		Repos:   &fakeRepos{repos: rps},
		Nebulas: &fakeNebulas{summaries: nebs},
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(t, "GET", "/api/v1/fleet", "secret"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var fleet fleetDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &fleet); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(fleet.Repos) != 1 {
		t.Fatalf("repos = %d, want 1", len(fleet.Repos))
	}
	g := fleet.Repos[0]
	if len(g.AwaitingApprove) != 1 || len(g.InFlight) != 1 || len(g.Recent) != 1 {
		t.Fatalf("lanes = (%d,%d,%d), want (1,1,1)", len(g.AwaitingApprove), len(g.InFlight), len(g.Recent))
	}
}

func TestApprovePublishesEvent(t *testing.T) {
	t.Parallel()
	nf := NewNotifier()
	ch, unsub := nf.Subscribe([]string{topicFleet})
	defer unsub()

	fn := &fakeNebulas{}
	h := testServer(t, Opts{Enabled: true, Nebulas: fn, Notifier: nf})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(t, "POST", "/api/v1/nebulas/neb-1/approve", "secret"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if fn.lastID != "neb-1" || fn.lastStatus != statusApproved {
		t.Fatalf("SetStatus got (%q,%q), want (neb-1,approved)", fn.lastID, fn.lastStatus)
	}
	select {
	case ev := <-ch:
		if ev.Type != eventNebulaStatusChange {
			t.Fatalf("event type = %q", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("approve did not publish a fleet event")
	}
}

func TestGetNebulaNotFound(t *testing.T) {
	t.Parallel()
	h := testServer(t, Opts{Enabled: true, Nebulas: &fakeNebulas{}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(t, "GET", "/api/v1/nebulas/missing", "secret"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestRunDetailTrace(t *testing.T) {
	t.Parallel()
	fr := &fakeRuns{
		run:  &fabric.RunRow{ID: "crun-1", State: "running", CurrentNode: "coder"},
		invs: []*fabric.StarInvocationRow{{Seq: 0, Node: "coder", State: "done", CostUSD: 0.34}},
	}
	h := testServer(t, Opts{Enabled: true, Runs: fr})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(t, "GET", "/api/v1/runs/crun-1", "secret"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var detail runDetailDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.ID != "crun-1" || len(detail.Invocations) != 1 || detail.Invocations[0].CostUSD != 0.34 {
		t.Fatalf("unexpected detail: %+v", detail)
	}
}

func TestRuntimeActionUnavailable(t *testing.T) {
	t.Parallel()
	h := testServer(t, Opts{Enabled: true}) // no RuntimeController
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authReq(t, "POST", "/api/v1/runs/crun-1/pause", "secret"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
