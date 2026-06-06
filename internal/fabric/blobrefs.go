package fabric

import "github.com/papapumpkin/quasar/internal/blobstore"

// init registers the blob-hash columns owned by this package's migrations with
// the blobstore so the mark-and-sweep GC counts them as live references. These
// must stay in lockstep with every *_blob_hash column declared across the
// migrations (both CREATE TABLE bodies and ALTER TABLE … ADD COLUMN additions);
// TestBlobHashColumnsRegistered fails CI if a new blob column is added without a
// matching registration here.
func init() {
	// migration 002 (CREATE TABLE phases)
	blobstore.RegisterReference("phases", "body_blob_hash")
	blobstore.RegisterReference("phases", "diff_blob_hash")
	// migration 004 (ALTER TABLE star_invocations ADD COLUMN)
	blobstore.RegisterReference("star_invocations", "rationale_blob_hash")
}
