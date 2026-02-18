+++
id = "cmd-entry-point"
title = "Add CLI entry point that launches the home TUI"
type = "feature"
priority = 2
depends_on = ["home-key-handling"]
+++

## Problem

There's no CLI command to launch the Quasar TUI in "home" mode. Currently you must run `quasar nebula apply .nebulas/X --auto` to start a specific nebula. We need a way to just run `quasar` (or `quasar tui`) in a project directory and get the landing page.

## Current State

- `cmd/root.go` — Root command with no `Run` function (just shows help)
- `cmd/nebula_apply.go` — `runNebulaApply()` handles the full lifecycle: load, plan, apply, TUI/stderr, worker loop
- The next-nebula loop in `runNebulaApply` (TUI path) already handles re-launching with a different nebula via `appModel.NextNebula`

## Solution

### 1. Add a `quasar tui` subcommand (or default root behavior)

Create `cmd/tui.go` with a new subcommand:

```bash
quasar tui                    # launch home screen, discovers .nebulas/ in cwd
quasar tui --dir ./project    # specify a different directory
quasar tui --no-splash        # skip splash animation
```

Alternatively, if the root command has no args and `.nebulas/` exists in cwd, auto-launch the TUI. This feels more natural — just `cd` into your project and run `quasar`.

### 2. Home-to-execution loop

The command implements a loop:

```
1. Discover .nebulas/ → build NebulaChoice list
2. Launch HomeProgram (splash → landing page)
3. User selects a nebula → HomeProgram.Run() returns with SelectedNebula set
4. Load and validate the selected nebula
5. Build plan, apply bead changes
6. Launch NebulaProgram for execution (reuse existing flow from runNebulaApply)
7. When nebula completes → overlay shown → user can quit or pick another
8. If user quits from overlay → return to step 1 (re-discover, may have new status)
9. If user quits from home → exit
```

### 3. Reuse existing infrastructure

The execution phase (steps 4-7) reuses the bulk of `runNebulaApply`'s TUI path. Extract the common setup (WorkerGroup creation, tuiLoopAdapter wiring, watcher setup) into a shared helper or call into `runNebulaApply`'s inner logic.

### 4. Flags

- `--no-splash` — skip splash animation
- `--no-tui` — error: this command requires a TTY
- `--dir` — override the directory to scan for `.nebulas/`
- `--max-workers`, `--verbose` — forwarded to nebula execution

## Files to Modify

- `cmd/tui.go` — New file with `tui` subcommand and home-to-execution loop
- `cmd/root.go` — Optionally wire default behavior to launch TUI when `.nebulas/` exists
- `cmd/nebula_apply.go` — Extract reusable nebula execution setup into a helper (or keep inline)

## Acceptance Criteria

- [ ] `quasar tui` launches the home screen TUI
- [ ] Discovers all nebulas in `.nebulas/` of the working directory
- [ ] Selecting a nebula starts execution in the existing nebula TUI view
- [ ] After nebula completion, the completion overlay is shown
- [ ] Dismissing the overlay returns to the home screen (re-discovers nebula status)
- [ ] q from the home screen exits cleanly
- [ ] `quasar nebula apply` path remains fully functional and unchanged
- [ ] `--no-splash` flag works
- [ ] Error message if no `.nebulas/` directory found
- [ ] `go build` and `go vet ./...` pass