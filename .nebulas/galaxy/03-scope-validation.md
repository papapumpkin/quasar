+++
id = "scope-validation"
title = "Add scope overlap detection to Validate()"
type = "feature"
priority = 1
depends_on = ["scope-fields", "graph-has-path"]
scope = ["internal/nebula/validate.go"]
+++

## Problem

With scope declarations on phases, we need to detect when two parallel phases claim overlapping file regions and surface this as a validation error.

## Solution

Add a new validation pass to `Validate()` in `internal/nebula/validate.go`, after the existing cycle detection block.

### Overlap detection logic

Extract a helper `scopesOverlap(a, b []string) (string, string, bool)`:

**Any containment = overlap.** For each pattern pair:

1. **Directory containment:** Normalize with `filepath.Clean`. If pattern A is a prefix of pattern B or vice versa, they overlap. E.g., `internal/` contains `internal/api/middleware/`.
2. **Glob overlap:** For `*` patterns, use `filepath.Match`. For `**` patterns, strip the glob suffix and compare directory prefixes for containment.
3. **Exact match:** If both are literal paths and equal, they overlap.

Returns `(patternA, patternB, overlaps)`.

### Validation pass

1. Collect phases with non-empty `Scope`
2. Build `Graph` from all phases (reuse `NewGraph`)
3. For each unordered pair of scoped phases (A, B):
   - Skip if `graph.Connected(A.ID, B.ID)` (serialized by dependency)
   - Skip if A.AllowScopeOverlap || B.AllowScopeOverlap
   - Call `scopesOverlap(A.Scope, B.Scope)`
   - If overlap → emit `ValidationError` with `ErrScopeOverlap`
4. Error message: `scope overlap: phases "X" and "Y" both match "pattern"; add a dependency or narrow scopes`

## Files to Modify

- `internal/nebula/validate.go` — Add scope overlap pass + scopesOverlap helper

## Acceptance Criteria

- [ ] Overlapping scopes with no dependency → hard validation error
- [ ] Overlapping scopes with dependency → no error (serialized)
- [ ] Overlapping scopes with allow_scope_overlap → no error
- [ ] Unscoped phases → no overlap checking
- [ ] Directory containment detected (internal/ contains internal/api/)
- [ ] Error message includes both phase IDs and the overlapping pattern
- [ ] `go vet ./...` passes
