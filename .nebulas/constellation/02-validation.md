+++
id = "validation"
title = "Validate constellation manifest: DAG cycles, nebula existence, budget constraints"
type = "feature"
priority = 1
depends_on = ["manifest-types"]
scope = ["internal/constellation/validate.go", "internal/constellation/validate_test.go"]
+++

## Problem

A parsed `Constellation` from phase 1 may contain structural errors that would cause runtime failures: cyclic dependencies between nebulas, references to nebula directories that do not exist on disk, inconsistent shared context (entanglement names that no nebula produces), or budget allocations that exceed the total cap. These errors must be caught early — before any nebula execution begins — with clear, actionable error messages.

The existing `internal/nebula/validate.go` provides `Validate(*Nebula) error` which checks phase-level DAG integrity and scope overlaps. The constellation validator needs analogous checks at the nebula-of-nebulas level, reusing `internal/dag/` for cycle detection rather than reimplementing it.

## Solution

Create `internal/constellation/validate.go` with a `Validate(*Constellation) error` function that runs a series of checks and collects all errors (not fail-fast). Return a multi-error that lists every problem found.

### Validation checks

1. **Name uniqueness**: Every `NebulaRef.Name` must be unique within the constellation.

2. **Cycle detection**: Build a `dag.DAG` from `NebulaRef` entries and call `dag.AddEdge` for each `depends_on` relationship. If `AddEdge` returns `dag.ErrCycle`, record the cyclic pair.

3. **Nebula existence**: For each `NebulaRef.Path`, verify the directory exists on disk and contains a valid `nebula.toml`. Use `nebula.Parse()` to confirm the referenced nebula is structurally valid.

4. **Dependency references**: Every entry in `NebulaRef.DependsOn` must reference a `NebulaRef.Name` that exists in the constellation.

5. **Budget constraints**: If `BudgetPerNebulaUSD > 0` and `BudgetTotalUSD > 0`, verify that `BudgetPerNebulaUSD * len(Nebulas) <= BudgetTotalUSD` (warn, not error, since not all nebulas may use their full allocation).

6. **Shared entanglement consistency**: If `SharedContext.Entanglements` lists entanglement names, verify that at least one nebula's phases reference files or packages that could produce those entanglements (best-effort check via scope patterns).

7. **Failure strategy validity**: Verify `CoordinationRules.FailureStrategy` is one of the recognized values.

```go
// internal/constellation/validate.go

package constellation

import (
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "github.com/aaronsalm/quasar/internal/dag"
    "github.com/aaronsalm/quasar/internal/nebula"
)

// Validate checks a Constellation for structural errors. It returns a
// multi-error listing every problem found, or nil if the constellation
// is valid. Validation does not short-circuit on the first error.
func Validate(c *Constellation) error {
    var errs []error

    errs = append(errs, validateNames(c)...)
    errs = append(errs, validateDAG(c)...)
    errs = append(errs, validatePaths(c)...)
    errs = append(errs, validateBudget(c)...)
    errs = append(errs, validateFailureStrategy(c)...)

    return errors.Join(errs...)
}

// validateNames checks that all nebula names are unique and non-empty.
func validateNames(c *Constellation) []error {
    var errs []error
    seen := make(map[string]bool)
    for _, ref := range c.Nebulas {
        if ref.Name == "" {
            errs = append(errs, fmt.Errorf("nebula entry has empty name"))
            continue
        }
        if seen[ref.Name] {
            errs = append(errs, fmt.Errorf("duplicate nebula name: %q", ref.Name))
        }
        seen[ref.Name] = true
    }
    return errs
}

// validateDAG builds a dag.DAG from the nebula references and checks for
// cycles and dangling dependency references.
func validateDAG(c *Constellation) []error {
    var errs []error
    names := make(map[string]bool)
    for _, ref := range c.Nebulas {
        names[ref.Name] = true
    }

    g := dag.New()
    for _, ref := range c.Nebulas {
        g.AddNode(dag.Node{ID: ref.Name, Priority: 1})
    }

    for _, ref := range c.Nebulas {
        for _, dep := range ref.DependsOn {
            if !names[dep] {
                errs = append(errs, fmt.Errorf(
                    "nebula %q depends on %q which does not exist in the constellation",
                    ref.Name, dep))
                continue
            }
            if err := g.AddEdge(ref.Name, dep); err != nil {
                if errors.Is(err, dag.ErrCycle) {
                    errs = append(errs, fmt.Errorf(
                        "cycle detected: %q -> %q", ref.Name, dep))
                } else {
                    errs = append(errs, fmt.Errorf(
                        "DAG edge error %q -> %q: %w", ref.Name, dep, err))
                }
            }
        }
    }
    return errs
}

// validatePaths checks that each nebula's path exists on disk and contains
// a parseable nebula.toml.
func validatePaths(c *Constellation) []error {
    var errs []error
    for _, ref := range c.Nebulas {
        dir := ref.Path
        if !filepath.IsAbs(dir) {
            dir = filepath.Join(c.Dir, dir)
        }
        info, err := os.Stat(dir)
        if err != nil {
            errs = append(errs, fmt.Errorf(
                "nebula %q path %q: %w", ref.Name, ref.Path, err))
            continue
        }
        if !info.IsDir() {
            errs = append(errs, fmt.Errorf(
                "nebula %q path %q is not a directory", ref.Name, ref.Path))
            continue
        }
        // Attempt to parse the nebula manifest to catch structural errors early.
        if _, err := nebula.Parse(dir); err != nil {
            errs = append(errs, fmt.Errorf(
                "nebula %q at %q: %w", ref.Name, ref.Path, err))
        }
    }
    return errs
}

// validateBudget checks budget constraints for consistency.
func validateBudget(c *Constellation) []error {
    var errs []error
    if c.Coordination.BudgetTotalUSD < 0 {
        errs = append(errs, fmt.Errorf("budget_total_usd cannot be negative"))
    }
    if c.Coordination.BudgetPerNebulaUSD < 0 {
        errs = append(errs, fmt.Errorf("budget_per_nebula_usd cannot be negative"))
    }
    return errs
}

// validateFailureStrategy checks that the failure strategy is recognized.
func validateFailureStrategy(c *Constellation) []error {
    for _, s := range ValidFailureStrategies {
        if c.Coordination.FailureStrategy == s {
            return nil
        }
    }
    return []error{fmt.Errorf(
        "unrecognized failure_strategy %q (valid: %s)",
        c.Coordination.FailureStrategy,
        strings.Join(failureStrategyStrings(), ", "))}
}

func failureStrategyStrings() []string {
    out := make([]string, len(ValidFailureStrategies))
    for i, s := range ValidFailureStrategies {
        out[i] = string(s)
    }
    return out
}
```

## Files

- `internal/constellation/validate.go` — `Validate()` function with `validateNames`, `validateDAG`, `validatePaths`, `validateBudget`, `validateFailureStrategy` helpers
- `internal/constellation/validate_test.go` — table-driven tests: valid constellation, duplicate names, cycle detection, missing dependency reference, nonexistent path, invalid nebula.toml at path, negative budget, invalid failure strategy

## Acceptance Criteria

- [ ] `Validate()` returns nil for a structurally valid constellation with no cycles
- [ ] `Validate()` detects duplicate nebula names and returns a descriptive error
- [ ] `Validate()` detects cycles in the nebula DAG via `dag.AddEdge` returning `dag.ErrCycle`
- [ ] `Validate()` detects dangling dependency references (depends_on a name not in the constellation)
- [ ] `Validate()` checks that each nebula path exists and contains a parseable `nebula.toml`
- [ ] `Validate()` rejects negative budget values
- [ ] `Validate()` rejects unrecognized failure strategy values
- [ ] All errors are collected (not fail-fast) and returned as a joined multi-error
- [ ] `go test ./internal/constellation/...` passes
- [ ] `go vet ./internal/constellation/...` passes
