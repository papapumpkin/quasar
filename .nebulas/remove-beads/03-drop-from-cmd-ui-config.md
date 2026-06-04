+++
id = "drop-from-cmd-ui-config"
title = "Remove beads from cmd, tui, ui, checkpoint, and config layers"
type = "task"
priority = 1
depends_on = ["drop-from-nebula"]
scope = [
    "cmd/run.go",
    "cmd/validate.go",
    "cmd/nebula_adapters.go",
    "cmd/nebula_apply.go",
    "cmd/tui.go",
    "cmd/nebula.go",
    "cmd/fabric_read.go",
    "internal/tui/beadview.go",
    "internal/ui/nebula.go",
    "internal/ui/ui_test.go",
    "internal/checkpoint/checkpoint.go",
    "internal/config/config.go",
    "internal/config/config_test.go",
]
+++

## Problem

The cmd and UI layers wire user-facing commands to the nebula/loop layers and inject the beads client at construction time. With phases 1 and 2 having removed `Beads` from the underlying APIs, this layer now fails to compile until the corresponding call sites are updated. There is also one cmd file (`cmd/fabric_read.go`) and one tui file (`internal/tui/beadview.go`) that exist solely to surface beads state — these are deleted outright. Beads config keys in `internal/config/` are removed since the user can no longer configure something that does not exist.

## Solution

Remove all beads imports and references from the cmd, ui, tui (only the beadview file — the rest of the TUI is dealt with in Nebula 4), checkpoint, and config layers. Delete the two beads-only files. Update Cobra command construction to drop beads flags and beads-arg passing. Update config schema and tests to not require/parse beads keys.

### Concrete changes

**Delete entire files:**
- `cmd/fabric_read.go` — this command reads beads-tracked fabric state and is no longer meaningful
- `internal/tui/beadview.go` — the TUI's per-bead inspection pane; the rest of the TUI is touched only minimally here (whatever it takes to compile after beadview is gone)

**`cmd/run.go`, `cmd/nebula_apply.go`:**
- Remove beads import
- Remove `--beads-*` CLI flags from the Cobra command definitions
- Remove `beads.NewClient(...)` instantiation
- Update the `nebula.Apply(...)` / `loop.NewLoop(...)` call sites to phase-1/phase-2 signatures (no beads arg)

**`cmd/validate.go`:**
- Remove beads import
- If validate used to check bead reachability or seed beads as a dry-run, remove those checks

**`cmd/nebula_adapters.go`:**
- This file likely contains adapter functions that wrap nebula/loop construction with beads injection. Remove beads import and any adapter that exists solely to thread the beads client.
- Adapters for non-beads concerns stay.

**`cmd/nebula.go`, `cmd/tui.go`:**
- Remove beads import
- If `cmd/tui.go` registers a `quasar tui beads` subcommand or beadview route, remove that route registration
- Keep all other subcommand registrations intact

**`internal/tui/beadview.go`:**
- Delete entire file
- The TUI's main `model.go` and other files that reference `beadview.Model` (or similar) must be updated to remove that reference. Touch the minimum number of lines needed to restore compilation — do NOT refactor the TUI in this phase (full TUI replacement is Nebula 4). Acceptable changes: delete the import, delete the field, delete one switch case in `Update`, delete one entry in a tab/page list.

**`internal/ui/nebula.go`:**
- Remove beads import
- Remove rendering helpers that displayed bead status next to phase status
- Phase status display stays; just the bead annotation goes

**`internal/ui/ui_test.go`:**
- Remove test cases that asserted bead annotations in rendered output
- Update mock data to not include beads

**`internal/checkpoint/checkpoint.go`:**
- Remove beads import
- If checkpoint state included a beads snapshot, drop that field
- Existing checkpoint files on disk that include the old field will be tolerated by the decoder (existing TOML/JSON decoding behavior likely ignores unknown fields; if it does not, add tolerance for one release)

**`internal/config/config.go`:**
- Remove beads keys from the Config struct (e.g., `Beads BeadsConfig`)
- Remove `BeadsConfig` type if it existed
- Remove default values for beads keys
- Remove Viper bindings for beads keys

**`internal/config/config_test.go`:**
- Remove test cases that loaded beads config
- Adjust default-config assertions

### Migration notes

- `.quasar.yaml` files in user repos may contain a `[beads]` block. After this phase, that block is ignored (TOML decoding will skip unknown sections by default). No active migration required; users can delete the block at their leisure.
- The `configs/default.yaml` reference file is scrubbed in phase 4, not here. Do not touch it from this phase.

### Verification within this phase

```bash
go build ./...
go vet ./...
go test ./cmd/... ./internal/ui/... ./internal/tui/... ./internal/checkpoint/... ./internal/config/...
grep -rn "beads\|bead_" cmd/ internal/ui/ internal/tui/beadview* internal/checkpoint/ internal/config/ 2>/dev/null
```

The full repo build (`go build ./...`) must now succeed — this is the first phase where it does. Vet and tests for the touched packages must pass. The grep must return no matches (the `internal/tui/beadview.go` file is gone).

Repo-wide `go test ./...` is **not** required here — final repo-wide verification is in phase 4.

## Files

- `cmd/fabric_read.go` — delete
- `internal/tui/beadview.go` — delete
- `cmd/run.go` — strip beads import, --beads-* flags, beads.NewClient, beads arg passing
- `cmd/nebula_apply.go` — strip beads import, --beads-* flags, beads arg passing
- `cmd/validate.go` — strip beads import and any bead-reachability checks
- `cmd/nebula_adapters.go` — drop beads adapter helpers and beads import
- `cmd/nebula.go` — strip beads import and any bead subcommand registration
- `cmd/tui.go` — strip beads import, drop beadview route registration, drop bead subcommands
- `internal/tui/*.go` (only files that reference beadview) — minimal-surface deletion of the beadview reference; no other TUI changes
- `internal/ui/nebula.go` — strip beads import and bead-status rendering
- `internal/ui/ui_test.go` — strip bead-annotation assertions
- `internal/checkpoint/checkpoint.go` — strip beads import and beads snapshot field
- `internal/config/config.go` — drop beads config struct and Viper bindings
- `internal/config/config_test.go` — drop beads config tests

## Acceptance Criteria

- [ ] `cmd/fabric_read.go` no longer exists
- [ ] `internal/tui/beadview.go` no longer exists
- [ ] `grep -rn "beads\|bead_" cmd/ internal/ui/ internal/checkpoint/ internal/config/ 2>/dev/null` returns no matches
- [ ] `grep -rn "beads\|bead_" internal/tui/ 2>/dev/null` returns only references that will be removed by Nebula 4 (i.e., generic TUI state references unrelated to the deleted beadview file); the beadview-specific references are gone
- [ ] `go build ./...` exits 0 across the entire repo
- [ ] `go vet ./cmd/... ./internal/ui/... ./internal/tui/... ./internal/checkpoint/... ./internal/config/...` exits 0
- [ ] `go test ./cmd/... ./internal/ui/... ./internal/tui/... ./internal/checkpoint/... ./internal/config/...` exits 0
- [ ] The `Config` struct in `internal/config/config.go` no longer has any beads-related fields
- [ ] No `--beads-*` flags appear in `quasar --help` output for any subcommand
- [ ] The TUI launches without referencing the deleted beadview (a brief manual smoke test via `quasar tui` is acceptable verification; if `quasar tui` cannot be run in the agent's environment, code inspection plus build success suffices)
