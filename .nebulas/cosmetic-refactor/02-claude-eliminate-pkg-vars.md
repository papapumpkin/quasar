+++
id = "claude-eliminate-pkg-vars"
title = "Replace package-level mutable vars with struct fields in claude package"
type = "task"
priority = 2
scope = ["internal/claude/"]
+++

## Problem

`internal/claude/claude.go` uses package-level mutable variables for test substitution:

```go
var execCommandContext = exec.CommandContext
var execCommand = exec.Command
```

This is a Go anti-pattern: package-level mutable state makes tests non-parallelizable and can leak between test cases. The `beads` package already uses a cleaner pattern with a `runner` field on the struct.

## Solution

Move the command-creation functions into the `Invoker` struct as fields, following the pattern already established by `beads.CLI.runner`:

1. Add fields to `Invoker`:
   ```go
   type Invoker struct {
       ClaudePath         string
       Verbose            bool
       execCommandContext func(ctx context.Context, name string, arg ...string) *exec.Cmd
       execCommand        func(name string, arg ...string) *exec.Cmd
   }
   ```
2. Initialize defaults in a constructor `NewInvoker(claudePath string, verbose bool) *Invoker` that sets the fields to `exec.CommandContext` and `exec.Command`.
3. Update `Invoke()` and `Validate()` to use `inv.execCommandContext` / `inv.execCommand` instead of the package vars.
4. Remove the package-level `var` declarations.
5. Update tests (if any in `claude/`) to set the struct fields instead of the package vars.
6. Update all call sites that construct `Invoker{}` to use `NewInvoker()`.

## Files

- `internal/claude/claude.go` — restructure `Invoker`, add constructor, remove package vars
- `internal/claude/claude_test.go` (if it exists) — update test setup
- Any files constructing `claude.Invoker{}` (check `cmd/run.go`, `cmd/nebula.go`, `internal/loop/loop.go`)

## Acceptance Criteria

- [ ] No package-level `var` for exec functions remains in `claude.go`
- [ ] `Invoker` uses struct fields with defaults set by a constructor
- [ ] All call sites updated to use the constructor
- [ ] `go test ./...` passes
- [ ] `go vet ./...` passes
