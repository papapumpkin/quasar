// Package fabric provides a shared state store for entanglement-based phase execution.
//
// Completed phases publish interface entanglements (exported types, function signatures,
// etc.) to the fabric. Dependent phases query the fabric before starting, enabling
// coordination without file-level locks. The store uses SQLite in WAL mode for
// sub-millisecond reads with concurrent reader/writer support.
package fabric

import (
	"context"
	"time"
)

// Phase states for the fabric's task lifecycle.
const (
	StateQueued        = "queued"
	StateScanning      = "scanning"
	StateRunning       = "running"
	StateBlocked       = "blocked"
	StateDone          = "done"
	StateFailed        = "failed"
	StateHumanDecision = "human_decision"
)

// Entanglement kinds describing what a phase produced.
const (
	KindType      = "type"
	KindFunction  = "function"
	KindInterface = "interface"
	KindMethod    = "method"
	KindPackage   = "package"
	KindFile      = "file"
)

// Entanglement statuses.
const (
	StatusFulfilled = "fulfilled"
	StatusDisputed  = "disputed"
	StatusPending   = "pending"
)

// Discovery kinds surfaced by agents during execution.
const (
	DiscoveryEntanglementDispute   = "entanglement_dispute"
	DiscoveryMissingDependency     = "missing_dependency"
	DiscoveryFileConflict          = "file_conflict"
	DiscoveryRequirementsAmbiguity = "requirements_ambiguity"
	DiscoveryBudgetAlert           = "budget_alert"
)

// Bead kinds for agent working memory entries.
const (
	BeadNote             = "note"
	BeadDecision         = "decision"
	BeadFailure          = "failure"
	BeadReviewerFeedback = "reviewer_feedback"
)

// Entanglement represents an interface entanglement published by a completed phase.
type Entanglement struct {
	ID        int64     `json:"id"`
	Producer  string    `json:"producer"`
	Consumer  string    `json:"consumer,omitempty"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Signature string    `json:"signature"`
	Package   string    `json:"package"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// Discovery represents an agent-surfaced issue that may require human or
// automated attention. Discoveries are posted by agents during execution
// and can target a specific task or broadcast to all consumers.
type Discovery struct {
	ID         int64     `json:"id"`
	SourceTask string    `json:"source_task"`
	Kind       string    `json:"kind"`
	Detail     string    `json:"detail"`
	Affects    string    `json:"affects,omitempty"`
	Resolved   bool      `json:"resolved"`
	CreatedAt  time.Time `json:"created_at"`
}

// Bead represents agent working memory — a timestamped note, decision, failure
// record, or reviewer feedback associated with a specific task.
type Bead struct {
	ID        int64     `json:"id"`
	TaskID    string    `json:"task_id"`
	Content   string    `json:"content"`
	Kind      string    `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
}

// Claim represents a file ownership claim by a phase.
type Claim struct {
	Filepath  string    `json:"filepath"`
	OwnerTask string    `json:"owner_task"`
	ClaimedAt time.Time `json:"claimed_at"`
}

// Fabric is the shared state store for entanglement-based execution.
// It provides phase state tracking, entanglement publishing, and file claim
// management backed by SQLite in WAL mode.
type Fabric interface {
	// SetPhaseState updates the state and worker for a phase, inserting if absent.
	SetPhaseState(ctx context.Context, phaseID, state string) error

	// GetPhaseState returns the current state for a phase. Returns empty string
	// and no error if the phase has no fabric entry.
	GetPhaseState(ctx context.Context, phaseID string) (string, error)

	// AllPhaseStates returns a map of phase ID to current state for all phases.
	AllPhaseStates(ctx context.Context) (map[string]string, error)

	// PublishEntanglement inserts or updates a single entanglement (upsert on producer+kind+name).
	PublishEntanglement(ctx context.Context, e Entanglement) error

	// PublishEntanglements inserts or updates multiple entanglements in a single transaction.
	PublishEntanglements(ctx context.Context, entanglements []Entanglement) error

	// EntanglementsFor returns all entanglements published by the given phase.
	EntanglementsFor(ctx context.Context, phaseID string) ([]Entanglement, error)

	// AllEntanglements returns every entanglement in the fabric.
	AllEntanglements(ctx context.Context) ([]Entanglement, error)

	// ClaimFile registers file ownership for a phase. Returns an error if the
	// file is already claimed by a different phase.
	ClaimFile(ctx context.Context, filepath, ownerPhaseID string) error

	// ReleaseClaims removes all file claims held by the given phase.
	ReleaseClaims(ctx context.Context, ownerPhaseID string) error

	// ReleaseFileClaim removes a specific file claim if it is owned by the given phase.
	ReleaseFileClaim(ctx context.Context, filepath, ownerPhaseID string) error

	// FileOwner returns the phase ID that owns the given file path, or empty
	// string if unclaimed.
	FileOwner(ctx context.Context, filepath string) (string, error)

	// ClaimsFor returns all file paths claimed by the given phase.
	ClaimsFor(ctx context.Context, phaseID string) ([]string, error)

	// AllClaims returns all file claims in the fabric.
	AllClaims(ctx context.Context) ([]Claim, error)

	// PostDiscovery inserts a new discovery record and returns its ID.
	PostDiscovery(ctx context.Context, d Discovery) (int64, error)

	// Discoveries returns all discoveries posted by the given task.
	Discoveries(ctx context.Context, taskID string) ([]Discovery, error)

	// AllDiscoveries returns every discovery in the fabric.
	AllDiscoveries(ctx context.Context) ([]Discovery, error)

	// ResolveDiscovery marks a discovery as resolved.
	ResolveDiscovery(ctx context.Context, id int64) error

	// UnresolvedDiscoveries returns all discoveries that have not been resolved.
	UnresolvedDiscoveries(ctx context.Context) ([]Discovery, error)

	// AddBead inserts a new bead (working memory entry) for a task.
	AddBead(ctx context.Context, b Bead) error

	// BeadsFor returns all beads associated with the given task.
	BeadsFor(ctx context.Context, taskID string) ([]Bead, error)

	// Close releases database resources.
	Close() error
}
