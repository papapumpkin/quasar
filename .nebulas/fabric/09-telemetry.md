+++
id = "telemetry"
title = "JSONL telemetry event stream"
type = "feature"
priority = 2
depends_on = ["fabric-rename"]
scope = ["internal/telemetry/**", "cmd/telemetry.go"]
+++

## Problem

There is no structured record of what happened during a nebula execution. The TUI shows live state, but once the session ends, the only artifacts are the code changes and fabric state. The design calls for a JSONL event stream that records every state transition, making runs auditable, replayable, and analyzable.

## Solution

Create `internal/telemetry` with an `Emitter` that writes structured JSON events to a JSONL file. Every state transition, agent invocation, discovery, and completion posts an event.

### Event types

```go
type Event struct {
    Timestamp time.Time `json:"ts"`
    Kind      string    `json:"kind"`
    EpochID   string    `json:"epoch,omitempty"`
    TaskID    string    `json:"task,omitempty"`
    Data      any       `json:"data,omitempty"`
}
```

Event kinds:
- `epoch_start` — nebula execution begins
- `epoch_done` — nebula execution completes
- `task_state` — task state transition (queued→scanning→running→etc.)
- `agent_start` — agent (coder/reviewer) invoked
- `agent_done` — agent finished (includes cost, duration, token count)
- `entanglement_posted` — new entanglement published to fabric
- `claim_acquired` — file claim acquired
- `claim_released` — file claim released
- `discovery_posted` — discovery posted to fabric
- `discovery_resolved` — discovery resolved
- `pulse_emitted` — quasar emitted a pulse (shared execution context)
- `filter_result` — pre-reviewer filter outcome
- `cycle_start` — coder-reviewer cycle begins
- `cycle_done` — cycle completes (approved or issues found)

### Emitter

```go
// Emitter writes telemetry events to a JSONL file.
type Emitter struct {
    file *os.File
    enc  *json.Encoder
    mu   sync.Mutex
}

// NewEmitter creates a new emitter writing to the given path.
// The file is created or appended to.
func NewEmitter(path string) (*Emitter, error)

// Emit writes a single event. Thread-safe.
func (e *Emitter) Emit(evt Event) error

// Close flushes and closes the file.
func (e *Emitter) Close() error
```

The default telemetry path is `.quasar/telemetry/<epoch-id>.jsonl`.

### Integration points

The emitter is injected into:
- `WorkerGroup` — emits epoch_start/done, task_state transitions
- `Loop` — emits agent_start/done, cycle_start/done, filter_result
- Fabric operations — emits entanglement_posted, claim_acquired/released, discovery_posted/resolved

Each integration adds 1-2 `emitter.Emit()` calls at the appropriate points. The emitter is optional (nil = no telemetry, backward compatible).

### CLI subcommand

**`quasar telemetry [--epoch <id>]`**
- Tails the JSONL file for the current or specified epoch
- Renders events as formatted text to stdout
- Without `--epoch`: discovers the most recent telemetry file
- With `--follow` (alias `-f`): watches the file for new events (like `tail -f`)

## Files

- `internal/telemetry/telemetry.go` — `Event` type, `Emitter` struct
- `internal/telemetry/telemetry_test.go` — Tests for event serialization and file writing
- `cmd/telemetry.go` — `quasar telemetry` command

## Acceptance Criteria

- [ ] `Emitter` writes valid JSONL (one JSON object per line)
- [ ] `Emit` is thread-safe (multiple goroutines can call concurrently)
- [ ] All event kinds are documented as constants
- [ ] `quasar telemetry` reads and formats JSONL events
- [ ] `--follow` mode tails the file for new events
- [ ] Emitter is optional — nil emitter is a no-op throughout the codebase
- [ ] `go test ./internal/telemetry/...` passes
- [ ] `go vet ./...` clean
