+++
id = "handle-modified-tasks"
title = "Hot-reload modified task descriptions"
type = "task"
priority = 2
depends_on = ["consume-watch-events"]
+++

## Problem

When a user edits a task `.md` file while the nebula is running, the change is detected by the watcher but nothing happens. Tasks that haven't started yet should pick up the new description.

## Solution

Implement `handleChange` for `ChangeModified` in `internal/nebula/worker.go`:

1. Re-parse the modified task file using `parseTaskFile`.
2. If the task is **not yet started** (not in-flight, not done, not failed): update `Nebula.Tasks` and the `tasksByID` map with the new description/metadata.
3. If the task is **in-flight or completed**: log a warning via `OnProgress` or a new `OnWatchEvent` callback — the change will not apply to the current run.
4. Guard all map access with `wg.mu`.

### Edge cases

- File parse error (invalid TOML frontmatter): log and skip, do not crash.
- Task ID changed in the file: treat as remove old + add new (or log a warning).

## Files to Modify

- `internal/nebula/worker.go` — Implement `ChangeModified` handling in `handleChange`

## Acceptance Criteria

- [ ] Modified task file updates the task description for pending tasks
- [ ] In-flight or completed tasks are not affected by modifications
- [ ] Parse errors are logged, not fatal
- [ ] Existing tests pass, no data races
