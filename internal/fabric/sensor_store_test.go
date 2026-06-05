package fabric

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

// newSensorStores builds cursor + event stores over a fresh migrated database
// and registers a repo so the foreign-key columns reference a real row.
func newSensorStores(t *testing.T) (*SensorCursorStore, *SensorEventStore, string) {
	t.Helper()
	dir := t.TempDir()
	fab, err := NewSQLiteFabric(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteFabric: %v", err)
	}
	t.Cleanup(func() { fab.Close() })

	repoPath := "/repos/example"
	now := time.Now().Unix()
	if _, err := fab.DB().ExecContext(context.Background(),
		"INSERT INTO repos (path, name, status, added_at, updated_at, last_seen_at) VALUES (?, ?, 'active', ?, ?, ?)",
		repoPath, "example", now, now, now); err != nil {
		t.Fatalf("insert repo: %v", err)
	}
	return NewSensorCursorStore(fab.DB()), NewSensorEventStore(fab.DB()), repoPath
}

func TestSensorCursorStore(t *testing.T) {
	ctx := context.Background()
	cursors, _, repo := newSensorStores(t)

	t.Run("missing cursor is nil", func(t *testing.T) {
		got, err := cursors.Get(ctx, repo, "github_issues")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got != nil {
			t.Errorf("got %q, want nil for unset cursor", got)
		}
	})

	t.Run("set then get round-trips", func(t *testing.T) {
		want := json.RawMessage(`{"last_issue_number":42}`)
		if err := cursors.Set(ctx, repo, "github_issues", want); err != nil {
			t.Fatalf("Set: %v", err)
		}
		got, err := cursors.Get(ctx, repo, "github_issues")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if string(got) != string(want) {
			t.Errorf("cursor = %s, want %s", got, want)
		}
	})

	t.Run("set overwrites in place", func(t *testing.T) {
		if err := cursors.Set(ctx, repo, "github_issues", json.RawMessage(`{"last_issue_number":99}`)); err != nil {
			t.Fatalf("Set: %v", err)
		}
		got, err := cursors.Get(ctx, repo, "github_issues")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if string(got) != `{"last_issue_number":99}` {
			t.Errorf("cursor = %s, want the overwritten value", got)
		}
	})

	t.Run("nil cursor round-trips to nil", func(t *testing.T) {
		if err := cursors.Set(ctx, repo, "nil_sensor", nil); err != nil {
			t.Fatalf("Set: %v", err)
		}
		got, err := cursors.Get(ctx, repo, "nil_sensor")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got != nil {
			t.Errorf("got %q, want nil", got)
		}
	})
}

func TestSensorEventStoreInsertDedup(t *testing.T) {
	ctx := context.Background()
	_, events, repo := newSensorStores(t)
	ts := time.Unix(1_700_000_000, 0)

	id1, isNew, err := events.Insert(ctx, repo, "github_issues", "owner/repo#1", ts)
	if err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	if !isNew {
		t.Error("first Insert isNew = false, want true")
	}

	// Re-inserting the same external id is a no-op (dedup) and returns the same
	// row id with isNew=false — this is what makes restart-after-crash safe.
	id2, isNew, err := events.Insert(ctx, repo, "github_issues", "owner/repo#1", ts)
	if err != nil {
		t.Fatalf("duplicate Insert: %v", err)
	}
	if isNew {
		t.Error("duplicate Insert isNew = true, want false")
	}
	if id2 != id1 {
		t.Errorf("duplicate Insert id = %d, want %d (same row)", id2, id1)
	}

	// A different external id is a distinct, new event.
	_, isNew, err = events.Insert(ctx, repo, "github_issues", "owner/repo#2", ts)
	if err != nil {
		t.Fatalf("second Insert: %v", err)
	}
	if !isNew {
		t.Error("distinct Insert isNew = false, want true")
	}
}

func TestSensorEventStoreMarkProcessedAndUnprocessed(t *testing.T) {
	ctx := context.Background()
	_, events, repo := newSensorStores(t)
	ts := time.Unix(1_700_000_000, 0)

	id1, _, err := events.Insert(ctx, repo, "github_issues", "owner/repo#1", ts)
	if err != nil {
		t.Fatalf("Insert #1: %v", err)
	}
	if _, _, err := events.Insert(ctx, repo, "github_issues", "owner/repo#2", ts); err != nil {
		t.Fatalf("Insert #2: %v", err)
	}

	unprocessed, err := events.Unprocessed(ctx, repo, "github_issues")
	if err != nil {
		t.Fatalf("Unprocessed: %v", err)
	}
	if len(unprocessed) != 2 {
		t.Fatalf("got %d unprocessed, want 2", len(unprocessed))
	}
	if unprocessed[0].ExternalID != "owner/repo#1" {
		t.Errorf("unprocessed ordering: first = %q, want owner/repo#1", unprocessed[0].ExternalID)
	}

	if err := events.MarkProcessed(ctx, id1, "nebula-1-foo"); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}

	unprocessed, err = events.Unprocessed(ctx, repo, "github_issues")
	if err != nil {
		t.Fatalf("Unprocessed after mark: %v", err)
	}
	if len(unprocessed) != 1 || unprocessed[0].ExternalID != "owner/repo#2" {
		t.Errorf("after MarkProcessed, unprocessed = %+v, want only owner/repo#2", unprocessed)
	}
}

func TestSensorEventStoreMarkProcessedMissing(t *testing.T) {
	ctx := context.Background()
	_, events, _ := newSensorStores(t)
	if err := events.MarkProcessed(ctx, 999, "nebula-x"); err == nil {
		t.Fatal("MarkProcessed for missing id: want error, got nil")
	}
}
