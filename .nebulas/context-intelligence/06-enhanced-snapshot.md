+++
id = "enhanced-snapshot"
title = "Enrich fabric snapshot with actionable detail"
type = "feature"
priority = 2
depends_on = ["fabric-auto-inject"]
scope = ["internal/fabric/snapshot.go", "internal/fabric/snapshot_test.go"]
+++

## Problem

The current `RenderSnapshot` function in `internal/fabric/snapshot.go` produces a functional but sparse representation. Entanglements show kind and name/signature but lack file locations. File claims show path and owner but not what the owner is doing. Discoveries show kind and detail but don't indicate urgency or age.

When agents receive this snapshot, they often still need to run additional commands to understand the implications. A richer snapshot reduces agent turns and improves decision quality.

## Solution

### 1. Entanglement file locations

When rendering entanglements, include the source file path if available. The `Publisher` already records this information — we just need to surface it:

```markdown
#### fabric (from: phase-fabric-rename)
- interface Fabric: SetPhaseState, GetPhaseState, ... (internal/fabric/fabric.go)
- function NewSQLiteFabric(path string) (*SQLiteFabric, error) (internal/fabric/sqlite.go)
```

This requires extending the `Entanglement` struct or adding a `File` field.

### 2. Claim context

Show what phase is actively working on claimed files. Cross-reference claims with phase states:

```markdown
### Active File Claims
- internal/loop/loop.go → phase-context-inject (in_progress, cycle 2/5)
- internal/agent/prompt.go → phase-scanner-wire (in_progress, cycle 1/5)
```

This requires the snapshot builder to receive phase state information alongside claims.

### 3. Discovery urgency

Add timestamps and staleness to discoveries. An unresolved discovery from 10 minutes ago is more urgent than a fresh one:

```markdown
### Unresolved Discoveries
- [file_conflict] internal/fabric/sqlite.go claimed by both phase-a and phase-b (from: phase-b, 3m ago)
- [entanglement_dispute] Invoker interface signature mismatch (from: phase-c, just now)
```

### 4. Pulse grouping

Group pulses by kind for faster scanning:

```markdown
### Shared Context
**Decisions:**
- [phase-a] Switched from interface{} to generics for type safety
- [phase-b] Using WAL mode for concurrent read access

**Failures:**
- [phase-c] gosec fails on unchecked error in line 42 — known issue, workaround applied
```

### 5. Snapshot summary line

Add a one-line summary at the top for quick orientation:

```markdown
## Fabric State (3 completed, 2 in-progress, 1 blocked, 15 entanglements, 2 unresolved)
```

### 6. Backward compatibility

The enhanced format is a superset of the current output. All existing tests that check for substring presence will continue to pass. New tests verify the enhanced detail.

## Files

- `internal/fabric/snapshot.go` — Enhance `RenderSnapshot` with file locations, claim context, discovery urgency, pulse grouping, summary line
- `internal/fabric/snapshot_test.go` — Tests for enhanced rendering
- `internal/fabric/fabric.go` — Add `File` field to `Entanglement` if not already present

## Acceptance Criteria

- [ ] Entanglement rendering includes source file paths when available
- [ ] File claim rendering includes phase status and cycle info when available
- [ ] Discoveries show relative timestamps (e.g., "3m ago", "just now")
- [ ] Pulses are grouped by kind (decisions, failures, notes, reviewer_feedback)
- [ ] Summary line at top shows counts of completed/in-progress/blocked/entanglements/unresolved
- [ ] All existing `snapshot_test.go` tests continue to pass
- [ ] `go test ./internal/fabric/...` passes
- [ ] `go vet ./...` clean
