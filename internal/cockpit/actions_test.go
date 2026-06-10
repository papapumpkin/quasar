package cockpit

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// openMemDB opens a temp fabric DB (schema migrated) for handler tests that need
// a non-nil DB but don't exercise queries.
func openMemDB(t *testing.T) *sql.DB {
	t.Helper()
	fab, err := fabric.NewSQLiteFabric(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("NewSQLiteFabric: %v", err)
	}
	t.Cleanup(func() { fab.Close() })
	return fab.DB()
}

// fakeRuntime records approve/reject calls for assertions.
type fakeRuntime struct {
	approved string
	rejected string
	reason   string
	err      error
}

func (f *fakeRuntime) Approve(_ context.Context, id string) error {
	f.approved = id
	return f.err
}

func (f *fakeRuntime) Reject(_ context.Context, id, reason string) error {
	f.rejected = id
	f.reason = reason
	return f.err
}

func newActionServer(t *testing.T, rt RuntimeActions) *Server {
	t.Helper()
	s, err := New(Opts{DB: openMemDB(t), Token: "t", Runtime: rt})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestHandleApproveCallsRuntimeAndPublishes(t *testing.T) {
	rt := &fakeRuntime{}
	s := newActionServer(t, rt)

	// Subscribe before the request so we can observe the published event.
	_, ch, cancel := s.notifier.Subscribe([]string{"fleet"})
	defer cancel()

	r := httptest.NewRequest("POST", "/nebulas/neb-1/approve", nil)
	r.SetPathValue("id", "neb-1")
	w := httptest.NewRecorder()
	s.handleApprove(w, r)

	if w.Code != 204 {
		t.Fatalf("code = %d, want 204", w.Code)
	}
	if rt.approved != "neb-1" {
		t.Errorf("approved = %q, want neb-1", rt.approved)
	}
	select {
	case e := <-ch:
		if e.Type != "nebula_status_changed" || e.Data["id"] != "neb-1" {
			t.Errorf("event = %+v, want nebula_status_changed for neb-1", e)
		}
	case <-time.After(time.Second):
		t.Error("expected a published lane-change event")
	}
}

func TestHandleRejectPassesReason(t *testing.T) {
	rt := &fakeRuntime{}
	s := newActionServer(t, rt)

	body := strings.NewReader("reason=not+now")
	r := httptest.NewRequest("POST", "/nebulas/neb-2/reject", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetPathValue("id", "neb-2")
	w := httptest.NewRecorder()
	s.handleReject(w, r)

	if w.Code != 204 {
		t.Fatalf("code = %d, want 204", w.Code)
	}
	if rt.rejected != "neb-2" || rt.reason != "not now" {
		t.Errorf("rejected=%q reason=%q, want neb-2 / 'not now'", rt.rejected, rt.reason)
	}
}

func TestHandleApproveWithoutRuntimeIs503(t *testing.T) {
	s, err := New(Opts{DB: openMemDB(t), Token: "t"}) // no Runtime
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/nebulas/x/approve", nil)
	r.SetPathValue("id", "x")
	w := httptest.NewRecorder()
	s.handleApprove(w, r)
	if w.Code != 503 {
		t.Errorf("code = %d, want 503 when no runtime configured", w.Code)
	}
}
