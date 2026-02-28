+++
id = "wave-scanner"
title = "Layer-by-layer wave scanning with downstream pruning"
type = "feature"
priority = 1
depends_on = ["contract-poller"]
scope = ["internal/tycho/wave_scan.go"]
+++

## Problem

The current `Scan()` in `tycho.go` iterates a flat list of eligible phases and polls each one independently. There is no awareness of wave structure — a phase in wave 5 is polled even if its ancestor in wave 2 can't proceed. This wastes poll cycles (and with the LLMPoller, wastes LLM calls).

More importantly, without wave-awareness the dispatch loop can't reason about the frontier: "wave 2 is blocked, so don't bother checking waves 3-5." The deterministic contract poller makes this cheap, but the topology-aware pruning makes it correct — you never dispatch a phase whose upstream contracts are structurally impossible to fulfill in this cycle.

## Solution

### 1. New file: `internal/tycho/wave_scan.go`

Replace the flat scan with a wave-aware scan that walks the DAG layer by layer:

```go
// WaveScanner walks execution waves in topological order, polling
// each phase via the ContractPoller. When a phase in wave N cannot
// proceed, its entire downstream subtree is pruned from consideration.
type WaveScanner struct {
    Poller   fabric.Poller
    Blocked  *fabric.BlockedTracker
    Pushback *fabric.PushbackHandler
    Fabric   fabric.Fabric
    Logger   io.Writer
}

// ScanWaves evaluates phases wave-by-wave. Returns the set of phases
// that can proceed and the set that were pruned.
func (ws *WaveScanner) ScanWaves(
    ctx context.Context,
    waves []dag.Wave,
    eligible map[string]bool,
    snap fabric.FabricSnapshot,
) (proceed []string, pruned map[string]string)
```

### 2. Algorithm

```
pruned := {}       // phaseID -> reason
proceed := []

for wave in waves (ordered):
    for phase in wave.NodeIDs:
        if phase not in eligible:
            continue   // already done, failed, or in-flight
        if phase in pruned:
            continue   // ancestor couldn't proceed

        result := poller.Poll(ctx, phase, snap)

        if result == PROCEED:
            proceed = append(proceed, phase)
        else:
            // Block this phase
            handleBlock(phase, result, snap)
            // Prune all descendants
            for desc in dag.Descendants(phase):
                pruned[desc] = fmt.Sprintf("upstream %s blocked: %s", phase, result.Reason)
```

### 3. Key properties

- **O(eligible) polls**: Each eligible phase is polled at most once. Pruned phases are never polled.
- **Wave ordering**: Ensures producers are checked before consumers. If a producer can't proceed, its consumers are pruned before being polled.
- **Deterministic**: Same board state + same DAG = same dispatch decision, every time.
- **Composable**: The `WaveScanner` uses the `Poller` interface, so it works with both `ContractPoller` (deterministic) and `LLMPoller` (fallback). But the pruning is most valuable with the deterministic poller because it avoids wasting LLM calls.

### 4. Integration with Tycho Scheduler

Replace the existing flat `Scan()` body in `tycho.go` with a delegation to `WaveScanner` when wave data is available:

```go
func (s *Scheduler) Scan(ctx context.Context, eligible []string, sb SnapshotBuilder) ([]string, error) {
    if s.Poller == nil || s.Blocked == nil || sb == nil {
        return eligible, nil
    }

    snap, err := sb.BuildSnapshot(ctx)
    if err != nil {
        return eligible, nil
    }

    if s.WaveScanner != nil {
        return s.WaveScanner.ScanWaves(ctx, s.Waves, toSet(eligible), snap)
    }

    // ... existing flat scan as fallback
}
```

The `Scheduler` gains two new fields:

```go
type Scheduler struct {
    // ... existing fields
    WaveScanner *WaveScanner  // nil = flat scan (legacy)
    Waves       []dag.Wave    // pre-computed wave ordering
    DAG         *dag.DAG      // for Descendants() lookups during pruning
}
```

### 5. Re-evaluation with pruning

When `Reevaluate()` unblocks a phase, clear its downstream from the pruned set so they become eligible for the next scan cycle. The pruned set is per-scan-cycle (not persistent), so this happens naturally — each dispatch loop iteration starts with a fresh scan.

## Files

- `internal/tycho/wave_scan.go` — `WaveScanner`, `ScanWaves()`, pruning logic
- `internal/tycho/wave_scan_test.go` — Tests for wave walking, pruning, edge cases
- `internal/tycho/tycho.go` — Add `WaveScanner`, `Waves`, `DAG` fields to `Scheduler`; update `Scan()` to delegate

## Acceptance Criteria

- [ ] `ScanWaves` walks waves in topological order (wave 1 before wave 2, etc.)
- [ ] When a phase polls non-PROCEED, all its transitive descendants are pruned
- [ ] Pruned phases are never polled (zero unnecessary poll calls)
- [ ] Phases already done, failed, or in-flight are skipped (not polled)
- [ ] When no `WaveScanner` is set, existing flat `Scan()` behavior is preserved
- [ ] Integration with `BlockedTracker` and `PushbackHandler` works correctly for blocked phases
- [ ] Table-driven tests cover: all-proceed, first-wave-blocked (prunes everything), mid-graph block (partial prune), diamond dependency with one branch blocked
- [ ] `go vet ./...` passes
