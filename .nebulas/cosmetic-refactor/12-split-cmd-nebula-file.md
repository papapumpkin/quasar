+++
id = "split-cmd-nebula-file"
title = "Split the 790-line cmd/nebula.go into per-subcommand files"
type = "task"
priority = 2
depends_on = ["unify-review-report"]
scope = ["cmd/nebula.go", "cmd/nebula_validate.go", "cmd/nebula_plan.go", "cmd/nebula_apply.go", "cmd/nebula_show.go", "cmd/nebula_status.go"]
+++

## Problem

`cmd/nebula.go` is ~790 lines containing:

- 5 Cobra subcommands (`validate`, `plan`, `apply`, `show`, `status`)
- Adapter types (`loopAdapter`, `tuiLoopAdapter`) that bridge `loop.Loop` to `nebula.PhaseRunner`
- JSON output types (`statusJSON`, `phaseJSON`, `dependencyJSON`)
- Signal handling (`handleSignals`)
- The `toPhaseRunnerResult` adapter function
- The root `nebulaCmd` setup

This is the largest file in `cmd/` and violates the project convention that "each file = one command."

## Solution

Split into focused files:

1. **`cmd/nebula.go`** — keep only `nebulaCmd` (the parent command) and `init()`.
2. **`cmd/nebula_validate.go`** — `nebulaValidateCmd` and its run function.
3. **`cmd/nebula_plan.go`** — `nebulaPlanCmd` and its run function.
4. **`cmd/nebula_apply.go`** — `nebulaApplyCmd`, `handleSignals`, and its run function.
5. **`cmd/nebula_show.go`** — `nebulaShowCmd` and its run function.
6. **`cmd/nebula_status.go`** — `nebulaStatusCmd`, JSON types (`statusJSON`, `phaseJSON`, `dependencyJSON`), and its run function.

All files remain in package `cmd`. Shared adapter types can stay in the parent `nebula.go` or go into a `cmd/nebula_adapters.go` (see next phase).

## Files

- `cmd/nebula.go` — strip down to parent command only
- `cmd/nebula_validate.go` (new)
- `cmd/nebula_plan.go` (new)
- `cmd/nebula_apply.go` (new)
- `cmd/nebula_show.go` (new)
- `cmd/nebula_status.go` (new)

## Acceptance Criteria

- [ ] `cmd/nebula.go` is under 50 lines (parent command + init only)
- [ ] Each subcommand lives in its own file
- [ ] `go build -o quasar .` succeeds
- [ ] `go test ./...` passes
- [ ] All nebula subcommands still work (manual smoke test: `./quasar nebula --help`)
