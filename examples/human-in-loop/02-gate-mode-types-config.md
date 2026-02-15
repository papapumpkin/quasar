+++
id = "gate-mode-types-config"
title = "Define gate mode types and configuration"
type = "feature"
priority = 1
depends_on = ["rename-task-to-phase"]
+++

## Problem

Quasar nebulae run fully autonomously with no mechanism for human involvement during execution. There is no way to configure whether or how the human should be consulted between phases.

## Solution

Introduce a `GateMode` enum and wire it into the manifest config (`nebula.toml`) and phase frontmatter so that gate behavior can be set globally and overridden per-phase.

### Gate Modes

```go
type GateMode string

const (
    GateModeTrust   GateMode = "trust"   // Fully autonomous, no pauses
    GateModeReview  GateMode = "review"  // Pause after each phase, show diff, await approval
    GateModeApprove GateMode = "approve" // Gate the plan AND each phase
    GateModeWatch   GateMode = "watch"   // Stream diffs in real time, no blocking
)
```

### Config Integration

**nebula.toml `[execution]` section:**
```toml
[execution]
gate = "review"  # Default gate mode for all phases
```

Add `Gate GateMode` field to the `Execution` struct in `types.go` with TOML tag `gate`. Default to `"trust"` for backward compatibility.

**Phase frontmatter override:**
```
+++
id = "deploy-to-prod"
gate = "approve"
+++
```

Add `Gate GateMode` field to `PhaseSpec` (after the rename) with TOML tag `gate`. Empty string means "inherit from manifest."

### Resolution Logic

Create a `ResolveGate(manifest Execution, phase PhaseSpec) GateMode` function:
1. If `phase.Gate` is non-empty, use it
2. Otherwise use `manifest.Gate`
3. If both empty, default to `GateModeTrust`

### Validation

- `validate.go` should reject unknown gate mode strings
- Valid values: `trust`, `review`, `approve`, `watch`, or empty (inherit)

## Files to Modify

- `internal/nebula/types.go` — Add `GateMode` type, constants, fields on `Execution` and `PhaseSpec`
- `internal/nebula/config.go` — Parse gate from manifest
- `internal/nebula/validate.go` — Validate gate mode values
- `internal/nebula/nebula_test.go` — Test gate resolution and validation

## Files to Create

- None — all changes are additions to existing files

## Acceptance Criteria

- [ ] `GateMode` type with four constants defined in `types.go`
- [ ] `Execution.Gate` field parsed from `[execution]` TOML
- [ ] `PhaseSpec.Gate` field parsed from phase frontmatter
- [ ] `ResolveGate` function returns correct mode with inheritance logic
- [ ] Unknown gate modes rejected during validation
- [ ] Empty gate defaults to `trust` (backward compatible)
- [ ] Tests cover all resolution paths and invalid values
- [ ] `go test ./internal/nebula/...` passes