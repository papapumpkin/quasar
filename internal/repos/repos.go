// Package repos manages the set of git repositories a long-running Quasar
// process is willing to operate on. A SQLite-backed Registry tracks registered
// repos; a Resolver layers per-repo configuration (constellations, stars,
// skills, sensors) over the embedded built-in defaults.
//
// This package is the multi-repo foundation: it ships the registry, the
// resolver, and the CLI surface. The supervisor that boots a scheduler per
// active repo lands in a later phase.
package repos

import (
	"errors"
	"time"
)

// Repo status values. A repo is active when its sensors poll and new nebulas
// may be created; paused when sensors are quiet but in-flight work continues;
// removed when soft-deleted and awaiting GC.
const (
	StatusActive  = "active"
	StatusPaused  = "paused"
	StatusRemoved = "removed"
)

// Registry/resolver error sentinels. Callers match these with errors.Is to map
// failures to exit codes and user-facing messages.
var (
	// ErrRepoNotRegistered is returned when a lookup targets a path that is not
	// in the registry.
	ErrRepoNotRegistered = errors.New("repo not registered")

	// ErrRepoAlreadyRegistered is returned when registering a path that already
	// exists in the registry.
	ErrRepoAlreadyRegistered = errors.New("repo already registered")

	// ErrRepoActiveNebulas is returned by Unregister (without force) when the
	// repo still has nebulas in a non-terminal status.
	ErrRepoActiveNebulas = errors.New("repo has active nebulas")

	// ErrRepoPathInvalid is returned when a path does not exist, is not a
	// directory, lacks a .git subdirectory, or is unreadable.
	ErrRepoPathInvalid = errors.New("repo path invalid")

	// ErrSensorNotConfigured is returned by Resolver.SensorPath when no per-repo
	// sensor file exists. Sensors have no embedded default.
	ErrSensorNotConfigured = errors.New("sensor not configured")
)

// Repo is a registered git repository that Quasar may operate on.
type Repo struct {
	// Path is the absolute filesystem path and the registry's primary key.
	Path string `json:"path"`
	// Name is the display name; defaults to filepath.Base(Path).
	Name string `json:"name"`
	// Status is one of StatusActive, StatusPaused, or StatusRemoved.
	Status string `json:"status"`
	// AddedAt is when the repo was first registered.
	AddedAt time.Time `json:"added_at"`
	// UpdatedAt is when the repo row last changed (status flips, renames).
	UpdatedAt time.Time `json:"updated_at"`
	// LastSeenAt is updated on every supervisor startup that touched the repo.
	LastSeenAt time.Time `json:"last_seen_at"`
}
