package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/repos"
)

// blobRoot returns the content-addressed blob store root (~/.quasar/blobs),
// shared across every repo a persistent Quasar process operates on.
func blobRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".quasar", "blobs"), nil
}

// resolveRepoForCWD finds the registered repo that owns cwd: the repo whose path
// equals cwd or is an ancestor directory. Parents are walked so `nebula apply`
// works from any subdirectory of a registered repo. It returns an actionable
// error when no registered repo contains cwd.
func resolveRepoForCWD(ctx context.Context, reg *repos.Registry, cwd string) (*repos.Repo, error) {
	dir, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("resolve cwd %q: %w", cwd, err)
	}
	for {
		repo, getErr := reg.Get(ctx, dir)
		if getErr == nil {
			return repo, nil
		}
		if !errors.Is(getErr, repos.ErrRepoNotRegistered) {
			return nil, getErr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("no registered repo for %s: run `quasar repo register <path>` first", cwd)
		}
		dir = parent
	}
}

// importNebulaToStore parses a nebula directory and inserts it (and its phases)
// into the SQLite-backed store under repoPath. The on-disk files are untouched;
// this only writes to the database and blobstore. It returns the new nebula id.
func importNebulaToStore(ctx context.Context, store nebula.Inserter, nebulaDir, repoPath string) (string, error) {
	n, err := nebula.Load(nebulaDir)
	if err != nil {
		return "", fmt.Errorf("load nebula %q: %w", nebulaDir, err)
	}
	return nebula.ImportNebula(ctx, store, n, repoPath)
}

// applyImportToSQLite is the production wiring for `quasar nebula apply`: it opens
// the shared fabric DB and blobstore, resolves the registered repo that owns the
// current working directory, and imports the nebula at nebulaDir into SQLite,
// returning the new nebula id. It errors if no repo is registered for the CWD.
func applyImportToSQLite(ctx context.Context, nebulaDir string) (string, error) {
	dbPath := fabricDBPath()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return "", fmt.Errorf("create db dir: %w", err)
	}
	fab, err := fabric.NewSQLiteFabric(ctx, dbPath)
	if err != nil {
		return "", fmt.Errorf("open fabric: %w", err)
	}
	defer fab.Close() //nolint:errcheck // close error is non-fatal for a CLI op

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	repo, err := resolveRepoForCWD(ctx, repos.New(fab.DB()), cwd)
	if err != nil {
		return "", err
	}

	root, err := blobRoot()
	if err != nil {
		return "", err
	}
	blobs, err := blobstore.New(root, fab.DB())
	if err != nil {
		return "", fmt.Errorf("open blobstore: %w", err)
	}
	store := fabric.NewNebulaStore(fab.DB(), blobs)
	return importNebulaToStore(ctx, store, nebulaDir, repo.Path)
}
