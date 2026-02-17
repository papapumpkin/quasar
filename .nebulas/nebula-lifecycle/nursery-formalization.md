+++
id = "nursery-formalization"
title = "Formalize nursery as the named creation phase of the lifecycle"
type = "feature"
priority = 1
depends_on = ["lifecycle-state-machine"]
+++

## Problem

The current nebula behavior (create markdown files, validate, plan, apply) is implicitly the "nursery" stage but isn't named or treated as part of a lifecycle. To make the lifecycle feel cohesive, the nursery needs to be explicitly recognized — not changed, just named and integrated into the state machine.

## Current State

- `nebula validate` checks structure
- `nebula plan` previews bead changes
- `nebula apply` creates beads and optionally runs workers
- `nebula show` displays current state
- `nebula status` shows post-run metrics
- No lifecycle awareness — these commands work on nebulas regardless of state

## Solution

### 1. Nursery Is the Default

All existing nebulas without a `lifecycle_stage` field are in the nursery. No migration needed — the default is `"nursery"`.

### 2. Command Awareness

Existing commands should be lifecycle-aware:
- `nebula validate/plan/apply` — only valid in nursery stage
- Running `nebula apply` on a constellation warns: "This nebula is in constellation stage. Reset to nursery? [y/n]"
- Running `nebula apply` on a neutron errors: "This nebula is archived. Use `nebula expand` first."

### 3. Nursery → Constellation Transition

When `nebula apply --auto` completes (all phases terminal), the state file transitions to constellation:

```toml
lifecycle_stage = "constellation"
completed_at = "2026-02-16T14:30:00Z"
```

This is automatic — the user doesn't need to run a separate command. The TUI shows the transition: "All phases complete. Entering constellation mode..."

For non-TUI (stderr) mode, print: "Nebula complete. Run `quasar nebula constellation <path>` to review."

### 4. Nursery Status Display

The TUI and `nebula show` display the stage:

```
Nebula: auth-feature [✧ nursery]
6 phases: 0/6 done
```

```
Nebula: auth-feature [☆ constellation]
6 phases: 5 done, 1 failed — ready for review
```

### 5. Re-entering Nursery

If a user in constellation mode re-runs or adds phases, the stage goes back to nursery while execution is in progress, then returns to constellation when complete. This is the natural "back to the nursery" cycle.

## Files to Modify

- `internal/nebula/state.go` — Default `lifecycle_stage = "nursery"`; auto-transition to constellation on completion
- `internal/nebula/worker.go` — Write constellation transition after `Run()` completes
- `cmd/nebula.go` — Add lifecycle stage guards to validate/plan/apply commands
- `internal/tui/statusbar.go` — Show lifecycle stage indicator
- `internal/tui/model.go` — Handle constellation transition message

## Acceptance Criteria

- [ ] Existing nebulas default to nursery (no migration)
- [ ] `nebula apply` warns when run on a constellation
- [ ] `nebula apply` errors when run on a neutron
- [ ] Auto-transition to constellation when all phases complete
- [ ] TUI shows lifecycle stage in status bar
- [ ] `go build` and `go test ./...` pass
