// Package board provides a shared state store for contract-based phase execution.
//
// Completed phases publish interface contracts (exported types, function signatures,
// etc.) to the board. Dependent phases query the board before starting, enabling
// coordination without file-level locks. The store uses SQLite in WAL mode for
// sub-millisecond reads with concurrent reader/writer support.
package board

import (
	"context"
	"time"
)

// Phase states for the board's task lifecycle.
const (
	StateQueued  = "queued"
	StatePolling = "polling"
	StateRunning = "running"
	StateBlocked = "blocked"
	StateDone    = "done"
	StateFailed  = "failed"
)

// Contract kinds describing what a phase produced.
const (
	KindType      = "type"
	KindFunction  = "function"
	KindInterface = "interface"
	KindPackage   = "package"
	KindFile      = "file"
)

// Contract statuses.
const (
	StatusFulfilled = "fulfilled"
	StatusDisputed  = "disputed"
)

// Contract represents an interface contract published by a completed phase.
type Contract struct {
	ID        int64     `json:"id"`
	Producer  string    `json:"producer"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Signature string    `json:"signature"`
	Package   string    `json:"package"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// FileClaim represents a file ownership claim by a phase.
type FileClaim struct {
	Filepath  string    `json:"filepath"`
	OwnerTask string    `json:"owner_task"`
	ClaimedAt time.Time `json:"claimed_at"`
}

// Board is the shared state store for contract-based execution.
// It provides phase state tracking, contract publishing, and file claim
// management backed by SQLite in WAL mode.
type Board interface {
	// SetPhaseState updates the state and worker for a phase, inserting if absent.
	SetPhaseState(ctx context.Context, phaseID, state string) error

	// GetPhaseState returns the current state for a phase. Returns empty string
	// and no error if the phase has no board entry.
	GetPhaseState(ctx context.Context, phaseID string) (string, error)

	// PublishContract inserts or updates a single contract (upsert on producer+kind+name).
	PublishContract(ctx context.Context, c Contract) error

	// PublishContracts inserts or updates multiple contracts in a single transaction.
	PublishContracts(ctx context.Context, contracts []Contract) error

	// ContractsFor returns all contracts published by the given phase.
	ContractsFor(ctx context.Context, phaseID string) ([]Contract, error)

	// AllContracts returns every contract in the board.
	AllContracts(ctx context.Context) ([]Contract, error)

	// ClaimFile registers file ownership for a phase. Returns an error if the
	// file is already claimed by a different phase.
	ClaimFile(ctx context.Context, filepath, ownerPhaseID string) error

	// ReleaseClaims removes all file claims held by the given phase.
	ReleaseClaims(ctx context.Context, ownerPhaseID string) error

	// FileOwner returns the phase ID that owns the given file path, or empty
	// string if unclaimed.
	FileOwner(ctx context.Context, filepath string) (string, error)

	// ClaimsFor returns all file paths claimed by the given phase.
	ClaimsFor(ctx context.Context, phaseID string) ([]string, error)

	// Close releases database resources.
	Close() error
}
