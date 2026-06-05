package fabric

import (
	"context"
	"fmt"
)

// NebulaRecord is one row in the nebulas table: the provenance and status of a
// nebula known to this fabric database. Ticket-seeded drafts created by
// `quasar nebula new` record their originating source here so a later review
// surface (web UI) can list and act on them. Manually authored nebulas are not
// persisted here.
type NebulaRecord struct {
	ID         string // nebula directory name; primary key
	SourceType string // e.g. "ticket"; distinguishes from future origins
	SourceName string // integration name, e.g. "github"
	SourceID   string // adapter-canonical id, e.g. "papapumpkin/quasar#42"
	Path       string // on-disk path to the nebula directory
	Status     string // lifecycle status, e.g. "draft"
}

// InsertNebula records a new nebula row. created_at and updated_at are set by
// the database. The ID must be unique — re-pulling a ticket produces a new
// directory name (with a numeric suffix) and therefore a distinct row.
func (f *SQLiteFabric) InsertNebula(ctx context.Context, rec NebulaRecord) error {
	const q = `
		INSERT INTO nebulas (id, source_type, source_name, source_id, path, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
	if _, err := f.db.ExecContext(ctx, q,
		rec.ID, rec.SourceType, rec.SourceName, rec.SourceID, rec.Path, rec.Status,
	); err != nil {
		return fmt.Errorf("fabric: insert nebula %q: %w", rec.ID, err)
	}
	return nil
}
