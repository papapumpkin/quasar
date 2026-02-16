+++
id = "resource-monitor"
title = "System resource consumption display in status bar"
type = "feature"
priority = 2
depends_on = []
+++

## Problem

Users may run multiple quasar instances simultaneously (multiple nebulae, or quasar + other heavy processes). There's no visibility into how much CPU/memory the current quasar process (and its child claude processes) are consuming, making it hard to know when you're overloading the machine.

## Current State

**Status bar** shows: logo, mode info, progress, cost, elapsed time. No system metrics.

**Process tree**: Each quasar spawns `claude -p` subprocesses via `exec.CommandContext`. With `max_workers=2` and each worker running a coder+reviewer, you could have 2+ claude processes at once, each consuming significant memory and CPU.

**No existing resource tracking** — quasar tracks cost (USD) but not compute resource usage.

## Solution

### 1. Resource Sampler

Create `internal/tui/resources.go` with a lightweight sampler that periodically collects:

```go
type ResourceSnapshot struct {
    CPUPercent    float64 // total CPU% of quasar + children
    MemoryMB      float64 // total RSS of quasar + children
    NumProcesses  int     // count of child processes
    LoadAvg1m     float64 // system 1-minute load average
}

func SampleResources(pid int) ResourceSnapshot
```

Implementation:
- Use `os.Getpid()` to get the quasar PID
- On macOS: `ps -o pid,rss,%cpu -g <pgid>` to get process group stats
- On Linux: read `/proc/<pid>/stat` and `/proc/<pid>/statm` for the process tree
- For load average: `syscall.Sysinfo` (Linux) or `sysctl` (macOS)
- Keep it simple — no cgo, just exec `ps` and parse output

### 2. Periodic Sampling

Send `MsgResourceUpdate` on a timer (every 5 seconds is sufficient — resource stats don't need to be real-time):

```go
type MsgResourceUpdate struct {
    Snapshot ResourceSnapshot
}

func resourceTickCmd() tea.Cmd {
    return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
        return MsgResourceUpdate{Snapshot: SampleResources(os.Getpid())}
    })
}
```

### 3. Status Bar Integration

Add a compact resource indicator to the right side of the status bar, between cost and elapsed:

```
 ✦ QUASAR  nebula: auth  4/10 done    $1.24  ◈2  48MB  3.2%  5m36s
                                              ^^^  ^^^^  ^^^^
                                              procs mem   cpu
```

Compact format: `◈N` for process count, `XXXMB` for memory, `X.X%` for CPU.

Color coding:
- **Green** (normal): CPU < 50%, memory < 1GB
- **Yellow** (warning): CPU 50-80%, memory 1-2GB
- **Red** (danger): CPU > 80%, memory > 2GB, or load average > core count

### 4. Multi-Quasar Detection

Optionally detect other running quasar processes:
- `pgrep -c quasar` to count instances
- If > 1, show a small indicator: `⚡2 quasars` in the status bar
- This helps users realize they have multiple nebulae running

### 5. Thresholds in Config

Add optional thresholds to `.quasar.yaml`:
```yaml
resource_warnings:
  cpu_warning_percent: 50
  cpu_danger_percent: 80
  memory_warning_mb: 1024
  memory_danger_mb: 2048
```

Defaults are reasonable for most machines. The TUI uses these to color-code the resource display.

## Files to Create

- `internal/tui/resources.go` — `ResourceSnapshot`, `SampleResources()`, platform-specific implementations
- `internal/tui/resources_test.go` — Tests (mock `ps` output parsing)

## Files to Modify

- `internal/tui/msg.go` — Add `MsgResourceUpdate`
- `internal/tui/model.go` — Handle `MsgResourceUpdate`; store latest snapshot; schedule sampling tick
- `internal/tui/statusbar.go` — Render compact resource indicator
- `internal/tui/styles.go` — Add `styleResourceNormal`, `styleResourceWarning`, `styleResourceDanger`

## Acceptance Criteria

- [ ] Status bar shows process count, memory, and CPU usage
- [ ] Updates every 5 seconds without noticeable performance impact
- [ ] Color coding: green/yellow/red based on thresholds
- [ ] Works on macOS (primary) and Linux
- [ ] Multiple quasar instances detected and indicated
- [ ] Graceful degradation if `ps` fails (show "?" instead of crashing)
- [ ] `go build` and `go test ./internal/tui/...` pass
