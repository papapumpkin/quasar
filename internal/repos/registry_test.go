package repos

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// newTestDB opens a fresh fabric database (schema + migrations applied) in a
// temp dir and returns its handle. The fabric owns the connection's lifetime
// and is closed via t.Cleanup.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	fab, err := fabric.NewSQLiteFabric(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteFabric: %v", err)
	}
	t.Cleanup(func() { fab.Close() })
	return fab.DB()
}

// makeGitRepo creates a directory containing a .git subdirectory and returns it.
func makeGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	return dir
}

// insertNebula inserts a nebula row associated with repoPath at the given status.
func insertNebula(t *testing.T, db *sql.DB, id, repoPath, status string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"INSERT INTO nebulas (id, status, repo_path) VALUES (?, ?, ?)", id, status, repoPath)
	if err != nil {
		t.Fatalf("insert nebula: %v", err)
	}
}

func TestRegister(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	reg := New(db)
	dir := makeGitRepo(t)

	t.Run("success with default name", func(t *testing.T) {
		repo, err := reg.Register(ctx, dir, "")
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		if repo.Path != dir {
			t.Errorf("Path = %q, want %q", repo.Path, dir)
		}
		if repo.Name != filepath.Base(dir) {
			t.Errorf("Name = %q, want %q", repo.Name, filepath.Base(dir))
		}
		if repo.Status != StatusActive {
			t.Errorf("Status = %q, want %q", repo.Status, StatusActive)
		}
		if repo.AddedAt.IsZero() {
			t.Error("AddedAt is zero")
		}
	})

	t.Run("already registered", func(t *testing.T) {
		_, err := reg.Register(ctx, dir, "")
		if !errors.Is(err, ErrRepoAlreadyRegistered) {
			t.Errorf("err = %v, want ErrRepoAlreadyRegistered", err)
		}
	})

	t.Run("explicit name", func(t *testing.T) {
		other := makeGitRepo(t)
		repo, err := reg.Register(ctx, other, "myrepo")
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		if repo.Name != "myrepo" {
			t.Errorf("Name = %q, want myrepo", repo.Name)
		}
	})
}

func TestRegister_InvalidPath(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	reg := New(db)

	t.Run("nonexistent", func(t *testing.T) {
		_, err := reg.Register(ctx, filepath.Join(t.TempDir(), "nope"), "")
		if !errors.Is(err, ErrRepoPathInvalid) {
			t.Errorf("err = %v, want ErrRepoPathInvalid", err)
		}
	})

	t.Run("not a git repo", func(t *testing.T) {
		plain := t.TempDir()
		_, err := reg.Register(ctx, plain, "")
		if !errors.Is(err, ErrRepoPathInvalid) {
			t.Errorf("err = %v, want ErrRepoPathInvalid", err)
		}
	})

	t.Run("not a directory", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := reg.Register(ctx, f, "")
		if !errors.Is(err, ErrRepoPathInvalid) {
			t.Errorf("err = %v, want ErrRepoPathInvalid", err)
		}
	})
}

func TestGet_NotRegistered(t *testing.T) {
	ctx := context.Background()
	reg := New(newTestDB(t))
	_, err := reg.Get(ctx, "/no/such/repo")
	if !errors.Is(err, ErrRepoNotRegistered) {
		t.Errorf("err = %v, want ErrRepoNotRegistered", err)
	}
}

func TestList_StatusFilter(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	reg := New(db)

	active := makeGitRepo(t)
	paused := makeGitRepo(t)
	if _, err := reg.Register(ctx, active, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Register(ctx, paused, ""); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetStatus(ctx, paused, StatusPaused); err != nil {
		t.Fatal(err)
	}

	all, err := reg.List(ctx, "")
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List all = %d repos, want 2", len(all))
	}

	onlyActive, err := reg.List(ctx, StatusActive)
	if err != nil {
		t.Fatalf("List active: %v", err)
	}
	if len(onlyActive) != 1 {
		t.Fatalf("List active = %d repos, want 1", len(onlyActive))
	}
	if onlyActive[0].Path != active {
		t.Errorf("active repo = %q, want %q", onlyActive[0].Path, active)
	}
}

func TestSetStatusAndTouch(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	reg := New(db)
	dir := makeGitRepo(t)
	if _, err := reg.Register(ctx, dir, ""); err != nil {
		t.Fatal(err)
	}

	t.Run("pause then resume", func(t *testing.T) {
		if err := reg.SetStatus(ctx, dir, StatusPaused); err != nil {
			t.Fatalf("pause: %v", err)
		}
		repo, _ := reg.Get(ctx, dir)
		if repo.Status != StatusPaused {
			t.Errorf("Status = %q, want paused", repo.Status)
		}
		if err := reg.SetStatus(ctx, dir, StatusActive); err != nil {
			t.Fatalf("resume: %v", err)
		}
		repo, _ = reg.Get(ctx, dir)
		if repo.Status != StatusActive {
			t.Errorf("Status = %q, want active", repo.Status)
		}
	})

	t.Run("touch updates last_seen", func(t *testing.T) {
		before, _ := reg.Get(ctx, dir)
		if err := reg.Touch(ctx, dir); err != nil {
			t.Fatalf("touch: %v", err)
		}
		after, _ := reg.Get(ctx, dir)
		if after.LastSeenAt.Before(before.LastSeenAt) {
			t.Error("LastSeenAt went backwards")
		}
	})

	t.Run("touch unknown repo", func(t *testing.T) {
		if err := reg.Touch(ctx, "/no/such/repo"); !errors.Is(err, ErrRepoNotRegistered) {
			t.Errorf("err = %v, want ErrRepoNotRegistered", err)
		}
	})
}

func TestUnregister(t *testing.T) {
	ctx := context.Background()

	t.Run("not registered", func(t *testing.T) {
		reg := New(newTestDB(t))
		if err := reg.Unregister(ctx, makeGitRepo(t), false); !errors.Is(err, ErrRepoNotRegistered) {
			t.Errorf("err = %v, want ErrRepoNotRegistered", err)
		}
	})

	t.Run("blocked by active nebula", func(t *testing.T) {
		db := newTestDB(t)
		reg := New(db)
		dir := makeGitRepo(t)
		if _, err := reg.Register(ctx, dir, ""); err != nil {
			t.Fatal(err)
		}
		insertNebula(t, db, "neb-active", dir, "draft")

		err := reg.Unregister(ctx, dir, false)
		if !errors.Is(err, ErrRepoActiveNebulas) {
			t.Errorf("err = %v, want ErrRepoActiveNebulas", err)
		}
	})

	t.Run("succeeds when only terminal nebulas", func(t *testing.T) {
		db := newTestDB(t)
		reg := New(db)
		dir := makeGitRepo(t)
		if _, err := reg.Register(ctx, dir, ""); err != nil {
			t.Fatal(err)
		}
		insertNebula(t, db, "neb-done", dir, "merged")

		if err := reg.Unregister(ctx, dir, false); err != nil {
			t.Fatalf("Unregister: %v", err)
		}
		repo, _ := reg.Get(ctx, dir)
		if repo.Status != StatusRemoved {
			t.Errorf("Status = %q, want removed", repo.Status)
		}
	})

	t.Run("force orphans active nebulas", func(t *testing.T) {
		db := newTestDB(t)
		reg := New(db)
		dir := makeGitRepo(t)
		if _, err := reg.Register(ctx, dir, ""); err != nil {
			t.Fatal(err)
		}
		insertNebula(t, db, "neb-active", dir, "draft")
		insertNebula(t, db, "neb-terminal", dir, "merged")

		if err := reg.Unregister(ctx, dir, true); err != nil {
			t.Fatalf("Unregister force: %v", err)
		}

		repo, _ := reg.Get(ctx, dir)
		if repo.Status != StatusRemoved {
			t.Errorf("repo Status = %q, want removed", repo.Status)
		}
		if got := nebulaStatus(t, db, "neb-active"); got != "orphaned" {
			t.Errorf("neb-active status = %q, want orphaned", got)
		}
		if got := nebulaStatus(t, db, "neb-terminal"); got != "merged" {
			t.Errorf("neb-terminal status = %q, want merged (unchanged)", got)
		}
	})
}

// nebulaStatus reads a nebula's current status by id.
func nebulaStatus(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var status string
	if err := db.QueryRowContext(context.Background(),
		"SELECT status FROM nebulas WHERE id = ?", id).Scan(&status); err != nil {
		t.Fatalf("read nebula status: %v", err)
	}
	return status
}
