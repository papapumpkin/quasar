+++
id = "nebula-wiring"
title = "Wire context snapshot into nebula apply pipeline"
type = "feature"
priority = 1
depends_on = ["agent-context-prefix"]
+++

## Problem

The context scanner and agent prefix support exist but aren't connected to the nebula execution pipeline. We need to generate the snapshot at nebula-apply time and propagate it through to every phase's agent invocations.

## Solution

### 1. Generate snapshot in `cmd/nebula_apply.go`

At the start of `runNebulaApply`, after loading and validating the nebula but before starting workers:

```go
var contextPrefix string
if !cfg.NoContext { // new config flag
    scanner := context.NewScanner(context.DefaultConfig())
    snapshot, err := scanner.Scan(ctx, workDir)
    if err != nil {
        // Non-fatal — log warning and continue without context
        printer.Warning("context scan failed: %v", err)
    } else {
        contextPrefix = snapshot
    }
}
```

### 2. Propagate through adapters

Add `contextPrefix string` field to both `loopAdapter` and `tuiLoopAdapter` in `cmd/nebula_adapters.go`. In their `RunExistingPhase` methods, set `l.ContextPrefix = a.contextPrefix` on the loop they construct.

### 3. Add to `WorkerGroup` (optional but clean)

Add `ContextPrefix string` to `WorkerGroup` so it's available alongside other global config. The adapters read from it when constructing loops.

Alternatively, the adapters can capture it via closure when constructed in `runNebulaApply` — simpler, less plumbing.

### 4. Also wire for single-task `quasar run`

In `cmd/run.go`, generate context the same way and pass to `loop.Loop.ContextPrefix`. This gives the cache benefit even for single-task runs (coder + reviewer share the prefix).

## Files

- `cmd/nebula_apply.go` — generate snapshot, pass to adapters
- `cmd/nebula_adapters.go` — add `contextPrefix` field, wire to `Loop.ContextPrefix`
- `cmd/run.go` — generate and wire context for single-task mode

## Acceptance Criteria

- [ ] Context snapshot is generated once at the start of `nebula apply`
- [ ] Same snapshot is used for all phase invocations in the run
- [ ] Adapters propagate `contextPrefix` to every `loop.Loop` they construct
- [ ] `quasar run` also generates and uses context
- [ ] Context generation failure is non-fatal (warning + continue)
- [ ] Existing behavior unchanged when context is empty or disabled
