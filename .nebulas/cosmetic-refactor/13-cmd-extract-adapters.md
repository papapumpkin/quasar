+++
id = "cmd-extract-adapters"
title = "Extract nebula adapter types into cmd/nebula_adapters.go"
type = "task"
priority = 3
depends_on = ["split-cmd-nebula-file"]
scope = ["cmd/nebula.go", "cmd/nebula_adapters.go"]
+++

## Problem

After splitting `cmd/nebula.go` into per-subcommand files (phase `split-cmd-nebula-file`), shared adapter types will need a home. These types bridge between the `loop` and `nebula` packages:

- `loopAdapter` — wraps `loop.Loop` to satisfy `nebula.PhaseRunner`
- `tuiLoopAdapter` — wraps `loopAdapter` with TUI integration
- `toPhaseRunnerResult` — converts `loop.TaskResult` to `nebula.PhaseRunnerResult`

These are used by `nebula_apply.go` (and potentially `nebula_plan.go`). Leaving them in the stripped-down `nebula.go` works but is unclear. A dedicated file makes their purpose obvious.

## Solution

Create `cmd/nebula_adapters.go`:

1. Move `loopAdapter`, `tuiLoopAdapter`, and `toPhaseRunnerResult` into it.
2. Keep in package `cmd`.
3. Add a file-level doc comment explaining these adapt `loop` types for `nebula` consumption.

## Files

- `cmd/nebula.go` (or whichever file they end up in after phase 12) — remove adapter types
- `cmd/nebula_adapters.go` (new) — adapter types and conversion functions

## Acceptance Criteria

- [ ] Adapter types live in a dedicated, well-documented file
- [ ] `go build -o quasar .` succeeds
- [ ] `go test ./...` passes
