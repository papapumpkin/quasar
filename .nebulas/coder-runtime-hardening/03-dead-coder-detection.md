+++
id = "dead-coder-detection"
title = "Multi-signal healthcheck for the claude subprocess — wall-clock cap, file-write activity, token rate, tool-call ratio, CPU. Terminate dead coders and hand partial work to the reviewer."
type = "task"
priority = 1
depends_on = ["context-budgeting"]
scope = [
    "internal/claude/healthcheck.go",
    "internal/claude/healthcheck_test.go",
    "internal/claude/claude.go",
    "internal/telemetry/health_events.go",
]
+++

## Problem

This is the most important phase in this nebula. Today a claude subprocess can run for 60–90 minutes without committing anything and then exit. The supervisor sees "claude returned non-zero" and marks the cycle failed — but the user has already paid for 60+ minutes of inference. Often a retry succeeds because the partial work persists on disk; the failed cycle was wasted compute that should have been caught and terminated at 25 minutes.

A wall-clock cap alone is insufficient. A coder doing legitimate work for 22 minutes shouldn't be killed at 25. A coder that's been idle (no file writes, no stdout) for 8 minutes should be killed even if it's only been alive 12 minutes total. The right detector is multi-signal.

## Solution

### The healthcheck

`internal/claude/healthcheck.go`:

```go
// Healthcheck monitors a claude subprocess via five signals and decides
// when to terminate. Runs in its own goroutine alongside the subprocess.
// On Dead, it sends SIGTERM, waits 5s, sends SIGKILL if still alive.
type Healthcheck struct {
    Cmd           *exec.Cmd
    Workdir       string         // where to watch for file changes
    Clock         func() time.Time

    Policy        HealthPolicy
    OnStateChange func(s HealthState, reason string)
}

type HealthPolicy struct {
    // WallClockCap is the absolute upper bound on subprocess lifetime.
    // Default: 25 * time.Minute.
    WallClockCap time.Duration

    // FileWriteIdleCap is the longest stretch without a write under Workdir
    // (excluding .git, node_modules) before we consider the coder stalled.
    // Default: 5 * time.Minute.
    FileWriteIdleCap time.Duration

    // TokenRateFloor is the minimum stream-token-rate (tokens/sec averaged
    // over the last TokenRateWindow) below which the coder is "stuck reasoning."
    // Default: 5 tokens/sec.
    TokenRateFloor    float64
    TokenRateWindow   time.Duration  // default 60s

    // ToolCallRatioCeiling is the max Read:Edit ratio over the last
    // ToolCallWindow. Above this, the coder is in "explore loop, no progress."
    // Default: 12.0 (12 Reads per Edit).
    ToolCallRatioCeiling float64
    ToolCallWindow       int            // last N tool calls; default 20

    // CPUIdleCap is the longest stretch the subprocess can be at < 1% CPU
    // before we declare it hung (sysc waiting on something that won't return).
    // Default: 90s.
    CPUIdleCap time.Duration
}

type HealthState int
const (
    Healthy HealthState = iota
    Degraded              // any one signal red
    Dead                  // any two signals red, OR wall-clock hit
)

func (h *Healthcheck) Run(ctx context.Context) error
```

### Signal sources

| Signal | Source | Implementation |
|---|---|---|
| Wall-clock | `Clock()` | Straightforward timer |
| File-write idle | `fsnotify` watching `Workdir` recursively (excl. `.git`) | Reset timer on every Write event |
| Token rate | Parse claude CLI's `--output-format json` streaming output for `usage.output_tokens_delta` per event | Exponential moving average over `TokenRateWindow` |
| Tool-call ratio | The Invoker already wraps tool calls (Phase 1's tool budget); reuse its ledger | Compute over last `ToolCallWindow` calls |
| CPU usage | `ps -o pcpu= -p <pid>` polled every 5s | Reset timer on each ≥1% reading |

The token-rate signal requires the claude CLI's JSON output to be streaming (not buffered). Today `--output-format json` returns a single document at the end; the CLI also supports `--output-format stream-json` which emits events. We switch to the streaming form when healthcheck is enabled.

### Decision logic

Per tick (every 5s):

```go
red := 0
if wallClockElapsed > policy.WallClockCap {
    return Dead, "wall-clock cap exceeded"
}
if timeSinceLastWrite > policy.FileWriteIdleCap { red++ }
if recentTokenRate < policy.TokenRateFloor   { red++ }
if recentReadEditRatio > policy.ToolCallRatioCeiling { red++ }
if timeSinceLastCPUActivity > policy.CPUIdleCap { red++ }

switch red {
case 0: return Healthy, ""
case 1: return Degraded, /* reason */
default: return Dead, /* combined reason */
}
```

A single red signal degrades; two or more kills. Wall-clock alone kills regardless. This means a coder that's making real file writes but reasoning slowly is *degraded* (operator-visible) but not killed. A coder that has both stalled writes AND no tokens flowing IS dead — clear signal it's never coming back.

### Termination and partial-work handoff

When state transitions to `Dead`:

1. Log the reason to telemetry (`internal/telemetry/health_events.go`)
2. Send SIGTERM to the subprocess
3. Wait up to 5s for graceful exit
4. Send SIGKILL if still alive
5. Capture the worktree state as `<state>.partial`
6. Return a typed error: `&DeadCoderError{Reason: ..., PartialWorkdir: ...}`

The supervisor (Phase 5 of constellation-runtime) catches `DeadCoderError`, marks the cycle as `terminated_health` (a new status, distinct from `failed`), and queues the reviewer to judge whether the partial work is shippable, needs another coder, or should be abandoned.

### Telemetry

`internal/telemetry/health_events.go`:

```jsonl
{"ts":"2026-06-06T13:01:00Z","invocation_id":"...","event":"degraded","signal":"token_rate","value":2.1}
{"ts":"2026-06-06T13:03:15Z","invocation_id":"...","event":"dead","signals":["token_rate","write_idle"],"elapsed":"12m18s","reason":"two signals red"}
{"ts":"2026-06-06T13:03:18Z","invocation_id":"...","event":"sigterm_sent"}
{"ts":"2026-06-06T13:03:19Z","invocation_id":"...","event":"exited","clean":true}
```

A `quasar coder report --since 24h` CLI walks the JSONL log and surfaces termination patterns: which phases trip degraded, which trip dead, average time-to-degraded, etc.

### Configuration

Defaults in `internal/claude/healthcheck.go`. Per-star override via the star's TOML:

```toml
# stars/coder.md frontmatter (excerpt)
[health]
wall_clock_cap = "25m"
file_write_idle_cap = "5m"
token_rate_floor = 5
cpu_idle_cap = "90s"
```

A `[health]` block in the nebula's `[execution]` overrides star defaults. Per-run override via CLI flag.

### Tests

- Unit tests for each signal in isolation with a fake clock + injected ps/fsnotify
- Integration test: a fixture script that idles for `FileWriteIdleCap + 1m` → asserted Dead with `signal=write_idle`
- Integration test: a fixture that writes constantly but reasons slowly (token rate below floor) for 90s → Degraded (one signal red), not Dead
- Integration test: wall-clock cap fires regardless of other signals
- Race test: concurrent state transitions; verify the `OnStateChange` callback isn't called from multiple goroutines

## Files

- `internal/claude/healthcheck.go` (new) — the Healthcheck struct + Run loop
- `internal/claude/healthcheck_test.go` (new)
- `internal/claude/signals.go` (new) — fsnotify watcher, token-rate parser, CPU poller
- `internal/claude/signals_test.go` (new)
- `internal/claude/claude.go` (modify) — wire Healthcheck into Invoke
- `internal/claude/dead_coder_error.go` (new) — typed error
- `internal/telemetry/health_events.go` (new) — JSONL writer
- `internal/telemetry/health_events_test.go` (new)
- `cmd/coder.go` (new) — `quasar coder report --since 24h` walker
- `internal/runtime/supervisor.go` (modify) — catch `DeadCoderError`, mark cycle `terminated_health`

## Acceptance Criteria

- [ ] `Healthcheck.Run(ctx)` blocks until the subprocess exits OR the state transitions to Dead
- [ ] Default WallClockCap is 25 minutes
- [ ] Wall-clock cap kills the subprocess regardless of other signal state
- [ ] File-write idle > FileWriteIdleCap with all other signals green → state = Degraded
- [ ] Two or more red signals (excluding wall-clock) → state = Dead
- [ ] Dead transition sends SIGTERM, waits 5s, sends SIGKILL if still alive
- [ ] `DeadCoderError` is returned with `PartialWorkdir` populated
- [ ] Supervisor catches `DeadCoderError` and marks the cycle `terminated_health`
- [ ] Every state transition is logged to `health_events.jsonl`
- [ ] `quasar coder report --since 24h` prints a histogram of termination causes
- [ ] Per-star override via `[health]` TOML block takes effect
- [ ] Fixture test: idle subprocess (no writes, no stdout) terminates in `FileWriteIdleCap + 1 tick`
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` exit 0
