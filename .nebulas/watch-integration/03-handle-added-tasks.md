+++
id = "handle-added-tasks"
title = "Dynamically add new task files during execution"
type = "task"
priority = 2
depends_on = ["consume-watch-events"]
+++

## Problem

If a user drops a new `.md` task file into the nebula directory while workers are running, it should be picked up and executed (respecting dependencies).

## Solution

Implement `handleChange` for `ChangeAdded` in `internal/nebula/worker.go`:

1. Parse the new task file using `parseTaskFile`.
2. Add the task to `Nebula.Tasks` and `tasksByID`.
3. Rebuild the dependency graph (call `NewGraph` or add the task to the existing graph).
4. The main loop will naturally pick up the new task on its next iteration if it becomes eligible.

### Design considerations

- The new task's `depends_on` may reference existing tasks. If dependencies are already done, it becomes immediately eligible.
- If `depends_on` references unknown task IDs, log a warning — the task will remain blocked.
- Duplicate task IDs (file has same ID as existing task): log a warning and skip.

## Files to Modify

- `internal/nebula/worker.go` — Implement `ChangeAdded` handling
- `internal/nebula/graph.go` — May need an `AddTask` method if graph rebuild is too expensive

## Acceptance Criteria

- [ ] New task files added during execution are parsed and added to the work queue
- [ ] Dependencies are respected for dynamically added tasks
- [ ] Duplicate task IDs are rejected with a warning
- [ ] Existing tests pass, no data races
