+++
id = "scope-fields"
title = "Add Scope and AllowScopeOverlap fields to PhaseSpec"
type = "feature"
priority = 1
depends_on = []
scope = ["internal/nebula/types.go", "internal/nebula/errors.go"]
+++

## Problem

Nebula phases have no way to declare which files or directories they intend to edit. Without ownership boundaries, parallel phases can silently collide on the same files.

## Solution

Add two new fields to `PhaseSpec` in `internal/nebula/types.go`:

- `Scope []string` — glob patterns (relative to working dir) declaring owned files/dirs
- `AllowScopeOverlap bool` — per-phase override to permit overlap with other phases

Add a new sentinel error `ErrScopeOverlap` in `internal/nebula/errors.go`.

### PhaseSpec changes

Add after the `Gate` field:

```go
Scope             []string `toml:"scope"`               // Glob patterns for owned files/dirs
AllowScopeOverlap bool     `toml:"allow_scope_overlap"` // Override: permit overlap
```

### Error sentinel

```go
var ErrScopeOverlap = errors.New("scope overlap")
```

No changes needed to `parse.go` — TOML unmarshalling picks up new fields automatically.

## Files to Modify

- `internal/nebula/types.go` — Add fields to PhaseSpec
- `internal/nebula/errors.go` — Add ErrScopeOverlap

## Acceptance Criteria

- [ ] PhaseSpec has Scope and AllowScopeOverlap fields with correct TOML tags
- [ ] ErrScopeOverlap sentinel exists in errors.go
- [ ] `go build ./...` compiles
- [ ] Existing tests pass: `go test ./internal/nebula/...`
