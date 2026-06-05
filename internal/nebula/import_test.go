package nebula

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// newImportStore builds a NebulaStore backed by a fresh migrated SQLite
// database and blobstore, registering a repo so imported nebulas reference a
// real repo_path.
func newImportStore(t *testing.T) (*fabric.NebulaStore, string) {
	t.Helper()
	dir := t.TempDir()
	fab, err := fabric.NewSQLiteFabric(context.Background(), filepath.Join(dir, "test.db"))
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
	return fabric.NewNebulaStore(fab.DB(), blobs), repoPath
}

func sampleNebula() *Nebula {
	return &Nebula{
		Dir: "/some/.nebulas/feat",
		Manifest: Manifest{
			Nebula: Info{Name: "Feat Nebula", Description: "does things"},
			Defaults: Defaults{
				Type:     "task",
				Priority: 2,
				Labels:   []string{"quasar"},
			},
			Execution: Execution{MaxWorkers: 1, MaxReviewCycles: 5},
			Context:   Context{Repo: "github.com/papapumpkin/quasar", Goals: []string{"g1"}},
		},
		Phases: []PhaseSpec{
			{ID: "p1", Title: "First", Type: "task", Priority: 2, Body: "## Problem\nfix it"},
			{ID: "p2", Title: "Second", Type: "task", Priority: 2, DependsOn: []string{"p1"}, Body: "## Problem\nthen this"},
		},
	}
}

func TestImportNebula(t *testing.T) {
	ctx := context.Background()
	store, repo := newImportStore(t)

	n := sampleNebula()
	id, err := ImportNebula(ctx, store, n, repo)
	if err != nil {
		t.Fatalf("ImportNebula: %v", err)
	}

	got, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	t.Run("nebula fields map from manifest", func(t *testing.T) {
		if got.Name != "Feat Nebula" {
			t.Errorf("name = %q, want %q", got.Name, "Feat Nebula")
		}
		if got.Description != "does things" {
			t.Errorf("description = %q, want %q", got.Description, "does things")
		}
		if got.RepoPath != repo {
			t.Errorf("repo_path = %q, want %q", got.RepoPath, repo)
		}
		if got.Status != "draft" {
			t.Errorf("status = %q, want draft", got.Status)
		}
	})

	t.Run("manifest blocks rendered to TOML", func(t *testing.T) {
		if !strings.Contains(got.DefaultsTOML, "priority = 2") {
			t.Errorf("defaults_toml = %q, want priority", got.DefaultsTOML)
		}
		if !strings.Contains(got.ExecutionTOML, "max_review_cycles = 5") {
			t.Errorf("execution_toml = %q, want max_review_cycles", got.ExecutionTOML)
		}
		if !strings.Contains(got.ContextTOML, "github.com/papapumpkin/quasar") {
			t.Errorf("context_toml = %q, want repo", got.ContextTOML)
		}
	})

	t.Run("phases preserved in order with bodies", func(t *testing.T) {
		if len(got.Phases) != 2 {
			t.Fatalf("phases = %d, want 2", len(got.Phases))
		}
		if got.Phases[0].ID != "p1" || got.Phases[1].ID != "p2" {
			t.Errorf("phase order = [%s, %s], want [p1, p2]", got.Phases[0].ID, got.Phases[1].ID)
		}
		if got.Phases[0].Body != "## Problem\nfix it" {
			t.Errorf("phase body = %q, want round-trip", got.Phases[0].Body)
		}
		if !strings.Contains(got.Phases[1].FrontmatterTOML, "p2") {
			t.Errorf("frontmatter_toml = %q, want id p2", got.Phases[1].FrontmatterTOML)
		}
	})
}

func TestImportNebulaSourceAttribution(t *testing.T) {
	ctx := context.Background()
	store, repo := newImportStore(t)

	n := sampleNebula()
	n.SourceName = "github"
	n.SourceID = "papapumpkin/quasar#42"

	id, err := ImportNebula(ctx, store, n, repo)
	if err != nil {
		t.Fatalf("ImportNebula: %v", err)
	}
	got, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SourceName != "github" || got.SourceID != "papapumpkin/quasar#42" {
		t.Errorf("source = %q/%q, want github/papapumpkin/quasar#42", got.SourceName, got.SourceID)
	}
}

func TestImportNebulaManifestSourceFallback(t *testing.T) {
	ctx := context.Background()
	store, repo := newImportStore(t)

	n := sampleNebula()
	// Source only present on the manifest (not the top-level Nebula fields).
	n.Manifest.Nebula.SourceName = "github"
	n.Manifest.Nebula.SourceID = "papapumpkin/quasar#7"

	id, err := ImportNebula(ctx, store, n, repo)
	if err != nil {
		t.Fatalf("ImportNebula: %v", err)
	}
	got, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SourceName != "github" || got.SourceID != "papapumpkin/quasar#7" {
		t.Errorf("source = %q/%q, want github fallback from manifest", got.SourceName, got.SourceID)
	}
}

func TestImportNebulaEmptyRepoPath(t *testing.T) {
	ctx := context.Background()
	store, _ := newImportStore(t)

	if _, err := ImportNebula(ctx, store, sampleNebula(), ""); err == nil {
		t.Fatal("ImportNebula with empty repoPath: want error, got nil")
	}
}
