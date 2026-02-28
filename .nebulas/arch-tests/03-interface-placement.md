+++
id = "interface-placement"
title = "Verify interfaces are defined at consumers, not implementers"
type = "task"
priority = 2
depends_on = ["arch-foundation"]
scope = ["internal/arch_test/interfaces_test.go"]
+++

## Problem

A core Go convention in this project is "define interfaces where they are consumed, not where they are implemented." Violations create unnecessary coupling — consumers end up importing implementation packages just for the interface type. Without automated checks, this convention erodes during large refactors.

## Solution

Create `internal/arch_test/interfaces_test.go` that scans for interface declarations and verifies they follow the consumer-side pattern.

### Approach

1. **Scan all packages** for interface declarations using the `interfaceDecls` helper.
2. **For each interface**, find all concrete implementations across the codebase by scanning for types whose method sets satisfy the interface.
3. **Flag violations** where an interface is defined in the same package as its sole (or primary) implementation.

### Pragmatic heuristics

Full interface-satisfaction checking via `go/ast` alone is complex. Use a simpler proxy:

1. **Scan for interface declarations** in each package.
2. **Check if the same package contains a struct type with methods matching all interface methods** (name + arity match is sufficient — no need for full type checking).
3. If yes, flag it: `"interface %s.%s appears to be implemented in the same package; consider moving to the consumer"`

### Known valid co-locations (allowlist)

Some interfaces legitimately live with their implementation (e.g. stdlib patterns, test helpers). Maintain an allowlist:

```go
var allowedColocations = map[string][]string{
    // Add any legitimate cases discovered during implementation
}
```

### Test function

`TestInterfacePlacement(t *testing.T)`:
- For each internal package, find all interface declarations
- For each interface, check if any struct in the same package has methods matching all interface methods
- If found and not in allowlist, fail with: `"interface %s defined in %s but struct %s in same package implements it; move interface to consumer"`
- Subtests per package for clear output

## Files

- `internal/arch_test/interfaces_test.go` — interface placement tests (new file)

## Acceptance Criteria

- [ ] Test discovers all interface declarations across `internal/`
- [ ] No false positives on current codebase (all legitimate co-locations are allowlisted)
- [ ] Moving an interface from consumer to implementer package would fail this test
- [ ] `go test ./internal/arch_test/...` passes
