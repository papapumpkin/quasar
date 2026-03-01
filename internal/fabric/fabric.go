// Package fabric provides a shared state store for entanglement-based phase execution.
//
// Completed phases publish interface entanglements (exported types, function signatures,
// etc.) to the fabric. Dependent phases query the fabric before starting, enabling
// coordination without file-level locks. The store uses SQLite in WAL mode for
// sub-millisecond reads with concurrent reader/writer support.
package fabric

import (
	"context"
	"fmt"
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
	StateDecomposed    = "decomposed"
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

// Pulse kinds for shared execution context emissions.
const (
	PulseNote             = "note"
	PulseDecision         = "decision"
	PulseFailure          = "failure"
	PulseReviewerFeedback = "reviewer_feedback"
)

// ValidPulseKinds enumerates the allowed values for Pulse.Kind.
var ValidPulseKinds = map[string]bool{
	PulseNote:             true,
	PulseDecision:         true,
	PulseFailure:          true,
	PulseReviewerFeedback: true,
}

// ValidatePulseKind returns an error if kind is not a recognized pulse kind.
func ValidatePulseKind(kind string) error {
	if !ValidPulseKinds[kind] {
		return fmt.Errorf("invalid pulse kind %q: must be one of note, decision, failure, reviewer_feedback", kind)
	}
	return nil
}

// Entanglement represents an interface entanglement published by a completed phase.
type Entanglement struct {
	ID        int64     `json:"id"`
	Producer  string    `json:"producer"`
	Consumer  string    `json:"consumer,omitempty"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Signature string    `json:"signature"`
	Package   string    `json:"package"`
	File      string    `json:"file,omitempty"`
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

// Pulse is a structured context emission from a quasar during execution.
// Pulses propagate through the fabric so concurrent and downstream quasars
// share execution context without direct communication.
type Pulse struct {
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

	// EmitPulse inserts a new pulse (shared execution context) for a task.
	EmitPulse(ctx context.Context, p Pulse) error

	// PulsesFor returns all pulses associated with the given task.
	PulsesFor(ctx context.Context, taskID string) ([]Pulse, error)

	// AllPulses returns every pulse in the fabric.
	AllPulses(ctx context.Context) ([]Pulse, error)

	// PurgeAll removes all state from the fabric (all tables).
	PurgeAll(ctx context.Context) error

	// PurgeFulfilledEntanglements removes entanglements with status 'fulfilled'.
	// Disputed and pending entanglements are preserved for human review.
	PurgeFulfilledEntanglements(ctx context.Context) error

	// Close releases database resources.
	Close() error
}
