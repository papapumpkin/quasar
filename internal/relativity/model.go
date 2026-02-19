// Package relativity provides the data model and persistence layer for the
// nebula catalog, stored in .relativity/spacetime.toml. It tracks the ordered
// history of nebulas, their codebase impact, relationships, and optional
// human-written annotations.
package relativity

import "time"

// Status values for a nebula entry.
const (
	StatusPlanned    = "planned"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusAbandoned  = "abandoned"
)

// Category values for classifying a nebula's purpose.
const (
	CategoryFeature     = "feature"
	CategoryBugfix      = "bugfix"
	CategoryRefactor    = "refactor"
	CategoryEnhancement = "enhancement"
	CategoryInfra       = "infra"
)

// Spacetime is the root catalog of all nebulas in the repo's history.
type Spacetime struct {
	Relativity Header  `toml:"relativity"`
	Nebulas    []Entry `toml:"nebula"`
}

// Header contains top-level metadata about the catalog itself.
type Header struct {
	Version  int       `toml:"version"`
	LastScan time.Time `toml:"last_scan"`
	Repo     string    `toml:"repo"`
}

// Entry is a single nebula's metadata in the catalog.
type Entry struct {
	Name      string    `toml:"name"`
	Sequence  int       `toml:"sequence"`
	Status    string    `toml:"status"`
	Category  string    `toml:"category"`
	Created   time.Time `toml:"created"`
	Completed time.Time `toml:"completed,omitzero"`
	Branch    string    `toml:"branch"`

	// Codebase impact.
	Areas            []string `toml:"areas"`
	PackagesAdded    []string `toml:"packages_added"`
	PackagesModified []string `toml:"packages_modified"`

	// Phase tracking.
	TotalPhases     int `toml:"total_phases"`
	CompletedPhases int `toml:"completed_phases"`

	// Relationships to other nebulas.
	Enables  []string `toml:"enables"`
	BuildsOn []string `toml:"builds_on"`

	// Manual enrichment (preserved across re-scans).
	Summary string   `toml:"summary,omitempty"`
	Lessons []string `toml:"lessons,omitempty"`
}
