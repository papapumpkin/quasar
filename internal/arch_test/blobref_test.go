package arch_test

import (
	"sort"
	"testing"
)

// TestBlobHashColumnsRegistered verifies that the set of *_blob_hash columns
// declared in the SQL migrations is exactly the set registered with the
// blobstore via blobstore.RegisterReference. The mark-and-sweep GC builds its
// live set from the registered references; an unregistered blob column is
// invisible to the sweep, so its blobs would be reclaimed while still in use —
// silent data loss after the next GC. Registering a column for a table that no
// migration declares is equally a bug (a typo that protects nothing).
func TestBlobHashColumnsRegistered(t *testing.T) {
	t.Parallel()

	migrationCols := keySet(blobHashColumnsInMigrations(t))
	registeredCols := keySet(registeredBlobRefs(t))

	for col := range migrationCols {
		if !registeredCols[col] {
			t.Errorf("migration declares blob column %q but no blobstore.RegisterReference registers it; "+
				"the GC sweep would miss it and reclaim live blobs", col)
		}
	}
	for col := range registeredCols {
		if !migrationCols[col] {
			t.Errorf("blobstore.RegisterReference registers %q but no migration declares such a *_blob_hash column", col)
		}
	}

	if len(migrationCols) == 0 {
		t.Error("found no *_blob_hash columns in migrations; the scanner is likely broken")
	}
}

// keySet collapses a slice of blobColumns into a set keyed by table.column.
func keySet(cols []blobColumn) map[string]bool {
	set := make(map[string]bool, len(cols))
	for _, c := range cols {
		set[c.String()] = true
	}
	return set
}

// TestBlobHashScannerSanity guards the scanner itself: it must find the known
// blob columns declared both in CREATE TABLE bodies (phases.*) and via
// ALTER TABLE … ADD COLUMN (star_invocations.rationale_blob_hash). A scanner
// that silently matches nothing — or that misses the ALTER form — would make
// TestBlobHashColumnsRegistered vacuously pass.
func TestBlobHashScannerSanity(t *testing.T) {
	t.Parallel()

	cols := blobHashColumnsInMigrations(t)
	var got []string
	for _, c := range cols {
		got = append(got, c.String())
	}
	sort.Strings(got)

	for _, want := range []string{"phases.body_blob_hash", "phases.diff_blob_hash", "star_invocations.rationale_blob_hash"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected scanner to find %q; got %v", want, got)
		}
	}
}
