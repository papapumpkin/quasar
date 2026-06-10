package cockpit

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// TestLoadFleet seeds a fabric database with a single repo carrying one
// awaiting-approval nebula, one running constellation_run (joined to its
// nebula), and one recent (merged) nebula, then asserts LoadFleet returns one
// repo with exactly one card in each of the three lanes.
func TestLoadFleet(t *testing.T) {
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
	runs := fabric.NewConstellationRunStore(db)

	// Awaiting-approval nebula -> Awaiting lane.
	awaitingID, err := nebulas.Insert(ctx, fabric.NebulaRow{
		RepoPath:   repoPath,
		Name:       "Fix flaky sensor poll dedup",
		SourceName: "github",
		SourceID:   "papapumpkin/quasar#42",
		Status:     "awaiting_approval",
	})
	if err != nil {
		t.Fatalf("insert awaiting nebula: %v", err)
	}

	// A separate nebula carrying the running constellation_run -> InFlight lane.
	runningNebID, err := nebulas.Insert(ctx, fabric.NebulaRow{
		RepoPath: repoPath,
		Name:     "Heartbeat refresh + poison guard",
		Status:   "approved",
	})
	if err != nil {
		t.Fatalf("insert running nebula: %v", err)
	}
	runID, err := runs.InsertRun(ctx, fabric.RunRow{
		RepoPath:          repoPath,
		NebulaID:          runningNebID,
		ConstellationName: "coder-reviewer",
		State:             "running",
		CurrentNode:       "review",
		StepIndex:         2,
	})
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"UPDATE constellation_runs SET step_count = ? WHERE id = ?", 4, runID); err != nil {
		t.Fatalf("set step_count: %v", err)
	}

	// Recent (merged) nebula -> Recent lane.
	if _, err := nebulas.Insert(ctx, fabric.NebulaRow{
		RepoPath: repoPath,
		Name:     "Close perimeter bypasses",
		Status:   "merged",
	}); err != nil {
		t.Fatalf("insert recent nebula: %v", err)
	}

	fleet, err := LoadFleet(ctx, db)
	if err != nil {
		t.Fatalf("LoadFleet: %v", err)
	}

	if len(fleet.Repos) != 1 {
		t.Fatalf("repos = %d, want 1", len(fleet.Repos))
	}
	repo := fleet.Repos[0]
	if repo.Path != repoPath {
		t.Errorf("repo path = %q, want %q", repo.Path, repoPath)
	}

	if len(repo.Awaiting) != 1 {
		t.Fatalf("awaiting = %d, want 1", len(repo.Awaiting))
	}
	if repo.Awaiting[0].ID != awaitingID {
		t.Errorf("awaiting id = %q, want %q", repo.Awaiting[0].ID, awaitingID)
	}
	if repo.Awaiting[0].Title != "Fix flaky sensor poll dedup" {
		t.Errorf("awaiting title = %q", repo.Awaiting[0].Title)
	}
	if repo.Awaiting[0].SourceName != "github" {
		t.Errorf("awaiting source name = %q, want github", repo.Awaiting[0].SourceName)
	}

	if len(repo.InFlight) != 1 {
		t.Fatalf("in-flight = %d, want 1", len(repo.InFlight))
	}
	rc := repo.InFlight[0]
	if rc.ID != runID {
		t.Errorf("run id = %q, want %q", rc.ID, runID)
	}
	if rc.Title != "Heartbeat refresh + poison guard" {
		t.Errorf("run title = %q", rc.Title)
	}
	if rc.ConstellationName != "coder-reviewer" {
		t.Errorf("constellation = %q, want coder-reviewer", rc.ConstellationName)
	}
	if rc.CurrentNode != "review" {
		t.Errorf("current node = %q, want review", rc.CurrentNode)
	}
	if rc.StepIndex != 2 || rc.StepCount != 4 {
		t.Errorf("step %d/%d, want 2/4", rc.StepIndex, rc.StepCount)
	}

	if len(repo.Recent) != 1 {
		t.Fatalf("recent = %d, want 1", len(repo.Recent))
	}
	if repo.Recent[0].Title != "Close perimeter bypasses" {
		t.Errorf("recent title = %q", repo.Recent[0].Title)
	}
	if repo.Recent[0].Status != "merged" {
		t.Errorf("recent status = %q, want merged", repo.Recent[0].Status)
	}
}
