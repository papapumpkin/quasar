// Package cockpit_test uses an external test package to avoid an import cycle:
// views imports cockpit for its data types, so cockpit cannot import views.
// Tests that wire the real views renderers live here.
package cockpit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/cockpit"
	"github.com/papapumpkin/quasar/internal/cockpit/views"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// renderNebulaDetail is the NebulaDetailRenderer wired in for tests: calls
// views.NebulaDetailPage and writes the rendered HTML into w.
func renderNebulaDetail(ctx context.Context, w http.ResponseWriter, d cockpit.NebulaDetail) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return views.NebulaDetailPage(d).Render(ctx, w)
}

// TestHandleNebulaDetailRendersPhases confirms that GET /nebulas/{id} returns
// HTTP 200 and includes the seeded phase title in the rendered HTML.
func TestHandleNebulaDetailRendersPhases(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	fab, err := fabric.NewSQLiteFabric(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteFabric: %v", err)
	}
	t.Cleanup(func() { fab.Close() })

	db := fab.DB()
	repoPath := "/repos/papapumpkin/quasar"

	blobs, err := blobstore.New(filepath.Join(dir, "blobs"), db)
	if err != nil {
		t.Fatalf("blobstore.New: %v", err)
	}
	nebulas := fabric.NewNebulaStore(db, blobs)

	nebID, err := nebulas.Insert(ctx, fabric.NebulaRow{
		RepoPath: repoPath,
		Name:     "Add heartbeat refresh",
		Status:   "approved",
	})
	if err != nil {
		t.Fatalf("insert nebula: %v", err)
	}
	if err := nebulas.InsertPhase(ctx, nebID, fabric.PhaseRow{
		ID:    "phase-1",
		Seq:   1,
		Title: "Implement heartbeat timer",
		Body:  "adds a ticker",
	}); err != nil {
		t.Fatalf("insert phase: %v", err)
	}

	s, err := cockpit.New(cockpit.Opts{
		DB:                 db,
		Token:              "t",
		Assets:             cockpit.Assets(),
		RenderPage:         renderPage,
		RenderNebulaDetail: renderNebulaDetail,
	})
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest("GET", "/nebulas/"+nebID, nil)
	r.AddCookie(&http.Cookie{Name: cockpit.CookieName, Value: "t"})
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("code %d, body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Implement heartbeat timer") {
		t.Errorf("expected phase title in body; got (first 512 bytes):\n%s", body[:minLen(len(body), 512)])
	}
	if !strings.Contains(body, "Add heartbeat refresh") {
		t.Errorf("expected nebula title in body")
	}
	if !strings.Contains(body, "phases") {
		t.Errorf("expected 'phases' heading in body")
	}
}

// TestHandleNebulaDetailNotFound confirms GET /nebulas/{unknown-id} returns 404.
func TestHandleNebulaDetailNotFound(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	fab, err := fabric.NewSQLiteFabric(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteFabric: %v", err)
	}
	t.Cleanup(func() { fab.Close() })

	s, err := cockpit.New(cockpit.Opts{
		DB:                 fab.DB(),
		Token:              "t",
		RenderPage:         renderPage,
		RenderNebulaDetail: renderNebulaDetail,
	})
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest("GET", "/nebulas/no-such-nebula", nil)
	r.AddCookie(&http.Cookie{Name: cockpit.CookieName, Value: "t"})
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", w.Code)
	}
}
