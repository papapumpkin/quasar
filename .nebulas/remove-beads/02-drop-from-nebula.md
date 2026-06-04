+++
id = "drop-from-nebula"
title = "Remove beads from the nebula orchestration layer"
type = "task"
priority = 1
depends_on = ["drop-from-loop"]
scope = [
    "internal/nebula/apply.go",
    "internal/nebula/worker.go",
    "internal/nebula/worker_exec.go",
    "internal/nebula/worker_options.go",
    "internal/nebula/worker_changes_test.go",
    "internal/nebula/worker_resume_test.go",
    "internal/nebula/worker_healing.go",
    "internal/nebula/plan.go",
    "internal/nebula/engine.go",
    "internal/nebula/engine_types.go",
    "internal/nebula/engine_test.go",
    "internal/nebula/hotreload.go",
    "internal/nebula/nebula_test.go",
    "internal/nebula/types.go",
    "internal/nebula/dashboard_test.go",
    "internal/nebula/git.go",
    "internal/nebula/validate.go",
]
+++

## Problem

`internal/nebula/` is the largest concentration of beads references after the loop layer: 63 in `nebula_test.go`, 22 in `apply.go`, 21 in `dashboard_test.go`, 15 in `worker_exec.go`, 14 in `plan.go`, and trails across most other files. The orchestrator constructs `Loop` instances, dispatches phase workers, and manages phase lifecycle — every spot where the old code created a bead, updated bead status, or read bead state has to be unwound. Phase 1 already removed `Beads` from the `Loop` constructor, so this phase is now blocked-then-unblocked by the compile errors that will surface here.

The nebula manifest schema also still has `requires_beads` in `[dependencies]` — that field is removed here so the schema reflects the new reality (existing manifests with `requires_beads = []` keep parsing because absent fields default to empty in TOML; explicit `requires_beads = []` will start producing a "unknown field" error unless the parser is told to ignore unknowns, which is the project's existing behavior; if it's not, this phase must add tolerant parsing for that one legacy key for one release).

## Solution

Strip every beads import, type reference, and call site from the `internal/nebula/` package. Update the manifest schema definition in `types.go` to remove the `requires_beads` field. Adjust tests that exercised beads behavior. Fix the `Loop` construction calls in this package to match the new constructor signature from phase 1.

### Concrete changes

**`internal/nebula/types.go`:**
- Remove `RequiresBeads []string` (or equivalent field name) from the manifest dependencies struct
- Remove any types like `BeadDependency`, `BeadReference`, etc.
- If the manifest parser uses strict-unknown-field decoding, switch to lenient for the `[dependencies]` section so old manifests with `requires_beads = []` continue to parse (warn-and-ignore is acceptable). If parsing is already lenient via Viper/TOML defaults, no change is needed here.

**`internal/nebula/apply.go`:**
- Remove the beads import
- Stop instantiating a `beads.Client` and stop passing it to `loop.NewLoop` (signature changed in phase 1)
- Remove `applyBeads`, `seedBeads`, `closeBeads`, or any similarly-named functions that wrote bead state
- Remove any beads-based blocking checks (e.g., "wait for bead X to close before starting phase Y")

**`internal/nebula/worker.go`, `worker_exec.go`, `worker_options.go`, `worker_healing.go`:**
- Remove beads imports
- Remove bead-write side effects in phase lifecycle (start/complete/fail/skip)
- `WorkerOptions` (if it carries a beads client) loses that field
- Worker exec stops setting bead status on phase transitions

**`internal/nebula/plan.go`:**
- Remove beads from plan-phase resolution
- If `Plan` had `BeadID` fields per phase, drop them (phase status lives in nebula.state.toml and SQLite)

**`internal/nebula/engine.go`, `engine_types.go`, `hotreload.go`:**
- Remove beads imports and the engine's beads field/initialization
- Drop bead-driven hot-reload signals if any

**`internal/nebula/git.go`, `validate.go`:**
- Remove beads imports if present
- `validate.go`: remove validation rules that touched `requires_beads` (e.g., "all bead IDs must be valid")

**`internal/nebula/nebula_test.go`, `engine_test.go`, `dashboard_test.go`, `worker_changes_test.go`, `worker_resume_test.go`:**
- Remove beads mock implementations and beads-arg passing
- Drop test cases that were purely about beads behavior
- Keep all non-beads test cases functional

### Migration notes

- After this phase, manifests in `.nebulas/*/nebula.toml` that contain `requires_beads = []` (which is most of them) continue to load. The field is ignored. A future cleanup nebula could rewrite all manifests to drop the field; that is **not** scope here.
- Nebula state files (`nebula.state.toml`) that include bead references in legacy state can keep those fields; they will be ignored by the new schema. A field like `bead_id` becomes dead data.
- The existing `[dependencies]` manifest block keeps `requires_nebulae` — that field is unrelated and stays.

### Verification within this phase

```bash
go build ./internal/nebula/...
go vet ./internal/nebula/...
go test ./internal/nebula/...
grep -rn "beads\|bead_\|Beads" internal/nebula/ 2>/dev/null
```
Build, vet, and tests must exit 0. Grep must return no matches except possibly `internal/nebula/types.go` if it carries a backward-compat comment about the `requires_beads` legacy field — that comment is OK if present.

Note: `go build ./...` at the repo root still fails because cmd/ references beads. Fixed in phase 3.

## Files

- `internal/nebula/apply.go` — strip beads import, beads.Client instantiation, applyBeads/seedBeads/closeBeads helpers; update loop.NewLoop call to phase-1 signature
- `internal/nebula/worker.go` — strip beads import and bead-write side effects
- `internal/nebula/worker_exec.go` — strip beads import and lifecycle bead writes
- `internal/nebula/worker_options.go` — drop beads field from WorkerOptions struct
- `internal/nebula/worker_healing.go` — strip beads imports if present
- `internal/nebula/plan.go` — strip beads import and BeadID per-phase fields
- `internal/nebula/engine.go` — strip beads import and engine.beads field
- `internal/nebula/engine_types.go` — drop beads-typed fields from engine state types
- `internal/nebula/hotreload.go` — strip beads import
- `internal/nebula/types.go` — remove RequiresBeads from manifest schema; ensure parser tolerates legacy field
- `internal/nebula/git.go` — strip beads import if present
- `internal/nebula/validate.go` — strip beads-related validation rules
- `internal/nebula/nebula_test.go` — drop beads mocks and beads-only test cases
- `internal/nebula/engine_test.go` — drop beads-only test cases, update mocks
- `internal/nebula/dashboard_test.go` — drop beads-only test cases
- `internal/nebula/worker_changes_test.go` — drop beads-only test cases
- `internal/nebula/worker_resume_test.go` — drop beads-only test cases

## Acceptance Criteria

- [ ] `grep -rn "beads\|bead_" internal/nebula/ 2>/dev/null` returns no matches (or only a single comment-line about the legacy `requires_beads` field for backward-compat)
- [ ] `grep -rn "Beads " internal/nebula/ 2>/dev/null` returns no matches
- [ ] No imports of `github.com/aaronsalm/quasar/internal/beads` remain in `internal/nebula/`
- [ ] `go build ./internal/nebula/...` exits 0
- [ ] `go vet ./internal/nebula/...` exits 0
- [ ] `go test ./internal/nebula/...` exits 0
- [ ] `internal/nebula/types.go` no longer declares a `RequiresBeads` field on the manifest dependencies struct
- [ ] Existing manifests in `.nebulas/*/nebula.toml` that contain `requires_beads = []` continue to parse (no regression on existing nebulas)
- [ ] `loop.NewLoop` (or equivalent constructor) is called everywhere with the phase-1 signature (no `beads.Client` argument)
- [ ] No code path attempts to read or write bead state
