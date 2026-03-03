+++
id = "cli-adapter"
title = "Reduce cmd/nebula_apply.go to a thin CLI adapter around Engine"
type = "task"
priority = 2
depends_on = ["engine-extract"]
scope = ["cmd/nebula_apply.go", "cmd/tui.go", "cmd/nebula_apply_test.go"]
allow_scope_overlap = true
+++

## Problem

With the `Engine` fully implemented (phase 7), `runNebulaApply` still contains the original ~567 lines of monolithic logic. It needs to be replaced with a thin adapter that:

1. Parses CLI flags and config into an `EngineConfig`.
2. Creates the bus and subscribes TUI/telemetry/stderr consumers.
3. Creates an `Engine` and calls `Engine.Run(ctx)`.
4. Handles the TUI/stderr display path branching.

Until this is done, there are two code paths — the old monolithic one and the new Engine. Both produce correct behavior, but the old one must be removed to complete the extraction and avoid maintenance burden.

Similarly, `cmd/tui.go` (`quasar cockpit`) should use the Engine for its `runSelectedNebula` function.

## Solution

### Rewrite `runNebulaApply`

Replace the body of `runNebulaApply` with:

```go
func runNebulaApply(cmd *cobra.Command, args []string) error {
    cfg, err := resolveEngineConfig(cmd, args)
    if err != nil {
        return err
    }

    // Create bus (nil if not in auto mode).
    var b bus.Bus
    if cfg.Auto {
        b = bus.New()
        defer b.Close()
    }

    // Create invoker and beads client.
    invoker := claude.NewInvoker(cfg.ClaudePath)
    beadsClient := beads.NewCLI(cfg.BeadsPath)

    // Create engine.
    engine := nebula.NewEngine(cfg, b, invoker, beadsClient)

    // Subscribe consumers to the bus.
    if b != nil {
        subscribeTelemetry(b, cfg)
        if cfg.UseTUI {
            return runWithTUI(cmd.Context(), engine, b, cfg)
        }
        subscribeStderr(b, cfg)
    }

    // Non-TUI path (or plan-only).
    result := engine.Run(cmd.Context())
    printResult(result, cfg)
    return result.Err
}
```

### resolveEngineConfig

Extract all flag/config resolution into a pure function that returns `EngineConfig`:

```go
// resolveEngineConfig reads CLI flags, Viper config, and manifest
// defaults to produce a fully-resolved EngineConfig. This is the
// only function that touches Cobra/Viper — everything downstream
// works with EngineConfig.
func resolveEngineConfig(cmd *cobra.Command, args []string) (nebula.EngineConfig, error) {
    cfg, err := config.Load()
    if err != nil {
        return nebula.EngineConfig{}, fmt.Errorf("load config: %w", err)
    }

    dir := args[0]
    auto, _ := cmd.Flags().GetBool("auto")
    noTUI, _ := cmd.Flags().GetBool("no-tui")
    noSplash, _ := cmd.Flags().GetBool("no-splash")
    resume, _ := cmd.Flags().GetBool("resume")
    verbose, _ := cmd.Flags().GetBool("verbose")
    maxWorkers, _ := cmd.Flags().GetInt("max-workers")
    // ... resolve remaining flags ...

    useTUI := auto && !noTUI && isStderrTTY()

    return nebula.EngineConfig{
        NebulaDir:       dir,
        WorkDir:         resolveWorkDir(cfg, dir),
        MaxWorkers:      maxWorkers,
        MaxReviewCycles: cfg.MaxReviewCycles,
        MaxBudgetUSD:    cfg.MaxBudgetUSD,
        Model:           cfg.Model,
        Verbose:         verbose,
        Auto:            auto,
        Resume:          resume,
        UseTUI:          useTUI,
        NoSplash:        noSplash,
        ClaudePath:      cfg.ClaudePath,
        BeadsPath:       cfg.BeadsPath,
        // ... remaining fields ...
    }, nil
}
```

### runWithTUI

The TUI path creates a `tea.Program`, subscribes it to the bus, and blocks on `tuiProgram.Run()`:

```go
func runWithTUI(ctx context.Context, engine *nebula.Engine, b bus.Bus, cfg nebula.EngineConfig) error {
    n, err := nebula.Load(cfg.NebulaDir)
    if err != nil {
        return err
    }
    phases := buildPhaseInfos(n)
    tuiProgram := tui.NewNebulaProgram(n.Manifest.Nebula.Name, phases, cfg.NebulaDir, cfg.NoSplash)

    // Subscribe TUI to bus events.
    sub := tui.NewBusSubscriber(tuiProgram.Program())
    b.Subscribe(sub)

    // Run engine in background goroutine.
    var result *nebula.EngineResult
    go func() {
        result = engine.Run(ctx)
        tuiProgram.Send(tui.MsgNebulaDone{
            Results: result.WorkerResults,
            Err:     result.Err,
        })
    }()

    // Block on TUI.
    if _, err := tuiProgram.Run(); err != nil {
        return err
    }
    if result != nil && result.Err != nil {
        return result.Err
    }
    return nil
}
```

### Update cmd/tui.go

`runSelectedNebula` in `cmd/tui.go` currently duplicates much of `runNebulaApply`. Replace it with:

```go
func runSelectedNebula(cfg *config.Config, printer *ui.Printer, dir string, noSplash bool, maxWorkers int, maxWorkersExplicit bool) nebulaResult {
    engineCfg := nebula.EngineConfig{
        NebulaDir: dir,
        Auto:      true,
        UseTUI:    true,
        NoSplash:  noSplash,
        // ... resolve from cfg ...
    }
    if maxWorkersExplicit {
        engineCfg.MaxWorkers = maxWorkers
    }

    b := bus.New()
    defer b.Close()

    invoker := claude.NewInvoker(cfg.ClaudePath)
    beadsClient := beads.NewCLI(cfg.BeadsPath)
    engine := nebula.NewEngine(engineCfg, b, invoker, beadsClient)

    // ... TUI setup and run (same pattern as runWithTUI) ...
}
```

### Verification

After this phase, `runNebulaApply` should be ~60-80 lines (down from ~567). The behavioral test is straightforward: run `quasar nebula apply .nebulas/<test-nebula> --auto` with both TUI and stderr paths and verify identical output/behavior to the pre-refactor version.

## Files

- `cmd/nebula_apply.go` — rewrite `runNebulaApply` as thin adapter: `resolveEngineConfig` + `Engine.Run` + consumer subscriptions
- `cmd/tui.go` — update `runSelectedNebula` to use `Engine`
- `cmd/nebula_apply_test.go` — add test for `resolveEngineConfig` producing correct `EngineConfig` from flags

## Acceptance Criteria

- [ ] `runNebulaApply` is <= 100 lines
- [ ] `resolveEngineConfig` is a pure function that returns `EngineConfig` with no side effects
- [ ] `resolveEngineConfig` has no dependency on `tea.Program`, `bus.Bus`, or `Engine`
- [ ] TUI path works identically: splash, phase table, live updates, gate prompts, completion overlay
- [ ] Stderr path works identically: plan output, progress bar, worker results
- [ ] Plan-only mode (no `--auto`) works: shows plan, exits
- [ ] `--resume` flag correctly enables checkpoint resume via `EngineConfig`
- [ ] `quasar cockpit` uses `Engine` for nebula execution
- [ ] All existing tests pass: `go test ./...`
- [ ] `go build ./...` and `go vet ./...` pass
- [ ] No duplicate logic between `runNebulaApply` and `runSelectedNebula`
