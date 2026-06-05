package blobstore

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SweptBlob records a single blob removed by Sweep, for the GC audit log.
type SweptBlob struct {
	Hash      string
	SizeBytes int64
}

// SweepReport summarizes one mark-and-sweep pass.
type SweepReport struct {
	// Scanned is the number of registry rows examined.
	Scanned int
	// Reachable is the number of distinct hashes referenced by some registered
	// (table, column) and therefore kept.
	Reachable int
	// Swept lists every blob deleted because it was unreferenced and old enough.
	Swept []SweptBlob
	// ReclaimedBytes is the sum of Swept blobs' decompressed sizes.
	ReclaimedBytes int64
	// SkippedYoung counts unreferenced blobs kept only because they are newer
	// than minAge (an in-flight write whose reference has not committed yet).
	SkippedYoung int
}

// Sweep performs a mark-and-sweep garbage collection of the blob store. It
// builds the live set by scanning every column registered via RegisterReference,
// then deletes any registry blob that is both unreferenced and older than
// minAge. Blobs newer than minAge are always kept: their reference may be an
// in-flight write that has not yet committed its hash to a row.
//
// The pass is recomputed from scratch every time, so a crash mid-sweep is safe —
// the next sweep simply rebuilds the live set. No persistent GC state is kept.
//
// When dryRun is true, Sweep reports what it would delete without removing any
// blob or registry row.
func (s *Store) Sweep(ctx context.Context, minAge time.Duration, now time.Time, dryRun bool) (*SweepReport, error) {
	seen, err := s.liveSet(ctx)
	if err != nil {
		return nil, err
	}

	report := &SweepReport{Reachable: len(seen)}
	cutoff := now.Add(-minAge)

	walk, err := s.Walk(ctx)
	if err != nil {
		return nil, err
	}

	// Collect deletion candidates first; Delete mutates the blobs table, which
	// must not happen while the Walk query's rows are still open on the single
	// shared connection.
	var toDelete []SweptBlob
	for info := range walk {
		report.Scanned++
		if _, ok := seen[info.Hash]; ok {
			continue
		}
		if info.CreatedAt.After(cutoff) {
			report.SkippedYoung++
			continue
		}
		toDelete = append(toDelete, SweptBlob{Hash: info.Hash, SizeBytes: info.SizeBytes})
	}

	for _, b := range toDelete {
		report.Swept = append(report.Swept, b)
		report.ReclaimedBytes += b.SizeBytes
		if dryRun {
			continue
		}
		if err := s.Delete(ctx, b.Hash); err != nil {
			return report, err
		}
	}
	return report, nil
}

// liveSet returns the set of blob hashes referenced by any registered column.
// Each registered (table, column) is scanned with SELECT DISTINCT; a hash that
// appears in any of them is live. The set is held in memory — 32 bytes per hash,
// so a million blobs is ~32MB.
func (s *Store) liveSet(ctx context.Context) (map[string]struct{}, error) {
	seen := make(map[string]struct{})
	for _, ref := range References() {
		// Table and column come from compile-time RegisterReference calls, never
		// user input, so interpolating them into the query is safe. Guard against
		// accidental injection anyway by rejecting non-identifier characters.
		if !isIdentifier(ref.Table) || !isIdentifier(ref.Column) {
			return nil, fmt.Errorf("blobstore: invalid reference %s.%s", ref.Table, ref.Column)
		}
		query := fmt.Sprintf("SELECT DISTINCT %s FROM %s WHERE %s IS NOT NULL AND %s != ''",
			ref.Column, ref.Table, ref.Column, ref.Column)
		if err := s.collectHashes(ctx, query, seen); err != nil {
			return nil, fmt.Errorf("blobstore: scan %s.%s: %w", ref.Table, ref.Column, err)
		}
	}
	return seen, nil
}

// collectHashes runs query (yielding one TEXT column) and adds each value to seen.
func (s *Store) collectHashes(ctx context.Context, query string, seen map[string]struct{}) error {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close() //nolint:errcheck // iteration cleanup
	for rows.Next() {
		var hash sql.NullString
		if err := rows.Scan(&hash); err != nil {
			return err
		}
		if hash.Valid && hash.String != "" {
			seen[hash.String] = struct{}{}
		}
	}
	return rows.Err()
}

// isIdentifier reports whether s is a safe SQL identifier (letters, digits,
// underscore; non-empty; not starting with a digit). Registered table/column
// names are constants, but validating keeps the dynamic SQL injection-proof.
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
