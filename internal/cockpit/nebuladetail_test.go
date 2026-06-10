package cockpit

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// TestLoadNebulaDetail seeds a fabric database with one nebula, two phases, and
// one constellation run, then asserts LoadNebulaDetail returns the header,
// phases in seq-ascending order, and the run.
func TestLoadNebulaDetail(t *testing.T) {
	t.Parallel()

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
		RepoPath:    repoPath,
		Name:        "Refactor sensors layer",
		Description: "Rename integrations to sensors",
		SourceName:  "github",
		SourceID:    "papapumpkin/quasar#99",
		SourceURL:   "https://github.com/papapumpkin/quasar/issues/99",
		Status:      "approved",
	})
	if err != nil {
		t.Fatalf("insert nebula: %v", err)
	}

	// Insert two phases in order; verify ORDER BY seq is honored.
	if err := nebulas.InsertPhase(ctx, nebID, fabric.PhaseRow{
		ID:    "phase-b",
		Seq:   2,
		Title: "Wire sensor adapters",
		Body:  "body b",
	}); err != nil {
		t.Fatalf("insert phase 2: %v", err)
	}
	if err := nebulas.InsertPhase(ctx, nebID, fabric.PhaseRow{
		ID:    "phase-a",
		Seq:   1,
		Title: "Rename integrations package",
		Body:  "body a",
	}); err != nil {
		t.Fatalf("insert phase 1: %v", err)
	}

	runID, err := runs.InsertRun(ctx, fabric.RunRow{
		RepoPath:          repoPath,
		NebulaID:          nebID,
		ConstellationName: "coder-reviewer",
		State:             "done",
		CurrentNode:       "reviewer",
		StepIndex:         3,
		Cycle:             1,
	})
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}

	d, found, err := LoadNebulaDetail(ctx, db, nebID)
	if err != nil {
		t.Fatalf("LoadNebulaDetail: %v", err)
	}
	if !found {
		t.Fatal("LoadNebulaDetail: expected found=true")
	}

	// --- nebula header ---
	if d.Nebula.ID != nebID {
		t.Errorf("nebula id = %q, want %q", d.Nebula.ID, nebID)
	}
	if d.Nebula.Title != "Refactor sensors layer" {
		t.Errorf("nebula title = %q", d.Nebula.Title)
	}
	if d.Nebula.Status != "approved" {
		t.Errorf("nebula status = %q, want approved", d.Nebula.Status)
	}
	if d.Nebula.SourceName != "github" {
		t.Errorf("source name = %q, want github", d.Nebula.SourceName)
	}
	if d.Nebula.IssueURL != "https://github.com/papapumpkin/quasar/issues/99" {
		t.Errorf("issue url = %q", d.Nebula.IssueURL)
	}
	if d.Description != "Rename integrations to sensors" {
		t.Errorf("description = %q", d.Description)
	}

	// --- phases ordered by seq ---
	if len(d.Phases) != 2 {
		t.Fatalf("phases = %d, want 2", len(d.Phases))
	}
	if d.Phases[0].Seq != 1 {
		t.Errorf("phases[0].Seq = %d, want 1", d.Phases[0].Seq)
	}
	if d.Phases[0].ID != "phase-a" {
		t.Errorf("phases[0].ID = %q, want phase-a", d.Phases[0].ID)
	}
	if d.Phases[0].Title != "Rename integrations package" {
		t.Errorf("phases[0].Title = %q", d.Phases[0].Title)
	}
	if d.Phases[0].Status != "pending" {
		t.Errorf("phases[0].Status = %q, want pending", d.Phases[0].Status)
	}
	if d.Phases[1].Seq != 2 {
		t.Errorf("phases[1].Seq = %d, want 2", d.Phases[1].Seq)
	}
	if d.Phases[1].ID != "phase-b" {
		t.Errorf("phases[1].ID = %q, want phase-b", d.Phases[1].ID)
	}

	// --- runs ---
	if len(d.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(d.Runs))
	}
	if d.Runs[0].ID != runID {
		t.Errorf("run id = %q, want %q", d.Runs[0].ID, runID)
	}
	if d.Runs[0].ConstellationName != "coder-reviewer" {
		t.Errorf("run constellation = %q, want coder-reviewer", d.Runs[0].ConstellationName)
	}
	if d.Runs[0].State != "done" {
		t.Errorf("run state = %q, want done", d.Runs[0].State)
	}
}

// TestLoadNebulaDetailNotFound confirms LoadNebulaDetail returns found=false for
// an unknown nebula ID without returning an error.
func TestLoadNebulaDetailNotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()

	fab, err := fabric.NewSQLiteFabric(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteFabric: %v", err)
	}
	t.Cleanup(func() { fab.Close() })

	_, found, err := LoadNebulaDetail(ctx, fab.DB(), "no-such-nebula")
	if err != nil {
		t.Fatalf("LoadNebulaDetail: unexpected error: %v", err)
	}
	if found {
		t.Error("expected found=false for unknown nebula id")
	}
}
