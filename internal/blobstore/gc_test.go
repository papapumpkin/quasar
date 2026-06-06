package blobstore

import (
	"context"
	"testing"
	"time"
)

// sweepNow is a fixed reference instant for deterministic age math in sweeps.
var sweepNow = time.Unix(1_700_000_000, 0).UTC()

// newSweepStore returns a store plus a registered reference table (gc_refs) so
// Sweep has a live-set source to scan. The table name is constant across tests,
// so the single registration stays valid for every store the suite creates.
func newSweepStore(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	if _, err := s.db.ExecContext(context.Background(),
		"CREATE TABLE gc_refs (blob_hash TEXT)"); err != nil {
		t.Fatalf("create gc_refs: %v", err)
	}
	RegisterReference("gc_refs", "blob_hash")
	return s
}

// addRef records hash as a live reference in gc_refs.
func addRef(t *testing.T, s *Store, hash string) {
	t.Helper()
	if _, err := s.db.ExecContext(context.Background(),
		"INSERT INTO gc_refs (blob_hash) VALUES (?)", hash); err != nil {
		t.Fatalf("add ref: %v", err)
	}
}

// ageBlob backdates a blob's created_at so the minAge cutoff can be exercised.
func ageBlob(t *testing.T, s *Store, hash string, createdAt time.Time) {
	t.Helper()
	if _, err := s.db.ExecContext(context.Background(),
		"UPDATE blobs SET created_at = ? WHERE hash = ?", createdAt.Unix(), hash); err != nil {
		t.Fatalf("age blob: %v", err)
	}
}

func TestSweep(t *testing.T) {
	ctx := context.Background()
	minAge := time.Hour

	t.Run("keeps referenced blobs and reaps unreferenced old ones", func(t *testing.T) {
		s := newSweepStore(t)

		live, err := s.Put(ctx, []byte("referenced content"))
		if err != nil {
			t.Fatalf("Put live: %v", err)
		}
		addRef(t, s, live)
		ageBlob(t, s, live, sweepNow.Add(-48*time.Hour)) // old, but referenced

		orphan, err := s.Put(ctx, []byte("orphan content"))
		if err != nil {
			t.Fatalf("Put orphan: %v", err)
		}
		ageBlob(t, s, orphan, sweepNow.Add(-48*time.Hour))

		report, err := s.Sweep(ctx, minAge, sweepNow, false)
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if report.Reachable != 1 {
			t.Errorf("Reachable = %d, want 1", report.Reachable)
		}
		if len(report.Swept) != 1 || report.Swept[0].Hash != orphan {
			t.Errorf("Swept = %+v, want only the orphan", report.Swept)
		}
		if report.ReclaimedBytes != int64(len("orphan content")) {
			t.Errorf("ReclaimedBytes = %d, want %d", report.ReclaimedBytes, len("orphan content"))
		}
		if s.Has(ctx, orphan) {
			t.Error("orphan still on disk")
		}
		if !s.Has(ctx, live) {
			t.Error("referenced blob was reaped")
		}
	})

	t.Run("keeps unreferenced blobs newer than minAge", func(t *testing.T) {
		s := newSweepStore(t)
		young, err := s.Put(ctx, []byte("just written"))
		if err != nil {
			t.Fatalf("Put: %v", err)
		}
		ageBlob(t, s, young, sweepNow.Add(-1*time.Minute)) // newer than minAge

		report, err := s.Sweep(ctx, minAge, sweepNow, false)
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if report.SkippedYoung != 1 {
			t.Errorf("SkippedYoung = %d, want 1", report.SkippedYoung)
		}
		if len(report.Swept) != 0 {
			t.Errorf("Swept = %+v, want none", report.Swept)
		}
		if !s.Has(ctx, young) {
			t.Error("young orphan was reaped")
		}
	})

	t.Run("dry run reports but deletes nothing", func(t *testing.T) {
		s := newSweepStore(t)
		orphan, err := s.Put(ctx, []byte("orphan"))
		if err != nil {
			t.Fatalf("Put: %v", err)
		}
		ageBlob(t, s, orphan, sweepNow.Add(-48*time.Hour))

		report, err := s.Sweep(ctx, minAge, sweepNow, true)
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if len(report.Swept) != 1 {
			t.Errorf("dry run Swept = %d, want 1 (reported)", len(report.Swept))
		}
		if !s.Has(ctx, orphan) {
			t.Error("dry run deleted a blob")
		}
	})

	t.Run("scanned count covers all walked blobs", func(t *testing.T) {
		s := newSweepStore(t)
		for _, c := range [][]byte{[]byte("a"), []byte("b"), []byte("c")} {
			h, err := s.Put(ctx, c)
			if err != nil {
				t.Fatalf("Put: %v", err)
			}
			ageBlob(t, s, h, sweepNow.Add(-48*time.Hour))
		}
		report, err := s.Sweep(ctx, minAge, sweepNow, true)
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if report.Scanned != 3 {
			t.Errorf("Scanned = %d, want 3", report.Scanned)
		}
	})
}

func TestIsIdentifier(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"phases":       true,
		"body_blob":    true,
		"_underscore":  true,
		"Table1":       true,
		"":             false,
		"1leading":     false,
		"has space":    false,
		"drop;table":   false,
		"semi;":        false,
		"with-dash":    false,
		"quote'inject": false,
	}
	for in, want := range cases {
		if got := isIdentifier(in); got != want {
			t.Errorf("isIdentifier(%q) = %v, want %v", in, got, want)
		}
	}
}
