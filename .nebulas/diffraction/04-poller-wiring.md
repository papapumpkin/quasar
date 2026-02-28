+++
id = "poller-wiring"
title = "Wire ContractPoller and WaveScanner into nebula apply"
type = "feature"
priority = 2
depends_on = ["soft-dag"]
scope = ["cmd/nebula_apply.go", "cmd/tui.go"]
+++

## Problem

The observatory nebula's `fabric-activation` phase wires `LLMPoller` as the dispatch poller. With the `ContractPoller`, `WaveScanner`, and soft DAG now implemented, the apply path needs to use these instead.

The wiring in `cmd/nebula_apply.go` and `cmd/tui.go` needs to:
1. Run the static scanner to produce contracts
2. Create a `ContractPoller` from those contracts
3. Create a `WaveScanner` and pre-compute waves
4. Pass these to the `WorkerGroup` and `Tycho Scheduler`

## Solution

### 1. Run static scanner during apply

After loading and validating the nebula but before creating the `WorkerGroup`, run the static scanner to produce contracts:

```go
if n.Manifest.Execution.AgentMail {
    scanner := &fabric.StaticScanner{WorkDir: workDir}
    contracts, err := scanner.Scan(n.Phases)
    if err != nil {
        printer.Warning(fmt.Sprintf("static scan failed, falling back to LLM poller: %v", err))
        // fall back to LLMPoller
    }
}
```

### 2. Create ContractPoller

```go
contractMap := make(map[string]*fabric.PhaseContract, len(contracts))
for i := range contracts {
    contractMap[contracts[i].PhaseID] = &contracts[i]
}

poller := &fabric.ContractPoller{
    Contracts: contractMap,
    MatchMode: fabric.MatchName, // start loose, tighten if too many false proceeds
}
```

### 3. Pre-compute waves for WaveScanner

The waves are already computed by the DAG engine during `NewScheduler`. Expose them and pass to the Tycho scheduler:

```go
scheduler, err := NewScheduler(n.Phases)
// ... existing code ...
waves, _ := scheduler.Analyzer().DAG().ComputeWaves()
```

### 4. Wire into WorkerGroup

The `WorkerGroup.Run()` already creates the `tycho.Scheduler`. Add the wave scanner and wave data:

```go
wg.tychoScheduler = &tycho.Scheduler{
    // ... existing fields ...
    WaveScanner: &tycho.WaveScanner{
        Poller:   wg.Poller,  // ContractPoller
        Blocked:  wg.blockedTracker,
        Pushback: wg.pushbackHandler,
        Fabric:   wg.Fabric,
        Logger:   wg.logger(),
    },
    Waves: waves,
    DAG:   scheduler.Analyzer().DAG(),
}
```

This may require a new `Option` or passing the waves through the existing wiring. Evaluate whether `WithWaves(waves)` is cleaner than computing them inside `Run()`.

### 5. LLMPoller as fallback

If the static scanner fails (e.g., can't parse phase bodies, scope globs don't resolve), fall back to `LLMPoller` gracefully:

```go
var poller fabric.Poller
if contracts != nil && len(contracts) > 0 {
    poller = &fabric.ContractPoller{Contracts: contractMap, MatchMode: fabric.MatchName}
    printer.Info("Using deterministic contract dispatch")
} else {
    poller = fabric.NewLLMPoller(claudeInv, phaseSpecs)
    printer.Info("Falling back to LLM dispatch poller")
}
```

### 6. Log the dispatch mode

Print which poller is active so the user knows what's happening:

```
Scheduler: 2 tracks, 2 workers (max: 3)
Dispatch: deterministic contract poller (6 contracts loaded)
  Track 0: [spacetime-model, nebula-scanner, catalog-reports] (impact: 4.21)
  Track 1: [spacetime-lock] (impact: 0.89)
```

## Files

- `cmd/nebula_apply.go` — Add static scanner call, create ContractPoller, pass to WorkerGroup
- `cmd/tui.go` — Same wiring for cockpit path
- `internal/nebula/worker.go` — Wire `WaveScanner` and `Waves` into Tycho scheduler creation in `Run()`
- `internal/nebula/worker_options.go` — Add `WithWaves()` option if needed

## Acceptance Criteria

- [ ] When `agentmail = true`, static scanner runs and ContractPoller is created
- [ ] Dispatch log shows "deterministic contract poller" with contract count
- [ ] Wave scanner is wired into Tycho scheduler with pre-computed waves
- [ ] If static scanner fails, LLMPoller is used as fallback with a warning
- [ ] Both stderr and TUI paths use the same poller wiring
- [ ] Existing behavior with `agentmail = false` is completely unchanged
- [ ] `go build` and `go vet ./...` pass
- [ ] All existing tests pass
