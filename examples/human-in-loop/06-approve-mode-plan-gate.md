+++
id = "approve-mode-plan-gate"
title = "Add plan-level approval gate for approve mode"
type = "feature"
priority = 2
depends_on = ["gate-mode-implementation"]
+++

## Problem

The `approve` gate mode should gate the execution plan *before* any phases run, in addition to gating each phase. Currently, `review` and `approve` behave identically — both only gate at phase boundaries.

## Solution

When `approve` mode is active, display the execution plan and require human approval before the worker group begins executing phases.

### Plan Display

Before `WorkerGroup.Run` starts dispatching phases, if gate mode is `approve`:

1. Build a plan summary showing the execution order (waves of parallelizable phases)
2. Display it via `RenderPlan`:

```
── Nebula: CI/CD Pipeline (approve mode) ─────────
   Wave 1 (parallel): test, vet, lint, fmt, security
   Wave 2:            build
   Wave 3:            ci-workflow
   Wave 4:            release-workflow

   Phases: 8 | Budget: $50.00 | Gate: approve
───────────────────────────────────────────────────
   [a]pprove  [s]kip (abort)
   > _
```

3. Call `Gater.Prompt` with a plan-level checkpoint (no diff, just the plan)
4. On `accept`: proceed with execution
5. On `skip`/`reject`: abort without running any phases

### Implementation

- Add a `PromptPlan` method to `Gater` (or reuse `Prompt` with a plan-typed checkpoint)
- Add a `RenderPlan(w io.Writer, plan *Plan, gate GateMode)` function
- Call this at the top of `WorkerGroup.Run` before the main loop

### Wave Computation

Reuse or expose the topological ordering from `graph.go` to group phases into waves (sets of phases whose dependencies are all satisfied). This is already implicitly computed by `filterEligible` but not exposed as a data structure.

## Files to Modify

- `internal/nebula/gate.go` — Add plan-level prompting
- `internal/nebula/worker.go` — Call plan gate at start of `Run`
- `internal/nebula/plan.go` — Add `RenderPlan` or `ComputeWaves` function
- `internal/nebula/graph.go` — Expose wave grouping if needed

## Acceptance Criteria

- [ ] `approve` mode displays execution plan before any work begins
- [ ] Plan shows phases grouped into dependency waves
- [ ] Human can approve or abort the plan
- [ ] Abort prevents any phases from executing
- [ ] `review` mode does NOT show the plan gate (only phase gates)
- [ ] `trust` and `watch` modes skip the plan gate entirely
- [ ] `go test ./internal/nebula/...` passes