// Package cockpit_test uses an external test package to avoid an import cycle:
// the views package (cockpit/views) imports cockpit for its data types, so
// cockpit itself cannot import views. Tests that need the real views renderer
// live here and wire views.RenderPage into Opts.RenderPage.
package cockpit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/cockpit"
	"github.com/papapumpkin/quasar/internal/cockpit/views"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// renderPage is the PageRenderer wired in for tests: calls views.Page and
// writes the rendered HTML into w.
func renderPage(ctx context.Context, w http.ResponseWriter, f cockpit.Fleet) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return views.Page(f).Render(ctx, w)
}

// TestHandleFleetRendersBoard confirms that GET / returns HTTP 200 and includes
// the "Awaiting" lane header text rendered from the seeded fleet data.
func TestHandleFleetRendersBoard(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	fab, err := fabric.NewSQLiteFabric(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteFabric: %v", err)
	}
	t.Cleanup(func() { fab.Close() })

	db := fab.DB()
	repoPath := "/repos/papapumpkin/quasar"
	now := time.Now().Unix()
	if _, err := db.ExecContext(ctx,
		"INSERT INTO repos (path, name, status, added_at, updated_at, last_seen_at) VALUES (?, ?, 'active', ?, ?, ?)",
		repoPath, "quasar", now, now, now); err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	blobs, err := blobstore.New(filepath.Join(dir, "blobs"), db)
	if err != nil {
		t.Fatalf("blobstore.New: %v", err)
	}
	nebulas := fabric.NewNebulaStore(db, blobs)
	if _, err := nebulas.Insert(ctx, fabric.NebulaRow{
		RepoPath:   repoPath,
		Name:       "Fix flaky sensor poll dedup",
		SourceName: "github",
		SourceID:   "papapumpkin/quasar#42",
		Status:     "awaiting_approval",
	}); err != nil {
		t.Fatalf("insert awaiting nebula: %v", err)
	}

	s, err := cockpit.New(cockpit.Opts{
		DB:         db,
		Token:      "t",
		Assets:     cockpit.Assets(),
		RenderPage: renderPage,
	})
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: cockpit.CookieName, Value: "t"})
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("code %d, body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Awaiting") {
		t.Errorf("expected a lane header in body; got:\n%s", body[:min(len(body), 512)])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
