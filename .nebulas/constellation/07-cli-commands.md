+++
id = "cli-commands"
title = "CLI commands: constellation apply, validate, plan, status"
type = "feature"
priority = 2
depends_on = ["state-persistence"]
scope = ["cmd/constellation.go"]
+++

## Problem

The constellation types, validation, scheduler, fabric, oracle, and state persistence all exist as library code but have no CLI entry point. Users need `quasar constellation` commands to validate, plan, execute, and monitor constellations from the command line.

## Solution

### Command hierarchy

```
quasar constellation validate <path>   — validate a constellation manifest
quasar constellation plan <path>       — show execution plan (wave order, dependencies)
quasar constellation apply <path>      — execute the constellation
quasar constellation status <path>     — show current execution status from persisted state
```

### Command registration

```go
// cmd/constellation.go

package cmd

import (
    "fmt"
    "os"

    "github.com/aaronsalm/quasar/internal/bus"
    "github.com/aaronsalm/quasar/internal/claude"
    "github.com/aaronsalm/quasar/internal/config"
    "github.com/aaronsalm/quasar/internal/constellation"
    "github.com/aaronsalm/quasar/internal/nebula"
    "github.com/spf13/cobra"
)

var constellationCmd = &cobra.Command{
    Use:   "constellation",
    Short: "Multi-nebula coordination commands",
}

var constellationValidateCmd = &cobra.Command{
    Use:   "validate <path>",
    Short: "Validate a constellation manifest",
    Args:  cobra.ExactArgs(1),
    RunE:  runConstellationValidate,
}

var constellationPlanCmd = &cobra.Command{
    Use:   "plan <path>",
    Short: "Show constellation execution plan",
    Args:  cobra.ExactArgs(1),
    RunE:  runConstellationPlan,
}

var constellationApplyCmd = &cobra.Command{
    Use:   "apply <path>",
    Short: "Execute a constellation",
    Args:  cobra.ExactArgs(1),
    RunE:  runConstellationApply,
}

var constellationStatusCmd = &cobra.Command{
    Use:   "status <path>",
    Short: "Show constellation execution status",
    Args:  cobra.ExactArgs(1),
    RunE:  runConstellationStatus,
}

func init() {
    rootCmd.AddCommand(constellationCmd)
    constellationCmd.AddCommand(constellationValidateCmd)
    constellationCmd.AddCommand(constellationPlanCmd)
    constellationCmd.AddCommand(constellationApplyCmd)
    constellationCmd.AddCommand(constellationStatusCmd)

    constellationApplyCmd.Flags().Bool("resume", false, "Resume from last checkpoint")
    constellationApplyCmd.Flags().Int("max-concurrent", 0, "Max concurrent nebulas (0 = use manifest)")
    constellationApplyCmd.Flags().Bool("web", false, "Launch web dashboard alongside execution")
    constellationApplyCmd.Flags().Int("web-port", 0, "Port for web dashboard (0 = auto-assign)")
}
```

### Validate

```go
func runConstellationValidate(cmd *cobra.Command, args []string) error {
    c, err := constellation.Load(args[0])
    if err != nil {
        return fmt.Errorf("load constellation: %w", err)
    }

    if err := constellation.Validate(c); err != nil {
        fmt.Fprintf(os.Stderr, "Validation errors:\n%v\n", err)
        return fmt.Errorf("constellation has validation errors")
    }

    fmt.Fprintf(os.Stderr, "Constellation %q is valid (%d nebulas)\n",
        c.Name, len(c.Nebulas))
    return nil
}
```

### Plan

```go
func runConstellationPlan(cmd *cobra.Command, args []string) error {
    c, err := constellation.Load(args[0])
    if err != nil {
        return err
    }
    if err := constellation.Validate(c); err != nil {
        return err
    }

    // Build DAG and compute waves.
    sched, err := constellation.NewScheduler(c, nil, nil) // No factory needed for plan.
    if err != nil {
        return err
    }

    fmt.Fprintf(os.Stderr, "Constellation: %s\n", c.Name)
    fmt.Fprintf(os.Stderr, "Description: %s\n\n", c.Description)
    fmt.Fprintf(os.Stderr, "Execution plan (%d waves, %d nebulas):\n\n", len(sched.Waves()), len(c.Nebulas))

    for i, wave := range sched.Waves() {
        fmt.Fprintf(os.Stderr, "  Wave %d:\n", i+1)
        for _, nodeID := range wave.NodeIDs() {
            ref := sched.FindRef(nodeID)
            deps := ""
            if len(ref.DependsOn) > 0 {
                deps = fmt.Sprintf(" (after: %s)", strings.Join(ref.DependsOn, ", "))
            }
            fmt.Fprintf(os.Stderr, "    - %s%s\n", ref.Name, deps)
        }
    }
    return nil
}
```

### Apply

```go
func runConstellationApply(cmd *cobra.Command, args []string) error {
    dir := args[0]
    cfg, err := config.Load()
    if err != nil {
        return err
    }

    c, err := constellation.Load(dir)
    if err != nil {
        return err
    }
    if err := constellation.Validate(c); err != nil {
        return err
    }

    // Load or create state.
    resume, _ := cmd.Flags().GetBool("resume")
    state, err := constellation.LoadState(dir, c.Name)
    if err != nil {
        return err
    }
    if !resume && state.ShouldResume() {
        fmt.Fprintf(os.Stderr, "Prior state found. Use --resume to continue or delete %s to start fresh.\n",
            constellation.StateFilePath(dir))
        return nil
    }

    // Create bus.
    b := bus.New()
    defer b.Close()

    // Engine factory creates a nebula.Engine for each NebulaRef.
    factory := func(ref constellation.NebulaRef, eventBus bus.Bus) (*nebula.Engine, error) {
        engineCfg := nebula.EngineConfig{
            NebulaDir: ref.Path,
            Auto:      true,
            ClaudePath: cfg.ClaudePath,
            BeadsPath:  cfg.BeadsPath,
            // ... resolve from constellation + config ...
        }
        invoker := claude.NewInvoker(cfg.ClaudePath)
        beadsClient := beads.NewCLI(cfg.BeadsPath)
        return nebula.NewEngine(engineCfg, eventBus, invoker, beadsClient), nil
    }

    // Create scheduler.
    maxConcurrent, _ := cmd.Flags().GetInt("max-concurrent")
    if maxConcurrent > 0 {
        c.Coordination.MaxConcurrentNebulas = maxConcurrent
    }
    sched, err := constellation.NewScheduler(c, factory, b)
    if err != nil {
        return err
    }

    // Mark completed nebulas from prior state.
    if resume {
        for _, name := range state.CompletedNebulas() {
            sched.MarkComplete(name)
        }
    }

    // Run constellation.
    outcomes, err := sched.Run(cmd.Context())

    // Persist final state.
    for _, o := range outcomes {
        state.RecordNebulaOutcome(o)
    }
    constellation.SaveState(dir, state)

    // Print results.
    printConstellationResults(outcomes)
    return err
}
```

### Status

```go
func runConstellationStatus(cmd *cobra.Command, args []string) error {
    state, err := constellation.LoadState(args[0], "")
    if err != nil {
        return err
    }

    fmt.Fprintf(os.Stderr, "Constellation: %s\n", state.ConstellationName)
    fmt.Fprintf(os.Stderr, "Started: %s\n", state.StartedAt.Format("2006-01-02 15:04"))
    fmt.Fprintf(os.Stderr, "Updated: %s\n", state.UpdatedAt.Format("2006-01-02 15:04"))
    fmt.Fprintf(os.Stderr, "Cost: $%.4f\n\n", state.TotalCostUSD)

    for name, ns := range state.Nebulas {
        icon := statusIcon(ns.Status)
        fmt.Fprintf(os.Stderr, "  %s %s — %s ($%.4f, %d attempts)\n",
            icon, name, ns.Status, ns.CostUSD, ns.Attempts)
        if ns.Error != "" {
            fmt.Fprintf(os.Stderr, "    Error: %s\n", ns.Error)
        }
    }

    if len(state.OracleDecisions) > 0 {
        fmt.Fprintf(os.Stderr, "\nOracle decisions:\n")
        for _, d := range state.OracleDecisions {
            fmt.Fprintf(os.Stderr, "  [%s] %s: %s — %s\n",
                d.Timestamp.Format("15:04"), d.NebulaName, d.Decision.Strategy, d.Decision.Reason)
        }
    }
    return nil
}
```

## Files

- `cmd/constellation.go` — `constellationCmd` with `validate`, `plan`, `apply`, `status` subcommands, flag registration, handler functions

## Acceptance Criteria

- [ ] `quasar constellation validate <path>` loads and validates a constellation manifest
- [ ] `quasar constellation plan <path>` displays wave-by-wave execution order
- [ ] `quasar constellation apply <path>` executes the constellation with the scheduler
- [ ] `quasar constellation apply --resume` skips already-completed nebulas
- [ ] `quasar constellation apply --max-concurrent=3` overrides concurrency limit
- [ ] `quasar constellation status <path>` displays per-nebula status, cost, and oracle decisions
- [ ] State is persisted after each nebula completes
- [ ] Validation errors are displayed with actionable messages
- [ ] `go build ./...` compiles
- [ ] `go vet ./...` passes
