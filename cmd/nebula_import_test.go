package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/repos"
)

// newImportFixture builds a migrated SQLite DB with a blobstore-backed
// NebulaStore and a repos registry over the same database.
func newImportFixture(t *testing.T) (*fabric.NebulaStore, *repos.Registry, *fabric.SQLiteFabric) {
	t.Helper()
	dir := t.TempDir()
	fab, err := fabric.NewSQLiteFabric(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteFabric: %v", err)
	}
	t.Cleanup(func() { fab.Close() })

	blobs, err := blobstore.New(filepath.Join(dir, "blobs"), fab.DB())
	if err != nil {
		t.Fatalf("blobstore.New: %v", err)
	}
	return fabric.NewNebulaStore(fab.DB(), blobs), repos.New(fab.DB()), fab
}

// registerRepoRow inserts an active repo row directly, bypassing the .git
// validation reg.Register requires (not relevant to these tests).
func registerRepoRow(t *testing.T, fab *fabric.SQLiteFabric, path string) {
	t.Helper()
	now := time.Now().Unix()
	if _, err := fab.DB().ExecContext(context.Background(),
		"INSERT INTO repos (path, name, status, added_at, updated_at, last_seen_at) VALUES (?, ?, 'active', ?, ?, ?)",
		path, filepath.Base(path), now, now, now); err != nil {
		t.Fatalf("insert repo: %v", err)
	}
}

func TestResolveRepoForCWD(t *testing.T) {
	ctx := context.Background()
	_, reg, fab := newImportFixture(t)

	repoDir := t.TempDir()
	registerRepoRow(t, fab, repoDir)

	t.Run("exact path", func(t *testing.T) {
		repo, err := resolveRepoForCWD(ctx, reg, repoDir)
		if err != nil {
			t.Fatalf("resolveRepoForCWD: %v", err)
		}
		if repo.Path != repoDir {
			t.Errorf("path = %q, want %q", repo.Path, repoDir)
		}
	})

	t.Run("subdirectory walks up to repo", func(t *testing.T) {
		sub := filepath.Join(repoDir, "internal", "pkg")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		repo, err := resolveRepoForCWD(ctx, reg, sub)
		if err != nil {
			t.Fatalf("resolveRepoForCWD: %v", err)
		}
		if repo.Path != repoDir {
			t.Errorf("path = %q, want %q", repo.Path, repoDir)
		}
	})

	t.Run("unregistered cwd errors", func(t *testing.T) {
		if _, err := resolveRepoForCWD(ctx, reg, t.TempDir()); err == nil {
			t.Fatal("want error for unregistered cwd, got nil")
		}
	})
}

func TestImportNebulaToStore(t *testing.T) {
	ctx := context.Background()
	store, _, fab := newImportFixture(t)

	repoDir := t.TempDir()
	registerRepoRow(t, fab, repoDir)

	nebulaDir := filepath.Join(t.TempDir(), "feat")
	if err := os.MkdirAll(nebulaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := "[nebula]\nname = \"Test Feat\"\ndescription = \"a feature\"\n"
	if err := os.WriteFile(filepath.Join(nebulaDir, "nebula.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	phase := "+++\nid = \"p1\"\ntitle = \"First\"\n+++\n\n## Problem\nfix it\n"
	if err := os.WriteFile(filepath.Join(nebulaDir, "p1.md"), []byte(phase), 0o644); err != nil {
		t.Fatalf("write phase: %v", err)
	}

	id, err := importNebulaToStore(ctx, store, nebulaDir, repoDir)
	if err != nil {
		t.Fatalf("importNebulaToStore: %v", err)
	}

	got, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Test Feat" || got.RepoPath != repoDir {
		t.Errorf("nebula = %q/%q, want %q/%q", got.Name, got.RepoPath, "Test Feat", repoDir)
	}
	if len(got.Phases) != 1 || got.Phases[0].ID != "p1" {
		t.Fatalf("phases = %+v, want one phase p1", got.Phases)
	}
	if got.Phases[0].Body != "## Problem\nfix it" {
		t.Errorf("phase body = %q, want round-trip", got.Phases[0].Body)
	}
}
