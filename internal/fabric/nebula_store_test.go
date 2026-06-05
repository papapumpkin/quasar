package fabric

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/blobstore"
)

// newNebulaStore builds a NebulaStore over a fresh migrated database plus a
// blobstore, and registers a repo so inserts reference a real repo_path.
func newNebulaStore(t *testing.T) (*NebulaStore, string) {
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

	blobs, err := blobstore.New(filepath.Join(dir, "blobs"), fab.DB())
	if err != nil {
		t.Fatalf("blobstore.New: %v", err)
	}
	return NewNebulaStore(fab.DB(), blobs), repoPath
}

func TestNebulaStoreInsert(t *testing.T) {
	ctx := context.Background()
	store, repo := newNebulaStore(t)

	t.Run("defaults status to draft", func(t *testing.T) {
		id, err := store.Insert(ctx, NebulaRow{RepoPath: repo, Name: "My Feature"})
		if err != nil {
			t.Fatalf("Insert: %v", err)
		}
		neb, err := store.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if neb.Status != "draft" {
			t.Errorf("status = %q, want draft", neb.Status)
		}
		if neb.RepoPath != repo {
			t.Errorf("repo_path = %q, want %q", neb.RepoPath, repo)
		}
	})

	t.Run("explicit status is honored", func(t *testing.T) {
		id, err := store.Insert(ctx, NebulaRow{RepoPath: repo, Name: "Seeded", Status: "awaiting_approval"})
		if err != nil {
			t.Fatalf("Insert: %v", err)
		}
		neb, err := store.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if neb.Status != "awaiting_approval" {
			t.Errorf("status = %q, want awaiting_approval", neb.Status)
		}
	})
}

func TestNebulaStorePhases(t *testing.T) {
	ctx := context.Background()
	store, repo := newNebulaStore(t)

	id, err := store.Insert(ctx, NebulaRow{RepoPath: repo, Name: "With Phases"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	longBody := make([]rune, 700)
	for i := range longBody {
		longBody[i] = 'x'
	}
	phase := PhaseRow{ID: "p1", Seq: 1, Title: "First", Body: string(longBody), FrontmatterTOML: "id = \"p1\""}
	if err := store.InsertPhase(ctx, id, phase); err != nil {
		t.Fatalf("InsertPhase: %v", err)
	}

	t.Run("body round-trips from blobstore via Get", func(t *testing.T) {
		neb, err := store.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if len(neb.Phases) != 1 {
			t.Fatalf("phases = %d, want 1", len(neb.Phases))
		}
		if neb.Phases[0].Body != string(longBody) {
			t.Errorf("body round-trip mismatch (len %d, want %d)", len(neb.Phases[0].Body), len(longBody))
		}
	})

	t.Run("preview is truncated to 500 runes at insert", func(t *testing.T) {
		var p string
		err := store.db.QueryRowContext(ctx,
			"SELECT body_preview FROM phases WHERE nebula_id = ? AND id = ?", id, "p1").Scan(&p)
		if err != nil {
			t.Fatalf("query preview: %v", err)
		}
		if len([]rune(p)) != previewLen {
			t.Errorf("preview len = %d, want %d", len([]rune(p)), previewLen)
		}
	})

	t.Run("UpdatePhaseResult stores diff in blobstore", func(t *testing.T) {
		res := PhaseResult{Status: "done", ResultTOML: "cost = 1.0", Diff: []byte("diff --git a b")}
		if err := store.UpdatePhaseResult(ctx, id, "p1", res); err != nil {
			t.Fatalf("UpdatePhaseResult: %v", err)
		}
		var hash string
		if err := store.db.QueryRowContext(ctx,
			"SELECT diff_blob_hash FROM phases WHERE nebula_id = ? AND id = ?", id, "p1").Scan(&hash); err != nil {
			t.Fatalf("query diff hash: %v", err)
		}
		got, err := store.blobs.Get(ctx, hash)
		if err != nil {
			t.Fatalf("blobs.Get: %v", err)
		}
		if string(got) != "diff --git a b" {
			t.Errorf("diff = %q, want %q", got, "diff --git a b")
		}
	})
}

func TestNebulaStoreList(t *testing.T) {
	ctx := context.Background()
	store, repo := newNebulaStore(t)

	a, _ := store.Insert(ctx, NebulaRow{RepoPath: repo, Name: "Alpha", Status: "draft"})
	if _, err := store.Insert(ctx, NebulaRow{RepoPath: repo, Name: "Beta", Status: "merged"}); err != nil {
		t.Fatalf("Insert beta: %v", err)
	}

	t.Run("filter by status", func(t *testing.T) {
		got, err := store.List(ctx, ListFilter{RepoPath: repo, Status: "draft"})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 1 || got[0].ID != a {
			t.Fatalf("List draft = %+v, want only %s", got, a)
		}
	})

	t.Run("no filter returns all for repo", func(t *testing.T) {
		got, err := store.List(ctx, ListFilter{RepoPath: repo})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("List all = %d, want 2", len(got))
		}
	})
}

func TestNebulaStoreStatusTransitions(t *testing.T) {
	ctx := context.Background()
	store, repo := newNebulaStore(t)

	id, err := store.Insert(ctx, NebulaRow{RepoPath: repo, Name: "Lifecycle"})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	t.Run("SetStatus updates", func(t *testing.T) {
		if err := store.SetStatus(ctx, id, "running"); err != nil {
			t.Fatalf("SetStatus: %v", err)
		}
		neb, _ := store.Get(ctx, id)
		if neb.Status != "running" {
			t.Errorf("status = %q, want running", neb.Status)
		}
	})

	t.Run("AppendMasterReview with status", func(t *testing.T) {
		if err := store.AppendMasterReview(ctx, id, MasterReviewRow{ReviewTOML: "verdict = \"ship\"", Status: "approved"}); err != nil {
			t.Fatalf("AppendMasterReview: %v", err)
		}
		neb, _ := store.Get(ctx, id)
		if neb.Status != "approved" {
			t.Errorf("status = %q, want approved", neb.Status)
		}
	})

	t.Run("MarkForGC sets gc_at", func(t *testing.T) {
		if err := store.MarkForGC(ctx, id, time.Hour); err != nil {
			t.Fatalf("MarkForGC: %v", err)
		}
		var gcAt int64
		if err := store.db.QueryRowContext(ctx, "SELECT gc_at FROM nebulas WHERE id = ?", id).Scan(&gcAt); err != nil {
			t.Fatalf("query gc_at: %v", err)
		}
		if gcAt <= time.Now().Unix() {
			t.Errorf("gc_at = %d, want future", gcAt)
		}
	})

	t.Run("unknown id returns ErrNebulaNotFound", func(t *testing.T) {
		if err := store.SetStatus(ctx, "nope", "x"); !errors.Is(err, ErrNebulaNotFound) {
			t.Errorf("err = %v, want ErrNebulaNotFound", err)
		}
		if _, err := store.Get(ctx, "nope"); !errors.Is(err, ErrNebulaNotFound) {
			t.Errorf("Get err = %v, want ErrNebulaNotFound", err)
		}
	})
}
