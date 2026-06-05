package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// runNebulaUndelete clears a nebula's deleted_at, rescuing it from the GC while
// it is still within its grace window. It is the escape hatch for an accidental
// GC mark; once the row is hard-deleted (grace elapsed) recovery is impossible.
func runNebulaUndelete(cmd *cobra.Command, args []string) error {
	id := args[0]

	dbPath := fabricDBPath()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}
	fab, err := fabric.NewSQLiteFabric(cmd.Context(), dbPath)
	if err != nil {
		return fmt.Errorf("open fabric: %w", err)
	}
	defer fab.Close() //nolint:errcheck // close error is non-fatal for a CLI op

	root, err := blobRoot()
	if err != nil {
		return err
	}
	blobs, err := blobstore.New(root, fab.DB())
	if err != nil {
		return fmt.Errorf("open blobstore: %w", err)
	}
	store := fabric.NewNebulaStore(fab.DB(), blobs)

	switch err := store.Undelete(cmd.Context(), id); {
	case err == nil:
		fmt.Fprintf(os.Stderr, "restored nebula %s\n", id)
		return nil
	case errors.Is(err, fabric.ErrNebulaNotFound):
		return fmt.Errorf("nebula %s not found — it may have been hard-deleted past its grace window", id)
	case errors.Is(err, fabric.ErrNotDeleted):
		return fmt.Errorf("nebula %s is not marked for deletion; nothing to restore", id)
	default:
		return err
	}
}
