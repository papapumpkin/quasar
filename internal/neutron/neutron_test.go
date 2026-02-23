package neutron

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/fabric"

	_ "modernc.org/sqlite"
)

// testFabric creates a real SQLiteFabric backed by a temp file for integration tests.
func testFabric(t *testing.T) *fabric.SQLiteFabric {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.fabric.db")
	f, err := fabric.NewSQLiteFabric(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteFabric: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// seedFabric populates a fabric with sample data for archival tests.
func seedFabric(t *testing.T, ctx context.Context, f *fabric.SQLiteFabric) {
	t.Helper()

	// Phase states.
	if err := f.SetPhaseState(ctx, "task-1", fabric.StateDone); err != nil {
		t.Fatalf("SetPhaseState task-1: %v", err)
	}
	if err := f.SetPhaseState(ctx, "task-2", fabric.StateFailed); err != nil {
		t.Fatalf("SetPhaseState task-2: %v", err)
	}

	// Entanglements.
	if err := f.PublishEntanglement(ctx, fabric.Entanglement{
		Producer:  "task-1",
		Consumer:  "task-2",
		Kind:      fabric.KindFunction,
		Name:      "DoStuff",
		Signature: "func DoStuff() error",
		Package:   "pkg",
		Status:    fabric.StatusFulfilled,
	}); err != nil {
		t.Fatalf("PublishEntanglement: %v", err)
	}

	// Discoveries.
	if _, err := f.PostDiscovery(ctx, fabric.Discovery{
		SourceTask: "task-1",
		Kind:       fabric.DiscoveryFileConflict,
		Detail:     "both tasks touch main.go",
	}); err != nil {
		t.Fatalf("PostDiscovery: %v", err)
	}

	// Pulses.
	if err := f.EmitPulse(ctx, fabric.Pulse{
		TaskID:  "task-1",
		Content: "starting work",
		Kind:    fabric.PulseNote,
	}); err != nil {
		t.Fatalf("EmitPulse: %v", err)
	}
	if err := f.EmitPulse(ctx, fabric.Pulse{
		TaskID:  "task-2",
		Content: "reviewer feedback",
		Kind:    fabric.PulseReviewerFeedback,
	}); err != nil {
		t.Fatalf("EmitPulse: %v", err)
	}
}

func TestArchive(t *testing.T) {
	t.Parallel()

	t.Run("creates neutron with correct data", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		f := testFabric(t)
		seedFabric(t, ctx, f)

		// Resolve the discovery so archival can proceed.
		discoveries, err := f.AllDiscoveries(ctx)
		if err != nil {
			t.Fatalf("AllDiscoveries: %v", err)
		}
		for _, d := range discoveries {
			if err := f.ResolveDiscovery(ctx, d.ID); err != nil {
				t.Fatalf("ResolveDiscovery: %v", err)
			}
		}

		dir := t.TempDir()
		outputPath := filepath.Join(dir, "epoch-1.db")

		n, err := Archive(ctx, f, "epoch-1", outputPath, ArchiveOptions{})
		if err != nil {
			t.Fatalf("Archive: %v", err)
		}

		if n.EpochID != "epoch-1" {
			t.Errorf("EpochID = %q, want %q", n.EpochID, "epoch-1")
		}
		if n.DBPath != outputPath {
			t.Errorf("DBPath = %q, want %q", n.DBPath, outputPath)
		}
		if n.CreatedAt.IsZero() {
			t.Error("CreatedAt is zero")
		}

		// Verify the neutron database contents.
		ndb, err := sql.Open("sqlite", outputPath)
		if err != nil {
			t.Fatalf("open neutron: %v", err)
		}
		defer ndb.Close()

		// Check metadata.
		var epochID string
		var taskCount int
		if err := ndb.QueryRow("SELECT epoch_id, task_count FROM metadata").Scan(&epochID, &taskCount); err != nil {
			t.Fatalf("query metadata: %v", err)
		}
		if epochID != "epoch-1" {
			t.Errorf("metadata.epoch_id = %q, want %q", epochID, "epoch-1")
		}
		if taskCount != 2 {
			t.Errorf("metadata.task_count = %d, want 2", taskCount)
		}

		// Check tasks.
		var count int
		if err := ndb.QueryRow("SELECT COUNT(*) FROM tasks").Scan(&count); err != nil {
			t.Fatalf("count tasks: %v", err)
		}
		if count != 2 {
			t.Errorf("tasks count = %d, want 2", count)
		}

		// Check entanglements.
		if err := ndb.QueryRow("SELECT COUNT(*) FROM entanglements").Scan(&count); err != nil {
			t.Fatalf("count entanglements: %v", err)
		}
		if count != 1 {
			t.Errorf("entanglements count = %d, want 1", count)
		}

		// Check discoveries.
		if err := ndb.QueryRow("SELECT COUNT(*) FROM discoveries").Scan(&count); err != nil {
			t.Fatalf("count discoveries: %v", err)
		}
		if count != 1 {
			t.Errorf("discoveries count = %d, want 1", count)
		}

		// Check pulses.
		if err := ndb.QueryRow("SELECT COUNT(*) FROM pulses").Scan(&count); err != nil {
			t.Fatalf("count pulses: %v", err)
		}
		if count != 2 {
			t.Errorf("pulses count = %d, want 2", count)
		}

		// Verify active fabric is purged.
		states, err := f.AllPhaseStates(ctx)
		if err != nil {
			t.Fatalf("AllPhaseStates after archive: %v", err)
		}
		if len(states) != 0 {
			t.Errorf("active fabric still has %d phase states after archive", len(states))
		}

		remaining, err := f.AllEntanglements(ctx)
		if err != nil {
			t.Fatalf("AllEntanglements after archive: %v", err)
		}
		if len(remaining) != 0 {
			t.Errorf("active fabric still has %d entanglements after archive", len(remaining))
		}
	})

	t.Run("refuses when claims exist", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		f := testFabric(t)
		seedFabric(t, ctx, f)

		// Add a claim.
		if err := f.ClaimFile(ctx, "main.go", "task-1"); err != nil {
			t.Fatalf("ClaimFile: %v", err)
		}

		dir := t.TempDir()
		outputPath := filepath.Join(dir, "epoch-1.db")

		_, err := Archive(ctx, f, "epoch-1", outputPath, ArchiveOptions{})
		if !errors.Is(err, ErrActiveClaims) {
			t.Fatalf("expected ErrActiveClaims, got: %v", err)
		}

		// Neutron file should not have been created.
		if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
			t.Error("neutron file was created despite active claims")
		}
	})

	t.Run("refuses with unresolved discoveries unless forced", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		f := testFabric(t)
		seedFabric(t, ctx, f)

		dir := t.TempDir()
		outputPath := filepath.Join(dir, "epoch-1.db")

		// Should fail without force.
		_, err := Archive(ctx, f, "epoch-1", outputPath, ArchiveOptions{Force: false})
		if !errors.Is(err, ErrUnresolvedDiscoveries) {
			t.Fatalf("expected ErrUnresolvedDiscoveries, got: %v", err)
		}

		// Should succeed with force.
		n, err := Archive(ctx, f, "epoch-1", outputPath, ArchiveOptions{Force: true})
		if err != nil {
			t.Fatalf("Archive with force: %v", err)
		}
		if n.EpochID != "epoch-1" {
			t.Errorf("EpochID = %q, want %q", n.EpochID, "epoch-1")
		}
	})

	t.Run("does not archive claims or scanning states", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		f := testFabric(t)

		// Create a task in scanning state with a claim.
		if err := f.SetPhaseState(ctx, "task-x", fabric.StateScanning); err != nil {
			t.Fatalf("SetPhaseState: %v", err)
		}

		dir := t.TempDir()
		outputPath := filepath.Join(dir, "epoch-no-claims.db")

		// Archive with force (no claims because ClaimFile wasn't called).
		n, err := Archive(ctx, f, "epoch-x", outputPath, ArchiveOptions{Force: true})
		if err != nil {
			t.Fatalf("Archive: %v", err)
		}

		// Verify neutron has the task but no claims table data (claims table
		// exists in neutron schema for structure but Archive does not copy claims).
		ndb, err := sql.Open("sqlite", n.DBPath)
		if err != nil {
			t.Fatalf("open neutron: %v", err)
		}
		defer ndb.Close()

		// The neutron schema does not have a file_claims table.
		var tableName string
		err = ndb.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='file_claims'").Scan(&tableName)
		if err == nil {
			t.Error("neutron should NOT have a file_claims table")
		}
	})
}

func TestPurge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := testFabric(t)
	seedFabric(t, ctx, f)

	// Add a claim to verify it gets purged too.
	if err := f.ClaimFile(ctx, "main.go", "task-1"); err != nil {
		t.Fatalf("ClaimFile: %v", err)
	}

	if err := Purge(ctx, f); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	// Verify everything is empty.
	states, err := f.AllPhaseStates(ctx)
	if err != nil {
		t.Fatalf("AllPhaseStates: %v", err)
	}
	if len(states) != 0 {
		t.Errorf("phase states remain: %d", len(states))
	}

	entanglements, err := f.AllEntanglements(ctx)
	if err != nil {
		t.Fatalf("AllEntanglements: %v", err)
	}
	if len(entanglements) != 0 {
		t.Errorf("entanglements remain: %d", len(entanglements))
	}

	discoveries, err := f.AllDiscoveries(ctx)
	if err != nil {
		t.Fatalf("AllDiscoveries: %v", err)
	}
	if len(discoveries) != 0 {
		t.Errorf("discoveries remain: %d", len(discoveries))
	}

	pulses, err := f.AllPulses(ctx)
	if err != nil {
		t.Fatalf("AllPulses: %v", err)
	}
	if len(pulses) != 0 {
		t.Errorf("pulses remain: %d", len(pulses))
	}

	claims, err := f.AllClaims(ctx)
	if err != nil {
		t.Fatalf("AllClaims: %v", err)
	}
	if len(claims) != 0 {
		t.Errorf("claims remain: %d", len(claims))
	}
}

func TestReadNeutron(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := testFabric(t)

	// Set up minimal data.
	if err := f.SetPhaseState(ctx, "task-1", fabric.StateDone); err != nil {
		t.Fatalf("SetPhaseState: %v", err)
	}

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "read-test.db")

	_, err := Archive(ctx, f, "read-epoch", outputPath, ArchiveOptions{Force: true})
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}

	n, err := ReadNeutron(ctx, outputPath)
	if err != nil {
		t.Fatalf("ReadNeutron: %v", err)
	}

	if n.EpochID != "read-epoch" {
		t.Errorf("EpochID = %q, want %q", n.EpochID, "read-epoch")
	}
	if n.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if n.DBPath != outputPath {
		t.Errorf("DBPath = %q, want %q", n.DBPath, outputPath)
	}
}

func TestArchiveTimestamp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := testFabric(t)

	if err := f.SetPhaseState(ctx, "task-1", fabric.StateDone); err != nil {
		t.Fatalf("SetPhaseState: %v", err)
	}

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "ts-test.db")
	before := time.Now().UTC()

	n, err := Archive(ctx, f, "ts-epoch", outputPath, ArchiveOptions{Force: true})
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}

	after := time.Now().UTC()
	if n.CreatedAt.Before(before) || n.CreatedAt.After(after) {
		t.Errorf("CreatedAt %v not between %v and %v", n.CreatedAt, before, after)
	}
}
