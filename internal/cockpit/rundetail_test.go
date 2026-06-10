package cockpit

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// TestLoadRunDetail seeds a fabric database with one run and two
// star_invocations, then asserts that LoadRunDetail returns the run header and
// the steps in seq-ascending order.
func TestLoadRunDetail(t *testing.T) {
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
		RepoPath: repoPath,
		Name:     "Add sensors layer",
		Status:   "approved",
	})
	if err != nil {
		t.Fatalf("insert nebula: %v", err)
	}
	runID, err := runs.InsertRun(ctx, fabric.RunRow{
		RepoPath:          repoPath,
		NebulaID:          nebID,
		ConstellationName: "coder-reviewer",
		State:             "done",
		CurrentNode:       "reviewer",
		StepIndex:         4,
		Cycle:             1,
	})
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}

	// Seed two star invocations: seq 1 (coder) and seq 2 (reviewer). Insert in
	// order to verify ORDER BY seq is honored.
	if _, err := runs.InsertStarInvocation(ctx, fabric.StarInvocationRow{
		RunID:            runID,
		Seq:              1,
		Node:             "coder",
		StarName:         "feature-dev",
		State:            "done",
		Cycle:            1,
		CostUSD:          0.042,
		DurationMs:       3200,
		RationalePreview: "implemented the sensor adapter",
	}); err != nil {
		t.Fatalf("insert invocation 1: %v", err)
	}
	if _, err := runs.InsertStarInvocation(ctx, fabric.StarInvocationRow{
		RunID:      runID,
		Seq:        2,
		Node:       "reviewer",
		StarName:   "code-review",
		State:      "done",
		Cycle:      1,
		CostUSD:    0.018,
		DurationMs: 1100,
	}); err != nil {
		t.Fatalf("insert invocation 2: %v", err)
	}

	d, found, err := LoadRunDetail(ctx, db, runID)
	if err != nil {
		t.Fatalf("LoadRunDetail: %v", err)
	}
	if !found {
		t.Fatal("LoadRunDetail: expected found=true")
	}

	// --- run header ---
	if d.Run.ID != runID {
		t.Errorf("run id = %q, want %q", d.Run.ID, runID)
	}
	if d.Run.Title != "Add sensors layer" {
		t.Errorf("run title = %q, want %q", d.Run.Title, "Add sensors layer")
	}
	if d.Run.ConstellationName != "coder-reviewer" {
		t.Errorf("constellation = %q, want coder-reviewer", d.Run.ConstellationName)
	}
	if d.Run.State != "done" {
		t.Errorf("state = %q, want done", d.Run.State)
	}
	if d.Run.Cycle != 1 {
		t.Errorf("cycle = %d, want 1", d.Run.Cycle)
	}

	// --- step trace ---
	if len(d.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(d.Steps))
	}
	// Verify seq-ascending order.
	if d.Steps[0].Seq != 1 {
		t.Errorf("steps[0].Seq = %d, want 1", d.Steps[0].Seq)
	}
	if d.Steps[1].Seq != 2 {
		t.Errorf("steps[1].Seq = %d, want 2", d.Steps[1].Seq)
	}

	s0 := d.Steps[0]
	if s0.Node != "coder" {
		t.Errorf("steps[0].Node = %q, want coder", s0.Node)
	}
	if s0.Star != "feature-dev" {
		t.Errorf("steps[0].Star = %q, want feature-dev", s0.Star)
	}
	if s0.State != "done" {
		t.Errorf("steps[0].State = %q, want done", s0.State)
	}
	if s0.RationalePreview != "implemented the sensor adapter" {
		t.Errorf("steps[0].RationalePreview = %q", s0.RationalePreview)
	}
	if s0.CostUSD <= 0 {
		t.Errorf("steps[0].CostUSD = %f, want > 0", s0.CostUSD)
	}
	if s0.DurationMS != 3200 {
		t.Errorf("steps[0].DurationMS = %d, want 3200", s0.DurationMS)
	}

	s1 := d.Steps[1]
	if s1.Node != "reviewer" {
		t.Errorf("steps[1].Node = %q, want reviewer", s1.Node)
	}
	if s1.Star != "code-review" {
		t.Errorf("steps[1].Star = %q, want code-review", s1.Star)
	}
}

// TestLoadRunDetailNotFound confirms LoadRunDetail returns found=false for an
// unknown run ID without returning an error.
func TestLoadRunDetailNotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()

	fab, err := fabric.NewSQLiteFabric(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteFabric: %v", err)
	}
	t.Cleanup(func() { fab.Close() })

	_, found, err := LoadRunDetail(ctx, fab.DB(), "no-such-run")
	if err != nil {
		t.Fatalf("LoadRunDetail: unexpected error: %v", err)
	}
	if found {
		t.Error("expected found=false for unknown run id")
	}
}
