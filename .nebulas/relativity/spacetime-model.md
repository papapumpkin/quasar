+++
id = "spacetime-model"
title = "Define the spacetime.toml schema and Go data model"
type = "feature"
priority = 1
depends_on = []
+++

## Problem

We need a well-defined schema for `.relativity/spacetime.toml` that captures everything worth knowing about a nebula's journey through the repo: when it was created, what it changed, how it relates to other nebulas, and what category of work it represents. We also need Go types to work with this data in memory.

## Solution

### Schema: `.relativity/spacetime.toml`

```toml
# Auto-generated + manually enrichable catalog of all nebulas
# Manual annotations override auto-derived values

[relativity]
version = 1
last_scan = "2026-02-18T12:00:00Z"
repo = "github.com/papapumpkin/quasar"

# Ordered timeline of nebulas (sequence matters)
[[nebula]]
name = "tui-landing-page"
sequence = 1                          # order in the repo's history
status = "completed"                  # planned | in_progress | completed | abandoned
category = "feature"                  # feature | bugfix | refactor | enhancement | infra
created = "2026-01-15T00:00:00Z"     # from git: first commit on nebula branch
completed = "2026-02-10T00:00:00Z"   # from git: merge commit to main
branch = "nebula/tui-landing-page"   # git branch

# What areas of the codebase this nebula touches
areas = ["internal/tui", "cmd"]
packages_added = ["internal/tui"]
packages_modified = ["cmd"]

# Phase summary
total_phases = 5
completed_phases = 5

# Relationships to other nebulas
enables = ["dag-engine"]              # this nebula unlocked work on these
builds_on = []                        # this nebula depended on these

# Human-written context (manual enrichment)
summary = "Built the TUI home screen with bubbletea, showing project health and nebula status."
lessons = ["bubbletea model/update/view pattern works well for our use case"]

[[nebula]]
name = "dag-engine"
sequence = 2
status = "planned"
category = "feature"
# ... etc
```

### Go Data Model

```go
// internal/relativity/model.go

// Spacetime is the root catalog of all nebulas in the repo's history.
type Spacetime struct {
    Version  int       `toml:"version"`
    LastScan time.Time `toml:"last_scan"`
    Repo     string    `toml:"repo"`
    Nebulas  []Entry   `toml:"nebula"`
}

// Entry is a single nebula's metadata in the catalog.
type Entry struct {
    Name      string    `toml:"name"`
    Sequence  int       `toml:"sequence"`
    Status    string    `toml:"status"`
    Category  string    `toml:"category"`
    Created   time.Time `toml:"created"`
    Completed time.Time `toml:"completed,omitempty"`
    Branch    string    `toml:"branch"`

    // Codebase impact
    Areas            []string `toml:"areas"`
    PackagesAdded    []string `toml:"packages_added"`
    PackagesModified []string `toml:"packages_modified"`

    // Phase tracking
    TotalPhases     int `toml:"total_phases"`
    CompletedPhases int `toml:"completed_phases"`

    // Relationships
    Enables  []string `toml:"enables"`
    BuildsOn []string `toml:"builds_on"`

    // Manual enrichment
    Summary string   `toml:"summary,omitempty"`
    Lessons []string `toml:"lessons,omitempty"`
}
```

### TOML Read/Write

- Load existing `spacetime.toml` if it exists
- Merge auto-derived data with manual annotations (manual wins on conflict)
- Write back preserving manual fields that weren't overwritten

## Files

- `internal/relativity/model.go` — Spacetime and Entry types
- `internal/relativity/toml.go` — load/save/merge logic for spacetime.toml
- `internal/relativity/model_test.go` — round-trip serialization tests

## Acceptance Criteria

- [ ] Spacetime struct fully represents the schema above
- [ ] Load/save round-trips without data loss
- [ ] Manual annotations survive re-scanning (merge logic preserves them)
- [ ] `go test ./internal/relativity/...` passes