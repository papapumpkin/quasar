package cockpit

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// seedRunningRun creates a temp fabric DB with one repo + one running
// constellation_run and returns the DB and the run id.
func seedRunningRun(t *testing.T) (*Server, string) {
	t.Helper()
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
	nebID, err := fabric.NewNebulaStore(db, blobs).Insert(ctx, fabric.NebulaRow{
		RepoPath: repoPath, Name: "Live run", Status: "approved",
	})
	if err != nil {
		t.Fatalf("insert nebula: %v", err)
	}
	runID, err := fabric.NewConstellationRunStore(db).InsertRun(ctx, fabric.RunRow{
		RepoPath: repoPath, NebulaID: nebID, ConstellationName: "coder-reviewer",
		State: "running", CurrentNode: "review", StepIndex: 2,
	})
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	// A Server whose RenderRun writes a recognizable fragment carrying the id.
	s, err := New(Opts{
		DB:    db,
		Token: "t",
		RenderRun: func(_ context.Context, w io.Writer, rc RunCard) error {
			_, err := fmt.Fprintf(w, "<div id=\"run-%s\">RENDERED %s</div>", rc.ID, rc.ID)
			return err
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, runID
}

func TestSSEEmitsFragmentOnPublish(t *testing.T) {
	s, runID := seedRunningRun(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("GET", "/sse", nil).WithContext(ctx)
	rec := httptest.NewRecorder() // ResponseRecorder implements http.Flusher

	done := make(chan struct{})
	go func() {
		s.handleSSE(rec, r)
		close(done)
	}()

	// Give the handler a moment to subscribe, then publish a run event.
	time.Sleep(30 * time.Millisecond)
	s.notifier.Publish(Event{Topic: "runs", Type: "step_completed",
		Data: map[string]any{"run_id": runID}})
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	body := rec.Body.String()
	if !strings.Contains(body, "event: datastar-merge-fragments") {
		t.Errorf("body missing merge-fragments event:\n%s", body)
	}
	if !strings.Contains(body, "run-"+runID) {
		t.Errorf("body missing rendered run fragment for %s:\n%s", runID, body)
	}
}

func TestSSEReloadsOnLaneChange(t *testing.T) {
	s, _ := seedRunningRun(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("GET", "/sse", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() { s.handleSSE(rec, r); close(done) }()

	time.Sleep(30 * time.Millisecond)
	s.notifier.Publish(Event{Topic: "fleet", Type: "nebula_status_changed",
		Data: map[string]any{"id": "x"}})
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if !strings.Contains(rec.Body.String(), "datastar-execute-script") {
		t.Errorf("lane change should emit a reload script:\n%s", rec.Body.String())
	}
}
