+++
id = "cmd-table-driven-registration"
title = "Apply table-driven registration for nebula subcommands"
type = "task"
priority = 3
depends_on = ["cmd-extract-adapters"]
scope = ["cmd/nebula.go", "cmd/nebula_validate.go", "cmd/nebula_plan.go", "cmd/nebula_apply.go", "cmd/nebula_show.go", "cmd/nebula_status.go"]
+++

## Problem

After phase `split-cmd-nebula-file`, each nebula subcommand lives in its own file, but each file repeats the same boilerplate:

1. Declare a `*cobra.Command` package-level var
2. Write an `init()` that adds flags and calls `nebulaCmd.AddCommand`
3. Define a `RunE` function with flag parsing, validation, and execution

The flag-binding and `AddCommand` wiring is mechanical and repetitive. Adding a new subcommand requires copying an existing file and modifying it, which invites copy-paste bugs.

## Solution

Use a table-driven registration pattern to reduce per-command boilerplate:

1. In `cmd/nebula.go`, define a registration type and a table:
   ```go
   type nebulaSubcmd struct {
       use   string
       short string
       args  cobra.PositionalArgs
       flags func(cmd *cobra.Command) // registers command-specific flags
       run   func(cmd *cobra.Command, args []string) error
   }

   var nebulaSubcmds = []nebulaSubcmd{
       {
           use:   "validate <path>",
           short: "Validate a nebula specification",
           args:  cobra.ExactArgs(1),
           run:   runNebulaValidate,
       },
       {
           use:   "plan <path>",
           short: "Show execution plan for a nebula",
           args:  cobra.ExactArgs(1),
           flags: addNebulaPlanFlags,
           run:   runNebulaPlan,
       },
       // ...
   }
   ```

2. Register all subcommands in a single loop in `init()`:
   ```go
   func init() {
       for _, sc := range nebulaSubcmds {
           cmd := &cobra.Command{
               Use:   sc.use,
               Short: sc.short,
               Args:  sc.args,
               RunE:  sc.run,
           }
           if sc.flags != nil {
               sc.flags(cmd)
           }
           nebulaCmd.AddCommand(cmd)
       }
       rootCmd.AddCommand(nebulaCmd)
   }
   ```

3. Each subcommand file (`nebula_validate.go`, `nebula_plan.go`, etc.) exports only:
   - `runNebulaValidate(cmd, args) error` — the run function
   - `addNebulaValidateFlags(cmd)` — optional flag registration function

4. This eliminates per-file `init()` functions, package-level `*cobra.Command` vars, and repetitive `AddCommand` calls.

## Files

- `cmd/nebula.go` — define `nebulaSubcmd` type, registration table, single `init()` loop
- `cmd/nebula_validate.go` — simplify to `runNebulaValidate` + optional `addNebulaValidateFlags`
- `cmd/nebula_plan.go` — simplify to `runNebulaPlan` + optional flags func
- `cmd/nebula_apply.go` — simplify to `runNebulaApply` + optional flags func
- `cmd/nebula_show.go` — simplify to `runNebulaShow` + optional flags func
- `cmd/nebula_status.go` — simplify to `runNebulaStatus` + optional flags func

## Acceptance Criteria

- [ ] Single registration loop in `cmd/nebula.go` wires all subcommands
- [ ] No per-file `init()` functions for nebula subcommands
- [ ] No package-level `*cobra.Command` vars for subcommands
- [ ] `go build -o quasar .` succeeds
- [ ] All subcommands still work: `./quasar nebula --help` shows all five
- [ ] `go test ./...` passes
