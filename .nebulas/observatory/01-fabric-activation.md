+++
id = "fabric-activation"
title = "Wire fabric into nebula apply"
type = "feature"
priority = 1
depends_on = []
scope = ["cmd/nebula_apply.go", "cmd/tui.go"]
+++

## Problem

The entanglement fabric infrastructure is fully implemented across multiple packages — `SQLiteFabric` (SQLite WAL store), `LLMPoller` (readiness evaluation), `Publisher` (post-phase symbol extraction), `BlockedTracker`, `PushbackHandler` — and the `WorkerGroup` has `WithFabric()`, `WithPoller()`, and `WithPublisher()` option functions. The Tycho scheduler already checks for nil and becomes a no-op when fabric is absent.

But `cmd/nebula_apply.go` never passes these options. The `NewWorkerGroup` call on line ~156 includes `WithMaxWorkers`, `WithBeadsClient`, `WithGlobalCycles`, `WithGlobalBudget`, `WithGlobalModel`, and `WithCommitter` — but no fabric options. So the entire coordination layer sits dormant.

This means phases execute with DAG ordering only. There is no runtime contract verification, no entanglement publishing, no blocked-phase re-evaluation, and no escalation when a consumer can't find what it needs from the fabric.

## Solution

### 1. Gate activation on manifest field

The `[execution]` section of `nebula.toml` already has an `agentmail` boolean field (parsed but unused in the apply path). Use this as the activation switch:

```go
if n.Manifest.Execution.AgentMail {
    // initialize fabric
}
```

When `agentmail = false` (default), behavior is unchanged. Legacy nebulas work exactly as before.

### 2. Initialize fabric in nebula_apply.go

When `agentmail = true`, before `NewWorkerGroup`:

```go
fabricDir := filepath.Join(dir, ".quasar")
os.MkdirAll(fabricDir, 0o755)
fabricPath := filepath.Join(fabricDir, "fabric.db")

fab, err := fabric.NewSQLiteFabric(ctx, fabricPath)
if err != nil {
    return fmt.Errorf("creating fabric: %w", err)
}
defer fab.Close()
```

### 3. Create LLMPoller from phase specs

Build the `LLMPoller` using the existing `claudeInv` invoker (already created by line ~141 in `nebula_apply.go`):

```go
poller := fabric.NewLLMPoller(claudeInv, n.Phases)
```

Check `internal/fabric/llmpoller.go` for the constructor signature and adapt as needed. The poller needs the phase specs to understand what each phase expects.

### 4. Create Publisher

```go
pub := &fabric.Publisher{
    Fabric:  fab,
    WorkDir: workDir,
    Logger:  os.Stderr,
}
```

### 5. Pass options to NewWorkerGroup

Add three options to the existing `NewWorkerGroup` call:

```go
nebula.WithFabric(fab),
nebula.WithPoller(poller),
nebula.WithPublisher(pub),
```

### 6. Mirror in cmd/tui.go

The `cmd/tui.go` cockpit entry point also calls `NewWorkerGroup` (line ~249). Apply the same conditional fabric initialization there so the cockpit path also gets live entanglement tracking.

### 7. Seed initial phase states

After fabric is created, seed all phases as `StateQueued` so the fabric board view has entries from the start:

```go
for _, p := range n.Phases {
    fab.SetPhaseState(ctx, p.ID, fabric.StateQueued)
}
```

## Files

- `cmd/nebula_apply.go` — Add conditional fabric initialization block before `NewWorkerGroup`, pass `WithFabric`/`WithPoller`/`WithPublisher` options
- `cmd/tui.go` — Same fabric initialization for the cockpit path

## Acceptance Criteria

- [ ] When `agentmail = true` in `nebula.toml`, a `.quasar/fabric.db` file is created during apply
- [ ] Phases transition through fabric states: queued -> scanning -> running -> done/failed
- [ ] After a phase completes, `Publisher.PublishPhase` is called and entanglements appear in the SQLite DB
- [ ] File claims are recorded per phase and released on completion
- [ ] When `agentmail = false` (default), no fabric is created — identical to current behavior
- [ ] Both the stderr and TUI paths initialize fabric correctly
- [ ] `go build` and `go vet ./...` pass
- [ ] All existing tests pass
