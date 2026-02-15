+++
id = "handle-removed-tasks"
title = "Handle removed task files gracefully"
type = "task"
priority = 2
depends_on = ["consume-watch-events"]
+++

## Problem

If a user deletes a task `.md` file during execution, the watcher emits a `ChangeRemoved` event but nothing handles it. Tasks that haven't started should be skipped; in-flight tasks need a decision.

## Solution

Implement `handleChange` for `ChangeRemoved` in `internal/nebula/worker.go`:

1. Identify the task by file path (the `Change.TaskID` may be empty for removals since the file can't be parsed).
2. If the task is **pending** (not started): mark it as skipped by adding it to the `done` set and recording a result with a "skipped (file removed)" status.
3. If the task is **in-flight**: log a warning but let it finish. Do not cancel running work.
4. If the task is **already done**: no action needed.
5. Update the graph: tasks that depended on the removed task should be unblocked (treat removed task as "done").

### File-to-task mapping

The `Change` for removals has `TaskID: ""` because the file can't be parsed after deletion. Maintain a `fileToTaskID map[string]string` populated at init and updated on add/modify events so removals can be resolved.

## Files to Modify

- `internal/nebula/worker.go` — Implement `ChangeRemoved` handling, add `fileToTaskID` map

## Acceptance Criteria

- [ ] Pending tasks whose files are removed are skipped
- [ ] In-flight tasks are not interrupted when their file is removed
- [ ] Tasks depending on removed tasks become unblocked
- [ ] Existing tests pass, no data races
