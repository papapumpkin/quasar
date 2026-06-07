+++
id = "entanglement-revival"
title = "Fix neutron to emit per-phase symbol manifests; surface entanglements in the TUI; wire master-reviewer to detect concurrent-coder collisions before they corrupt the tree"
type = "task"
priority = 2
depends_on = ["dead-coder-detection"]
scope = [
    "internal/neutron/**",
    "internal/fabric/entanglement_store.go",
    "internal/tui/entanglement_panel.go",
    "internal/runtime/collision_detector.go",
]
+++

## Problem

The `entanglements` table exists in SQLite (schema: `producer/consumer/kind/name/signature/package/status`) but it's empty. `internal/neutron/neutron.go` writes to the table — but apparently never runs on phase output, or runs but never matches. `internal/tui/boardview.go` reads from the table but the panel renders nothing because there's no data.

When two coders run concurrently on independent phases of the same nebula, they're supposed to coordinate via entanglements: "Phase A produces `MaxCycles int` in `internal/runtime`; Phase B consumes it." The master-reviewer would then refuse to start Phase B's coder until Phase A's coder has produced a manifest with the expected symbol. Without entanglements, both coders write into `internal/runtime/engine.go` blindly; one overwrites the other; the loser's work is lost. The "random fail" you saw with two concurrent master-reviewer coders is almost certainly this.

This phase fixes the producer (neutron), surfaces the consumer (TUI), and wires the safety check (master-reviewer collision detector).

## Solution

### Diagnose neutron first

Step 1 of this phase is investigation, not implementation. Read `internal/neutron/neutron.go` and determine:

- Is `Run()` actually invoked anywhere in the constellation runtime flow? Or is it dead code?
- If invoked, what's its trigger? End-of-phase? End-of-cycle? Pre-commit?
- What's its scan strategy? AST-based symbol extraction? Regex? Magic comments?
- What's the failure mode when it can't classify a symbol — does it emit a warning, skip, or panic?

The deliverable for step 1 is a 1-paragraph note appended to this phase's report explaining the *current* behavior. Step 2 onward addresses what's actually broken — which depends on what step 1 finds.

### Likely fix shape (informed guess)

Based on the schema (`producer`, `kind`, `name`, `signature`, `package`), neutron probably scans phase commits for newly-exported Go symbols. Likely gaps:

1. **It's never invoked.** The simplest possibility — `Run()` exists but no caller. Wire it into the constellation runtime's per-cycle post-commit hook.
2. **It scans the wrong revision.** Compares the wrong base ref (e.g. HEAD instead of the cycle-start commit), so no symbols look "new."
3. **It's invoked but its INSERTs silently fail.** Permission issue, or the unique constraint is wrong for the data shape.
4. **It runs but excludes the directories that matter.** Skip-list inherited from a prior phase still excludes `internal/runtime/` or `internal/sensors/`.

Each diagnosis has a different fix. Step 2 is "implement the fix that step 1's diagnosis indicates."

### Wire into the constellation runtime

Once neutron emits, add a hook in `internal/runtime/engine.go` after every successful cycle commit:

```go
// After a green-build commit, scan the diff for produced symbols and
// declare entanglements for them. Idempotent — re-running on the same
// commit doesn't duplicate rows (UNIQUE on producer, kind, name).
func (e *Engine) emitEntanglements(ctx context.Context, runID, commitSHA string) error {
    symbols, err := e.neutron.Scan(ctx, e.workdir, commitSHA)
    if err != nil { return err }
    for _, s := range symbols {
        if err := e.entanglements.Declare(ctx, Entanglement{
            Producer:  runID,
            Kind:      s.Kind,        // "func" | "type" | "interface" | "table" | ...
            Name:      s.Name,
            Signature: s.Signature,
            Package:   s.Package,
            Status:    "pending",     // becomes "fulfilled" once a consumer claims it
        }); err != nil {
            // log, don't fail the cycle
        }
    }
    return nil
}
```

### Collision detector for concurrent coders

`internal/runtime/collision_detector.go`:

```go
// CollisionDetector consults the entanglements table before starting a new
// coder. If another in-flight cycle has already declared production of a
// symbol this phase's spec references, the new coder is paused until the
// other producer's cycle completes.
type CollisionDetector struct {
    store *EntanglementStore
}

func (c *CollisionDetector) WouldCollide(ctx context.Context, phaseSpec *PhaseSpec, scope ScopeGlobs) ([]Collision, error)

type Collision struct {
    Symbol       string
    OtherRunID   string
    OtherPhaseID string
    Kind         string
    // Recommended action: "wait" | "abandon" | "merge_after"
    Action string
    Reason string
}
```

The phase spec's `scope = [glob...]` block is the key. If two phases' scope globs overlap AND one is already running, the second waits. This converts "random concurrent fail" into "deterministic wait" — a strict improvement.

For the master-reviewer-loop-hardening nebula specifically (which we know triggered the collision), this would have queued Phase 02 (budget-propagation) to wait until Phase 01 (cycle-limit) committed, instead of running them simultaneously into the same `internal/runtime/engine.go` file.

### TUI surface

`internal/tui/entanglement_panel.go`:

```
Entanglements (5 active)
  ✓ runtime.CycleGuard.RecordEntry      master-reviewer/01  → pending
  ✓ runtime.Budget.RecordCost           master-reviewer/02  → pending
  ✓ stars.MasterReviewer                master-reviewer/00  → fulfilled
  ⚠ Symbol collision: 2 phases want internal/runtime/engine.go
```

The bottom warning line is the new collision-detector output rendered in the TUI fleet detail view. Operator sees the conflict before the run dies.

### Tests

- Neutron unit tests for the actual fix in step 1 (depends on what we find)
- `collision_detector_test.go`: two fixture phases with overlapping scope → WouldCollide returns one Collision
- `entanglement_store_test.go`: Declare is idempotent on UNIQUE constraint; Claim transitions `status` to fulfilled
- Integration: fire two phases concurrently in a fixture nebula, verify the second waits

## Files

- `internal/neutron/neutron.go` (modify per step-1 diagnosis)
- `internal/neutron/neutron_test.go` (modify)
- `internal/fabric/entanglement_store.go` (new) — typed Go API over the existing table
- `internal/fabric/entanglement_store_test.go` (new)
- `internal/runtime/engine.go` (modify) — wire neutron scan into post-commit hook
- `internal/runtime/collision_detector.go` (new)
- `internal/runtime/collision_detector_test.go` (new)
- `internal/tui/entanglement_panel.go` (new)
- `internal/tui/entanglement_panel_test.go` (new)

## Acceptance Criteria

- [ ] Step 1 diagnosis: a paragraph in this phase's review report explaining what neutron currently does
- [ ] Step 2 fix: after running a fixture phase that produces a new exported function, the `entanglements` table contains a row with the correct producer/kind/name/signature
- [ ] Idempotent: re-running the post-commit hook on the same commit does not produce duplicate rows
- [ ] `CollisionDetector.WouldCollide` returns at least one `Collision` when two fixture phases have overlapping scope globs
- [ ] The master-reviewer constellation, given two concurrent phases with overlapping scope, starts only one at a time
- [ ] TUI entanglement panel renders the current entanglements + any active collisions
- [ ] No new external dependencies for symbol extraction (use `go/ast` from stdlib)
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
