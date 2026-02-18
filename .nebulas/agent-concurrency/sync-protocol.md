+++
id = "sync-protocol"
title = "Design agent sync protocol for coordinated codebase work"
type = "feature"
priority = 2
depends_on = ["lock-manager"]
+++

## Problem

File locking prevents conflicts, but agents also need to sync their view of the codebase as changes are committed by other agents. An agent working on module B needs to see changes from the agent that just finished module A if there are dependencies.

## Solution

Design a sync protocol that keeps agents coordinated:

1. **Change notification**: When a worker completes a phase and commits, broadcast a change event containing:
   - Phase ID, changed file paths, commit SHA
   - Whether the change affects other phases' scope

2. **Refresh mechanism**: Workers that depend on changed files can:
   - Pull the latest changes (git pull / rebase onto the latest)
   - Re-read affected files before continuing

3. **Checkpoint sync**: At phase boundaries (between coder-reviewer cycles), workers check for upstream changes and integrate them.

4. **Conflict resolution strategy**:
   - If two phases have non-overlapping scope: no conflict possible
   - If scope overlaps: sequential execution (DAG dependency)
   - If unexpected conflict at commit time: pause and notify user

5. **Communication channel**: For local execution, use Go channels or a shared file-based event log. Design the interface so it can be swapped for a network protocol later.

## Files

- `internal/nebula/sync.go` — sync protocol interface and local impl
- `internal/nebula/sync_test.go` — tests for change notification and refresh
- `internal/nebula/worker.go` — integrate sync checkpoints

## Acceptance Criteria

- [ ] Change notification interface defined
- [ ] Workers can detect and integrate upstream changes at phase boundaries
- [ ] Non-overlapping scopes proceed without blocking
- [ ] Overlapping scopes are serialized via DAG dependencies
- [ ] Interface is pluggable for future distributed backends
- [ ] `go test ./internal/nebula/...` passes
