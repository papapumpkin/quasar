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

// renderRunDetail is the RunDetailRenderer wired in for tests: calls
// views.RunDetailPage and writes the rendered HTML into w.
func renderRunDetail(ctx context.Context, w http.ResponseWriter, d cockpit.RunDetail) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return views.RunDetailPage(d).Render(ctx, w)
}

// TestHandleRunDetailRendersSteps confirms that GET /runs/{id} returns HTTP 200
// and includes the seeded star name in the rendered HTML.
func TestHandleRunDetailRendersSteps(t *testing.T) {
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
	runs := fabric.NewConstellationRunStore(db)

	nebID, err := nebulas.Insert(ctx, fabric.NebulaRow{
		RepoPath: repoPath,
		Name:     "Refactor sensors",
		Status:   "approved",
	})
	if err != nil {
		t.Fatalf("insert nebula: %v", err)
	}
	runID, err := runs.InsertRun(ctx, fabric.RunRow{
		RepoPath:          repoPath,
		NebulaID:          nebID,
		ConstellationName: "coder-reviewer",
		State:             "running",
		CurrentNode:       "coder",
	})
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := runs.InsertStarInvocation(ctx, fabric.StarInvocationRow{
		RunID:    runID,
		Seq:      1,
		Node:     "coder",
		StarName: "superpowers",
		State:    "done",
		Cycle:    1,
	}); err != nil {
		t.Fatalf("insert star invocation: %v", err)
	}

	s, err := cockpit.New(cockpit.Opts{
		DB:              db,
		Token:           "t",
		Assets:          cockpit.Assets(),
		RenderPage:      renderPage,
		RenderRunDetail: renderRunDetail,
	})
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest("GET", "/runs/"+runID, nil)
	r.AddCookie(&http.Cookie{Name: cockpit.CookieName, Value: "t"})
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("code %d, body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "superpowers") {
		t.Errorf("expected star name 'superpowers' in body; got (first 512 bytes):\n%s", body[:minLen(len(body), 512)])
	}
	// Also confirm the run title and step trace header are present.
	if !strings.Contains(body, "Refactor sensors") {
		t.Errorf("expected run title in body")
	}
	if !strings.Contains(body, "step trace") {
		t.Errorf("expected 'step trace' heading in body")
	}
}

// TestHandleRunDetailNotFound confirms GET /runs/{unknown-id} returns 404.
func TestHandleRunDetailNotFound(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	fab, err := fabric.NewSQLiteFabric(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteFabric: %v", err)
	}
	t.Cleanup(func() { fab.Close() })

	s, err := cockpit.New(cockpit.Opts{
		DB:              fab.DB(),
		Token:           "t",
		RenderPage:      renderPage,
		RenderRunDetail: renderRunDetail,
	})
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest("GET", "/runs/no-such-run", nil)
	r.AddCookie(&http.Cookie{Name: cockpit.CookieName, Value: "t"})
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", w.Code)
	}
}

// minLen returns the smaller of a and b. Used to safely trim body previews.
func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}
