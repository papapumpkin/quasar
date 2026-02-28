+++
id = "auto-enable-contract-board"
title = "Auto-enable contract board when DAG has dependencies instead of requiring agentmail toggle"
type = "task"
priority = 1
depends_on = []
labels = ["quasar", "nebula"]
scope = ["cmd/nebula_adapters.go", "internal/nebula/types.go"]
allow_scope_overlap = true
+++

## Problem

The fabric/contract board (which manages phase concurrency, entanglements, and scratchpad telemetry) is gated behind a manual `agentmail = true` toggle in the nebula TOML `[execution]` section. This is problematic:

1. **Stale terminology** — the field is called `agentmail` but the system is actually the "contract board." The name misleads users.
2. **Manual opt-in for automatic behavior** — if a nebula has inter-phase dependencies (`depends_on`), the contract board is *required* for correct concurrent scheduling. Requiring a separate TOML flag for something the engine can infer from the DAG structure is unnecessary complexity.
3. **Silent failures** — without `agentmail = true`, entanglements stay empty, scratchpad never populates (no telemetry file created), and the scanning toast (phase 02) would never fire. Users see empty tabs with no indication of what's wrong.
4. **Agent burden** — AI agents authoring nebulae must remember to set this flag. The DAG engine should "just work."

### Current Flow

```
nebula.toml: agentmail = true
    → initFabric() checks n.Manifest.Execution.AgentMail
    → if false: returns empty fabricComponents{} (no poller, no publisher, no fabric DB)
    → Tycho scheduler gets nil Poller/Blocked → Scan() no-ops → all phases run immediately
    → No telemetry file → TelemetryBridge never starts → scratchpad empty
    → No fabric → emitFabricEvents skips → entanglements empty
```

## Solution

Replace the `AgentMail` boolean check with automatic detection: if **any** phase has a non-empty `DependsOn`, initialize the contract board. Also remove the stale `AgentMail` and `AgentMailPort` fields from the config.

### Changes

1. **`internal/nebula/types.go`** — Add a `HasDependencies()` method to `Nebula` and deprecate the `AgentMail`/`AgentMailPort` fields:
   ```go
   // HasDependencies reports whether any phase in the nebula has explicit
   // dependency edges. When true, the contract board is required for correct
   // concurrent scheduling.
   func (n *Nebula) HasDependencies() bool {
       for _, p := range n.Phases {
           if len(p.DependsOn) > 0 {
               return true
           }
       }
       return false
   }
   ```
   Remove (or leave as ignored) the `AgentMail` and `AgentMailPort` fields from `Execution`. If removing would break existing TOML parsing for nebulae that still have the key, keep the fields but stop reading them — TOML decoders typically ignore unknown keys, so removal is safe if `DisallowUnknownFields` is not set.

2. **`cmd/nebula_adapters.go`** — Replace the `AgentMail` guard in `initFabric()`:
   ```go
   func initFabric(ctx context.Context, n *nebula.Nebula, dir, workDir string, inv agent.Invoker) (*fabricComponents, error) {
       if !n.HasDependencies() {
           return &fabricComponents{}, nil
       }
       // ... rest unchanged
   }
   ```

3. **`cmd/nebula_apply.go`** — Update the comment at the `initFabric` call site:
   ```go
   // Initialize fabric infrastructure when the DAG has inter-phase dependencies.
   fc, err := initFabric(ctx, n, dir, workDir, claudeInv)
   ```

### Telemetry Auto-Creation

The telemetry bridge (scratchpad) currently requires `.quasar/telemetry/current.jsonl` to already exist. Since `initFabric()` already creates `.quasar/` via `os.MkdirAll`, extend it to also ensure the telemetry directory and file exist so that `TelemetryBridge` can start tailing immediately:
```go
// Inside initFabric, after creating fabricDir:
telemetryDir := filepath.Join(dir, ".quasar", "telemetry")
if err := os.MkdirAll(telemetryDir, 0o755); err != nil {
    return nil, fmt.Errorf("creating telemetry directory: %w", err)
}
telemetryFile := filepath.Join(telemetryDir, "current.jsonl")
if _, err := os.Stat(telemetryFile); os.IsNotExist(err) {
    if f, err := os.Create(telemetryFile); err == nil {
        f.Close()
    }
}
```

## Files

- `internal/nebula/types.go` — Add `HasDependencies()` method to `Nebula`, remove `AgentMail`/`AgentMailPort` fields from `Execution`
- `cmd/nebula_adapters.go` — Replace `AgentMail` check with `n.HasDependencies()` in `initFabric()`, ensure telemetry dir+file exist
- `cmd/nebula_apply.go` — Update comment at `initFabric` call site

## Acceptance Criteria

- [ ] Nebulae with inter-phase dependencies (`depends_on`) automatically initialize the contract board without any TOML toggle
- [ ] Nebulae with no dependencies skip fabric initialization (same as before when `agentmail = false`)
- [ ] The `AgentMail` and `AgentMailPort` fields are removed from the `Execution` struct
- [ ] Existing nebula TOMLs that still contain `agentmail = true` do not cause parse errors
- [ ] Telemetry directory and file are auto-created when fabric initializes, so scratchpad works immediately
- [ ] Entanglements tab populates when phases have dependencies
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] All existing tests pass
