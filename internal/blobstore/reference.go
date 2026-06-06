package blobstore

import "sort"

// Reference identifies a database column that stores a blob hash. The
// mark-and-sweep GC computes the live set by scanning every registered column;
// a *_blob_hash column that is not registered here is invisible to the sweep,
// so its blobs would be reclaimed while still referenced — silent data loss.
//
// Registration is therefore not optional bookkeeping but a load-bearing
// invariant. An arch test (TestBlobHashColumnsRegistered) asserts that the set
// of registered references exactly matches the set of *_blob_hash columns in the
// migrations, so a new blob column cannot ship without a matching registration.
type Reference struct {
	Table  string
	Column string
}

// registeredReferences holds every blob reference declared via
// RegisterReference. It is populated from package init() functions at startup
// and never mutated afterward, so it needs no synchronization for reads.
var registeredReferences []Reference

// RegisterReference records that table.column stores a blob hash that the GC
// must treat as a live reference. It is called from the init() of the package
// that owns the column's migration. Duplicate registrations are collapsed so
// repeated init() runs (e.g. under `go test`) stay idempotent.
func RegisterReference(table, column string) {
	for _, r := range registeredReferences {
		if r.Table == table && r.Column == column {
			return
		}
	}
	registeredReferences = append(registeredReferences, Reference{Table: table, Column: column})
}

// References returns a sorted copy of all registered blob references. The GC
// and the arch test consume this to learn which columns hold live blob hashes.
func References() []Reference {
	out := make([]Reference, len(registeredReferences))
	copy(out, registeredReferences)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Table != out[j].Table {
			return out[i].Table < out[j].Table
		}
		return out[i].Column < out[j].Column
	})
	return out
}
