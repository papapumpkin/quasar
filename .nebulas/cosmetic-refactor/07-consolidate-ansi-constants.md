+++
id = "consolidate-ansi-constants"
title = "Consolidate triplicated ANSI color constants into ui package"
type = "task"
priority = 2
depends_on = ["nebula-extract-phases-by-id"]
scope = ["internal/ui/ui.go", "internal/nebula/dashboard.go", "internal/nebula/checkpoint.go"]
+++

## Problem

ANSI escape code constants are defined three times across the codebase with inconsistent naming:

1. **`internal/ui/ui.go`**: `reset`, `bold`, `dim`, `blue`, `yellow`, `green`, `red`, `cyan`, `magenta`
2. **`internal/nebula/dashboard.go`**: `ansiReset`, `ansiBold`, `ansiDim`, `ansiGreen`, `ansiYellow`, `ansiRed`, `ansiCyan`, `ansiCursorUp`, `ansiClearLine`
3. **`internal/nebula/checkpoint.go`**: `cpReset`, `cpBold`, `cpDim`, `cpGreen`, `cpYellow`, `cpRed`, `cpCyan`, `cpMagenta`

Three copies of the same escape codes with three different naming conventions is a textbook DRY violation.

## Solution

Export the ANSI constants from the `ui` package and use them everywhere:

1. In `internal/ui/ui.go`, export the common constants (capitalize them): `Reset`, `Bold`, `Dim`, `Blue`, `Yellow`, `Green`, `Red`, `Cyan`, `Magenta`. Add `CursorUp` and `ClearLine` which are currently only in `dashboard.go`.
2. In `internal/nebula/dashboard.go`, remove the local `ansi*` constants and use `ui.Reset`, `ui.Bold`, etc. For `CursorUp` (which is a format string `"\033[%dA"`), export it as `ui.CursorUpFmt` or keep it local since it's specific to dashboard.
3. In `internal/nebula/checkpoint.go`, remove the local `cp*` constants and use `ui.*` constants.
4. Update all references within those files to use the new `ui.*` names.

Note: The `nebula` package will gain an import on `ui`, which is fine — `ui` is a leaf package with no dependencies on `nebula`.

## Files

- `internal/ui/ui.go` — export ANSI constants, add missing ones (`CursorUp`, `ClearLine`)
- `internal/nebula/dashboard.go` — remove local constants, import `ui`
- `internal/nebula/checkpoint.go` — remove local constants, import `ui`

## Acceptance Criteria

- [ ] ANSI constants are defined exactly once, in `internal/ui/`
- [ ] `dashboard.go` and `checkpoint.go` have no local ANSI constant definitions
- [ ] No import cycles introduced
- [ ] `go test ./...` passes
- [ ] `go vet ./...` passes
