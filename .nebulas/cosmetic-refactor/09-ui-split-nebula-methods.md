+++
id = "ui-split-nebula-methods"
title = "Split nebula-specific UI methods into a separate file"
type = "task"
priority = 2
depends_on = ["consolidate-ansi-constants"]
scope = ["internal/ui/"]
+++

## Problem

`internal/ui/ui.go` is ~600 lines and mixes two distinct concerns:

1. **Loop UI methods**: `CycleStart`, `AgentStart`, `AgentDone`, `ReviewResult`, `TaskSuccess`, `Error`, `Warning`, `Info`, `Verbose`, `Section`, `Detail`, `Separator`, etc.
2. **Nebula UI methods**: `NebulaValidateResult`, `NebulaPlan`, `NebulaApplyDone`, `NebulaWorkerResults`, `NebulaShow`, `NebulaStatus`, `NebulaProgressBar`, `NebulaPhaseStatus`, `NebulaPhaseComplete`, etc.

The nebula methods are only used by `cmd/nebula.go` and `internal/nebula/`. Mixing them in one file makes `ui.go` hard to navigate.

This phase depends on `consolidate-ansi-constants` because that phase exports the ANSI constants, which affects the `ui` package structure.

## Solution

Split into two files:

1. Keep `internal/ui/ui.go` with the `Printer` struct, constructor, and all loop/general-purpose methods.
2. Create `internal/ui/nebula.go` with all `Nebula*` methods on `Printer`.
3. Both files stay in package `ui` — this is just a file split, no API changes.

## Files

- `internal/ui/ui.go` — remove all `Nebula*` methods
- `internal/ui/nebula.go` (new) — all `Nebula*` methods on `Printer`

## Acceptance Criteria

- [ ] `ui.go` contains only loop/general-purpose methods
- [ ] `nebula.go` contains all `Nebula*` methods
- [ ] No exported API changes
- [ ] `go test ./...` passes
