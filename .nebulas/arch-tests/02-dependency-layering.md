+++
id = "dependency-layering"
title = "Enforce package dependency DAG with layering tests"
type = "task"
priority = 1
depends_on = ["arch-foundation"]
scope = ["internal/arch_test/layering_test.go"]
+++

## Problem

The codebase has an implicit layering where lower-level packages (agent, config) should never import higher-level ones (nebula, tui). Without automated enforcement, refactors can accidentally introduce upward imports that create tight coupling or even import cycles.

## Solution

Create `internal/arch_test/layering_test.go` that encodes the desired dependency DAG and fails on violations.

### Layer definitions

```go
var layers = map[string]int{
    "agent":     0,
    "ansi":      0,
    "beads":     0,
    "config":    0,
    "dag":       0,
    "filter":    0,
    "snapshot":  0,
    "telemetry": 0,

    "claude":  1,
    "fabric":  1,

    "neutron": 2,
    "tycho":   2,

    "loop":   3,

    "nebula":  4,

    "ui":      5,

    "tui":     6,
}
```

### Rules

1. **No upward imports**: A package at layer N may only import packages at layer N or below.
2. **No unknown packages**: Any internal package not in the layer map (except `board` and `arch_test`) fails the test, forcing developers to assign new packages a layer.

### Known exceptions

The current codebase has one known layering violation:

- `ui` (layer 1 in ideal model, layer 5 in reality) imports `nebula` (layer 4). This is documented as a `TODO` in the test with a comment explaining the aspirational fix: extract the nebula-dependent types from `ui` into a separate package or move them into `nebula`.

Encode this as:

```go
var allowedExceptions = map[string]map[string]string{
    "ui": {
        "nebula": "TODO: ui imports nebula for plan rendering types; extract to break this dependency",
    },
}
```

The test should still log the exception as a warning so it stays visible.

### Test function

`TestDependencyLayering(t *testing.T)`:
- Iterate all internal packages via `internalPackages(t)`
- For each, get its imports via `importsOf(t, pkgDir)`
- For each import, verify `layers[importer] >= layers[imported]`
- If violated and not in `allowedExceptions`, fail with clear message: `"layer violation: %s (layer %d) imports %s (layer %d)"`
- If violated and in `allowedExceptions`, log `t.Logf("known exception: ...")`

`TestNoUnknownPackages(t *testing.T)`:
- Verify every package from `internalPackages(t)` exists in `layers`
- Fail with: `"package %s has no layer assignment; add it to the layers map"`

## Files

- `internal/arch_test/layering_test.go` — layer definitions, exception list, test functions (new file)

## Acceptance Criteria

- [ ] `TestDependencyLayering` passes with zero unexpected violations
- [ ] `TestNoUnknownPackages` passes — all current packages have layer assignments
- [ ] Known `ui → nebula` exception is logged but does not fail
- [ ] Adding a new package without a layer assignment causes `TestNoUnknownPackages` to fail
- [ ] Introducing an upward import (e.g. `agent` importing `loop`) causes `TestDependencyLayering` to fail
