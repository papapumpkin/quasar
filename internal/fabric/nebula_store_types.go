package fabric

import "time"

// NebulaRow is the input to NebulaStore.Insert: the manifest-derived fields of
// a new nebula. Manifest blocks are pre-rendered to TOML by the caller. Status
// defaults to "draft" when empty.
type NebulaRow struct {
	RepoPath      string
	Name          string
	Description   string
	SourceName    string
	SourceID      string
	SourceURL     string
	DefaultsTOML  string
	ExecutionTOML string
	ContextTOML   string
	Status        string
}

// PhaseRow is the input to NebulaStore.InsertPhase. Body is the full Markdown
// body; it is written to the blobstore and a preview is denormalized onto the
// row. Seq orders phases within a nebula.
type PhaseRow struct {
	ID              string
	Seq             int
	Title           string
	Body            string
	FrontmatterTOML string
}

// PhaseResult records a phase's completion outcome. Diff, when non-empty, is
// written to the blobstore and its hash stored on the row.
type PhaseResult struct {
	Status     string
	ResultTOML string
	Diff       []byte
}

// MasterReviewRow carries the outcome of a master-review cycle persisted onto
// the nebula row.
type MasterReviewRow struct {
	ReviewTOML string
	Status     string // optional new nebula status; ignored when empty
}

// ListFilter narrows NebulaStore.List. Empty fields are not constrained.
type ListFilter struct {
	RepoPath string
	Status   string
}

// NebulaSummary is a list-view projection: no phase bodies, preview only.
type NebulaSummary struct {
	ID          string
	RepoPath    string
	Name        string
	Description string
	Status      string
	SourceName  string
	SourceID    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Nebula is a fully populated nebula including its phases (bodies loaded from
// the blobstore).
type Nebula struct {
	NebulaSummary
	SourceURL     string
	DefaultsTOML  string
	ExecutionTOML string
	ContextTOML   string
	Phases        []Phase
}

// Phase is a fully populated phase, body loaded from the blobstore.
type Phase struct {
	ID              string
	Seq             int
	Title           string
	Body            string
	FrontmatterTOML string
	Status          string
}
