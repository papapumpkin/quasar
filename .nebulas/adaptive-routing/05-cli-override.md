+++
id = "cli-override"
title = "Add CLI flag and config key to control auto-routing"
type = "feature"
priority = 3
depends_on = ["resolve-integration"]
+++

## Problem

Operators need a way to enable, disable, or inspect auto-routing from the CLI without editing `nebula.toml`. This is critical for debugging (force a specific model), cost control (disable routing to expensive models), and CI environments (pin a known-good model).

## Solution

### New CLI Flag

Add a `--routing` flag to the `nebula apply` command in `cmd/nebula_apply.go` (or wherever the apply command is defined):

```go
cmd.Flags().String("routing", "", "Auto-routing mode: 'auto' (use nebula config), 'off' (disable), or a tier name to force (e.g. 'fast', 'balanced', 'heavy')")
```

Behavior:
- `--routing auto` (default when flag is empty): respect `[execution.routing]` in the nebula manifest.
- `--routing off`: disable auto-routing entirely, even if the manifest enables it. All phases use the normal cascade.
- `--routing fast` / `--routing balanced` / `--routing heavy`: force all phases to the named tier, bypassing scoring. Useful for cost-capping or debugging.

### Config Key

Add `routing` to the `Config` struct in `internal/config/config.go`:

```go
Config struct {
    // ... existing fields ...
    Routing string `mapstructure:"routing"` // "auto", "off", or a tier name
}
```

This enables `QUASAR_ROUTING=off` as an environment variable and `routing: "off"` in `.quasar.yaml`.

### Precedence

CLI flag > env var > `.quasar.yaml` > nebula manifest (standard Quasar config precedence).

### Integration

In the nebula apply flow (where `WorkerGroup` is constructed), read the routing config and adjust the `RoutingContext` accordingly:

```go
switch cfg.Routing {
case "", "auto":
    // Use nebula manifest's Routing config as-is.
case "off":
    // Set Routing.Enabled = false, overriding the manifest.
    rc.Routing.Enabled = false
default:
    // Treat the value as a tier name. Enable routing and force all phases
    // to the named tier by setting a single-tier config.
    rc.Routing.Enabled = true
    tier, ok := findTierByName(cfg.Routing, rc.Routing.Tiers)
    if !ok {
        tier, ok = findTierByName(cfg.Routing, DefaultTiers)
    }
    if !ok {
        return fmt.Errorf("unknown routing tier: %q", cfg.Routing)
    }
    // Force all phases to this tier by making it the only tier with MaxScore 1.0.
    rc.Routing.Tiers = []ModelTier{{Name: tier.Name, Model: tier.Model, MaxScore: 1.0}}
}
```

### Dry-Run Visibility

When `--verbose` is set, print the routing decision for each phase to stderr via `ui.Printer`:

```
Phase "setup-db": complexity=0.28, tier=fast, model=claude-haiku
Phase "refactor-auth": complexity=0.72, tier=heavy, model=claude-opus
```

This uses the existing `ui.Printer` stderr output convention.

## Files

- `cmd/nebula_apply.go` (or equivalent) -- add `--routing` flag
- `internal/config/config.go` -- add `Routing` field to `Config` struct
- `internal/nebula/tiers.go` -- add `findTierByName` helper
- `internal/nebula/worker_exec.go` -- add verbose routing output when `wg.Logger` is set
- `internal/nebula/tiers_test.go` -- test `findTierByName` with valid/invalid names

## Acceptance Criteria

- [ ] `--routing off` disables auto-routing even when the nebula manifest enables it
- [ ] `--routing fast` forces all phases to the "fast" tier regardless of complexity score
- [ ] `--routing auto` (or omitted) defers to the nebula manifest
- [ ] Unknown tier names produce a clear error message
- [ ] `QUASAR_ROUTING` environment variable works as expected
- [ ] Verbose mode prints the routing decision for each phase to stderr
- [ ] `go test ./internal/nebula/...` and `go test ./cmd/...` pass
