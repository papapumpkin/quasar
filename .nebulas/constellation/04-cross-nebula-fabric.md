+++
id = "cross-nebula-fabric"
title = "Cross-nebula fabric with namespace isolation and entanglement propagation"
type = "feature"
priority = 2
depends_on = ["dag-scheduler"]
scope = ["internal/constellation/fabric.go", "internal/constellation/fabric_test.go", "internal/fabric/sqlite.go"]
allow_scope_overlap = true
+++

## Problem

Each nebula currently creates its own isolated fabric instance (`internal/fabric/sqlite.go`) for tracking entanglements, claims, discoveries, and pulses between phases. In a constellation, nebulas need to coordinate across boundaries: if nebula A produces an API endpoint, nebula B (which depends on A) should be able to reference that entanglement.

Without cross-nebula fabric coordination, dependent nebulas cannot verify that upstream interfaces are fulfilled, leading to runtime integration failures that could have been caught at the fabric level.

## Solution

### Namespace isolation

Add a `nebula_id` column to the fabric's SQLite tables so that entanglements, claims, discoveries, and pulses are namespaced per-nebula by default:

```go
// In internal/fabric/sqlite.go, modify table schemas:

// entanglements table gains nebula_id column
CREATE TABLE IF NOT EXISTS entanglements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    nebula_id TEXT NOT NULL DEFAULT '',
    producer TEXT NOT NULL,
    consumer TEXT NOT NULL,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    signature TEXT,
    package TEXT,
    file TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
)

// claims table gains nebula_id column
CREATE TABLE IF NOT EXISTS claims (
    filepath TEXT NOT NULL,
    owner_task TEXT NOT NULL,
    nebula_id TEXT NOT NULL DEFAULT '',
    claimed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (filepath, nebula_id)
)
```

Existing queries are modified to filter by `nebula_id` when it is set. A zero-value `nebula_id` (empty string) preserves backward compatibility for single-nebula execution.

### NamespacedFabric

```go
// internal/constellation/fabric.go

package constellation

import "github.com/aaronsalm/quasar/internal/fabric"

// NamespacedFabric wraps a shared fabric.Fabric instance and scopes
// all operations to a specific nebula namespace. Each nebula in the
// constellation gets its own NamespacedFabric instance that shares
// the underlying SQLite database.
type NamespacedFabric struct {
    inner     fabric.Fabric
    nebulaID  string
}

// NewNamespacedFabric creates a fabric view scoped to the given nebula.
func NewNamespacedFabric(inner fabric.Fabric, nebulaID string) *NamespacedFabric {
    return &NamespacedFabric{inner: inner, nebulaID: nebulaID}
}
```

Each `fabric.Fabric` method delegates to the inner fabric with the namespace applied:

```go
func (nf *NamespacedFabric) PublishEntanglement(ctx context.Context, e fabric.Entanglement) error {
    e.NebulaID = nf.nebulaID
    return nf.inner.PublishEntanglement(ctx, e)
}

func (nf *NamespacedFabric) EntanglementsFor(ctx context.Context, phaseID string) ([]fabric.Entanglement, error) {
    return nf.inner.EntanglementsForNamespace(ctx, phaseID, nf.nebulaID)
}
```

### Entanglement propagation

When a nebula completes, its exported entanglements can be propagated to dependent nebulas. The propagation rules are defined in the constellation manifest:

```go
// PropagationRule defines which entanglements flow across nebula boundaries.
type PropagationRule struct {
    // From is the source nebula name.
    From string

    // To is the target nebula name.
    To string

    // Kinds filters which entanglement kinds to propagate (empty = all).
    Kinds []string

    // Packages filters by package name (empty = all).
    Packages []string
}

// Propagator copies fulfilled entanglements from one nebula namespace
// to another, making them visible to the downstream nebula's phases.
type Propagator struct {
    fabric fabric.Fabric
    rules  []PropagationRule
}

// NewPropagator creates a propagator with the given rules.
func NewPropagator(f fabric.Fabric, rules []PropagationRule) *Propagator {
    return &Propagator{fabric: f, rules: rules}
}

// Propagate copies eligible entanglements from the source namespace
// to target namespaces per the configured rules. Called by the
// scheduler after a nebula completes.
func (p *Propagator) Propagate(ctx context.Context, fromNebula string) error {
    for _, rule := range p.rules {
        if rule.From != fromNebula {
            continue
        }
        entanglements, err := p.fabric.EntanglementsForNamespace(ctx, "", rule.From)
        if err != nil {
            return fmt.Errorf("read entanglements from %s: %w", rule.From, err)
        }
        for _, e := range entanglements {
            if !p.matchesFilter(e, rule) {
                continue
            }
            e.NebulaID = rule.To
            if err := p.fabric.PublishEntanglement(ctx, e); err != nil {
                return fmt.Errorf("propagate entanglement to %s: %w", rule.To, err)
            }
        }
    }
    return nil
}
```

### Shared fabric instance

The constellation creates a single SQLite fabric instance shared by all nebulas, with each nebula getting a `NamespacedFabric` view:

```go
// SharedFabric creates a single fabric instance and namespace views
// for each nebula in the constellation.
func SharedFabric(ctx context.Context, c *Constellation, dbPath string) (fabric.Fabric, map[string]*NamespacedFabric, error) {
    shared, err := fabric.NewSQLite(dbPath)
    if err != nil {
        return nil, nil, err
    }

    views := make(map[string]*NamespacedFabric, len(c.Nebulas))
    for _, ref := range c.Nebulas {
        views[ref.Name] = NewNamespacedFabric(shared, ref.Name)
    }

    return shared, views, nil
}
```

### Fabric schema migration

The `internal/fabric/sqlite.go` migration adds the `nebula_id` column with a default empty string, preserving backward compatibility:

```go
// Migration: add nebula_id to existing tables.
ALTER TABLE entanglements ADD COLUMN nebula_id TEXT NOT NULL DEFAULT '';
ALTER TABLE claims ADD COLUMN nebula_id TEXT NOT NULL DEFAULT '';
ALTER TABLE discoveries ADD COLUMN nebula_id TEXT NOT NULL DEFAULT '';
ALTER TABLE pulses ADD COLUMN nebula_id TEXT NOT NULL DEFAULT '';
```

## Files

- `internal/constellation/fabric.go` — `NamespacedFabric`, `Propagator`, `PropagationRule`, `SharedFabric`
- `internal/constellation/fabric_test.go` — tests for: namespace isolation (nebula A cannot see nebula B's entanglements), propagation copies entanglements across boundaries, filter by kind/package, empty namespace preserves old behavior
- `internal/fabric/sqlite.go` — add `nebula_id` column to tables, update queries to filter by namespace, add `EntanglementsForNamespace` method
- `internal/fabric/fabric.go` — add `EntanglementsForNamespace(ctx, phaseID, nebulaID)` and `NebulaID` field to `Entanglement` struct

## Acceptance Criteria

- [ ] Each nebula in a constellation gets an isolated fabric namespace
- [ ] Entanglements published by nebula A are not visible to nebula B by default
- [ ] `Propagator.Propagate` copies eligible entanglements from source to target namespace
- [ ] Propagation rules can filter by entanglement kind and package
- [ ] Single-nebula execution (empty `nebula_id`) works identically to before
- [ ] Schema migration adds `nebula_id` column without breaking existing databases
- [ ] `SharedFabric` creates one SQLite database and per-nebula namespace views
- [ ] `go test ./internal/constellation/...` passes
- [ ] `go test ./internal/fabric/...` passes (existing tests unaffected)
- [ ] `go vet ./...` passes
