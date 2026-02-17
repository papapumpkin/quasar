+++
id = "lifecycle-state-machine"
title = "Nebula lifecycle state machine: nursery → constellation → neutron"
type = "feature"
priority = 1
depends_on = []
+++

## Problem

Nebulas currently have a flat lifecycle: you create phase files, run them, and they're "done." There's no formal concept of what happens before execution (the creative planning energy), during post-execution review (when the user shapes the results), or after completion (preserving the knowledge). The lifecycle needs three named stages that flow naturally.

## Concept

```
  ╭──────────╮       ╭───────────────╮       ╭──────────╮
  │ NURSERY  │──────▶│ CONSTELLATION │──────▶│ NEUTRON  │
  │          │       │               │       │          │
  │ potential │       │ formed work   │       │ dense    │
  │ energy    │       │ review/refine │       │ archive  │
  ╰──────────╯       ╰───────────────╯       ╰──────────╯
       │                    │ ▲                    │
       │                    ╰─╯                    │
    markdown           iterate until            compress
    files as            satisfied              & preserve
    work units                                     │
                                                   ▼
                                              decompress
                                              to resume
```

- **Nursery**: The current nebula state. Markdown files sitting in a directory — potential energy waiting to be converted into work. `nebula validate`, `nebula plan`, `nebula apply` operate here.
- **Constellation**: After execution, the work has taken form. The user can review what was built, refactor phases, re-run specific phases, and iterate. The constellation is a living workspace. `nebula constellation` enters this mode.
- **Neutron**: When the user is satisfied, the nebula is compressed into a dense artifact — all context, decisions, outputs, diffs, and bead references packed into a portable format. `nebula neutron` compresses; `nebula expand` decompresses.

## Current State

**Nebula state tracking** (`internal/nebula/state.go`):
- `nebula.state.toml` tracks per-phase status (pending/done/failed), bead IDs, costs
- No concept of lifecycle stage — it's implicitly "nursery" when state is empty, "done" when all phases are terminal

**Nebula types** (`internal/nebula/types.go`):
- `Nebula` struct with `Manifest`, `Phases`, `Dir`
- `PhaseSpec` with execution params
- No lifecycle field

## Solution

### 1. Lifecycle Stage Type

Add a lifecycle stage to the nebula manifest and state:

```go
type LifecycleStage string

const (
    StageNursery       LifecycleStage = "nursery"
    StageConstellation LifecycleStage = "constellation"
    StageNeutron       LifecycleStage = "neutron"
)
```

### 2. State File Extension

Add `lifecycle_stage` to `nebula.state.toml`:

```toml
lifecycle_stage = "nursery"

[phases.setup-models]
status = "done"
bead_id = "quasar-abc"
# ...
```

Default is `"nursery"` (backwards compatible — existing nebulas without the field are nurseries).

### 3. Stage Transitions

Transitions are explicit commands or automatic:
- **nursery → constellation**: Automatic when all phases reach a terminal state (done/failed/skipped), OR manual via `nebula constellation <path>`
- **constellation → nursery**: If the user adds/modifies phases and re-runs (the constellation goes "back to the nursery")
- **constellation → neutron**: Manual via `nebula neutron <path>` when the user is satisfied
- **neutron → constellation**: Manual via `nebula expand <path>` to decompress and resume work

### 4. CLI Commands

```
quasar nebula constellation <path>  — Enter constellation mode (review/refactor)
quasar nebula neutron <path>        — Compress to neutron archive
quasar nebula expand <path>         — Decompress a neutron archive
quasar nebula lifecycle <path>      — Show current lifecycle stage
```

### 5. TUI Integration

The status bar shows the lifecycle stage:
```
 ✦ QUASAR  nebula: auth ☆constellation  4/6 done  $1.24  5m36s
```

Stage icons: `✧` nursery, `☆` constellation, `★` neutron

## Files to Modify

- `internal/nebula/types.go` — Add `LifecycleStage`, constants, add `Stage` field to state
- `internal/nebula/state.go` — Read/write `lifecycle_stage` in state file
- `internal/nebula/validate.go` — Validate stage transitions

## Files to Create

- `internal/nebula/lifecycle.go` — Stage transition logic, validation
- `internal/nebula/lifecycle_test.go` — Tests for transitions

## Acceptance Criteria

- [ ] `LifecycleStage` type with nursery/constellation/neutron values
- [ ] State file persists lifecycle stage (default: nursery)
- [ ] Existing nebulas without the field default to nursery (backwards compatible)
- [ ] Stage transitions are validated (can't go nursery → neutron directly)
- [ ] `go build` and `go test ./...` pass
