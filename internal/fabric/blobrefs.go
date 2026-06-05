package fabric

import "github.com/papapumpkin/quasar/internal/blobstore"

// init registers the blob-hash columns owned by this package's migrations with
// the blobstore so the mark-and-sweep GC counts them as live references. These
// must stay in lockstep with the *_blob_hash columns declared in
// migrations/002_nebulas_to_sqlite.sql; TestBlobHashColumnsRegistered fails CI
// if a new blob column is added without a matching registration here.
func init() {
	blobstore.RegisterReference("phases", "body_blob_hash")
	blobstore.RegisterReference("phases", "diff_blob_hash")
}
