+++
id = "manifest-types"
title = "Define constellation manifest types and TOML parsing"
type = "feature"
priority = 1
depends_on = []
scope = ["internal/constellation/types.go", "internal/constellation/parse.go", "internal/constellation/parse_test.go"]
+++

## Problem

Quasar currently orchestrates work at the single-nebula level via `internal/nebula/`. There is no way to define a higher-level DAG of nebulas — specifying which nebulas depend on which, sharing context across them, or setting coordination rules like failure strategies and budget allocation. Without a manifest format for multi-nebula coordination, users must manually sequence nebula runs and track cross-nebula dependencies by hand.

The existing `nebula.Manifest` type (`internal/nebula/types.go`) defines per-nebula configuration with fields like `Info`, `Defaults`, `Execution`, `Context`, and `Dependencies`. The constellation layer needs analogous types that operate at the nebula-of-nebulas level, with each entry in the constellation referencing a nebula directory on disk.

## Solution

Create `internal/constellation/` with two files: `types.go` for the data model and `parse.go` for TOML deserialization. The constellation manifest (`constellation.toml`) lives in a directory alongside the nebula directories it references.

### Manifest format (`constellation.toml`)

```toml
[constellation]
name = "my-release"
description = "Full release pipeline: infra, features, tests, deploy"

[shared_context]
goals = ["Ship v2.0 with zero regressions"]
constraints = ["Total budget cannot exceed $200"]
entanglements = ["api-contract-v2", "database-schema"]

[coordination]
failure_strategy = "halt"       # halt | skip | retry
retry_max = 3
retry_backoff_seconds = 30
budget_total_usd = 200.0
budget_per_nebula_usd = 0.0    # 0 = no per-nebula cap (use total only)
oracle_enabled = true

[[nebula]]
name = "infra-setup"
path = ".nebulas/infra-setup"
depends_on = []

[[nebula]]
name = "feature-alpha"
path = ".nebulas/feature-alpha"
depends_on = ["infra-setup"]

[[nebula]]
name = "feature-beta"
path = ".nebulas/feature-beta"
depends_on = ["infra-setup"]

[[nebula]]
name = "integration-tests"
path = ".nebulas/integration-tests"
depends_on = ["feature-alpha", "feature-beta"]
```

### Type definitions

```go
// internal/constellation/types.go

package constellation

import "time"

// FailureStrategy determines how the constellation handles a nebula failure.
type FailureStrategy string

const (
    // FailureHalt stops the entire constellation when any nebula fails.
    FailureHalt FailureStrategy = "halt"
    // FailureSkip marks the failed nebula as skipped and continues with
    // nebulas that do not depend on it.
    FailureSkip FailureStrategy = "skip"
    // FailureRetry retries the failed nebula up to CoordinationRules.RetryMax
    // times before falling back to the failure strategy.
    FailureRetry FailureStrategy = "retry"
)

// ValidFailureStrategies lists all recognized failure strategy values.
var ValidFailureStrategies = []FailureStrategy{FailureHalt, FailureSkip, FailureRetry}

// NebulaStatus tracks the execution state of a single nebula within the constellation.
type NebulaStatus string

const (
    NebulaStatusPending   NebulaStatus = "pending"
    NebulaStatusRunning   NebulaStatus = "running"
    NebulaStatusDone      NebulaStatus = "done"
    NebulaStatusFailed    NebulaStatus = "failed"
    NebulaStatusSkipped   NebulaStatus = "skipped"
    NebulaStatusRetrying  NebulaStatus = "retrying"
    NebulaStatusBlocked   NebulaStatus = "blocked"
)

// Constellation is the top-level type representing a multi-nebula coordination
// manifest parsed from constellation.toml.
type Constellation struct {
    Dir           string
    Info          ConstellationInfo
    SharedContext SharedContext
    Coordination  CoordinationRules
    Nebulas       []NebulaRef
}

// ConstellationInfo holds the name and description from [constellation].
type ConstellationInfo struct {
    Name        string `toml:"name"`
    Description string `toml:"description"`
}

// SharedContext defines goals, constraints, and entanglements shared across
// all nebulas in the constellation. These are injected into each nebula's
// context at execution time.
type SharedContext struct {
    Goals         []string `toml:"goals"`
    Constraints   []string `toml:"constraints"`
    Entanglements []string `toml:"entanglements"`
}

// CoordinationRules govern how the constellation scheduler handles failures,
// retries, and budget allocation.
type CoordinationRules struct {
    FailureStrategy     FailureStrategy `toml:"failure_strategy"`
    RetryMax            int             `toml:"retry_max"`
    RetryBackoffSeconds int             `toml:"retry_backoff_seconds"`
    BudgetTotalUSD      float64         `toml:"budget_total_usd"`
    BudgetPerNebulaUSD  float64         `toml:"budget_per_nebula_usd"`
    OracleEnabled       bool            `toml:"oracle_enabled"`
}

// NebulaRef is a reference to a nebula directory within the constellation DAG.
// Each ref specifies the nebula's name, its path on disk relative to the
// constellation directory, and which other nebulas it depends on.
type NebulaRef struct {
    Name      string   `toml:"name"`
    Path      string   `toml:"path"`
    DependsOn []string `toml:"depends_on"`
}

// NebulaState tracks runtime state for a single nebula within the constellation.
type NebulaState struct {
    Name       string       `json:"name"`
    Status     NebulaStatus `json:"status"`
    CostUSD    float64      `json:"cost_usd"`
    Retries    int          `json:"retries"`
    Error      string       `json:"error,omitempty"`
    StartedAt  *time.Time   `json:"started_at,omitempty"`
    FinishedAt *time.Time   `json:"finished_at,omitempty"`
}
```

### TOML parsing

```go
// internal/constellation/parse.go

package constellation

import (
    "fmt"
    "os"
    "path/filepath"

    "github.com/pelletier/go-toml/v2"
)

const manifestFile = "constellation.toml"

// rawManifest mirrors the TOML structure for deserialization.
type rawManifest struct {
    Constellation ConstellationInfo `toml:"constellation"`
    SharedContext SharedContext     `toml:"shared_context"`
    Coordination  CoordinationRules `toml:"coordination"`
    Nebulas       []NebulaRef       `toml:"nebula"`
}

// Load reads a constellation directory, parses constellation.toml, and
// returns a fully populated Constellation. It does not validate the DAG
// or check that referenced nebula directories exist — use Validate for that.
func Load(dir string) (*Constellation, error) {
    path := filepath.Join(dir, manifestFile)
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("read constellation manifest: %w", err)
    }

    var raw rawManifest
    if err := toml.Unmarshal(data, &raw); err != nil {
        return nil, fmt.Errorf("parse constellation manifest: %w", err)
    }

    c := &Constellation{
        Dir:           dir,
        Info:          raw.Constellation,
        SharedContext: raw.SharedContext,
        Coordination:  raw.Coordination,
        Nebulas:       raw.Nebulas,
    }

    // Apply defaults for unset coordination fields.
    if c.Coordination.FailureStrategy == "" {
        c.Coordination.FailureStrategy = FailureHalt
    }

    return c, nil
}
```

## Files

- `internal/constellation/types.go` — `Constellation`, `ConstellationInfo`, `SharedContext`, `CoordinationRules`, `NebulaRef`, `NebulaState`, status constants, failure strategy constants
- `internal/constellation/parse.go` — `Load()` function, `rawManifest` deserialization struct, default application
- `internal/constellation/parse_test.go` — table-driven tests for `Load()`: valid manifest, missing file, malformed TOML, missing name, default failure strategy

## Acceptance Criteria

- [ ] `internal/constellation/types.go` compiles and defines all exported types with GoDoc comments
- [ ] `Load()` correctly parses a valid `constellation.toml` into a `*Constellation`
- [ ] `Load()` returns a wrapped error when the file is missing or TOML is malformed
- [ ] Default failure strategy is `FailureHalt` when omitted from the manifest
- [ ] `NebulaRef.DependsOn` correctly deserializes as a string slice from `[[nebula]]` entries
- [ ] `go build ./internal/constellation/...` passes
- [ ] `go vet ./internal/constellation/...` passes
- [ ] `go test ./internal/constellation/...` passes with table-driven tests
