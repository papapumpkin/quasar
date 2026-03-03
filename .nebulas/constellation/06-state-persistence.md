+++
id = "state-persistence"
title = "Constellation state persistence for partial completion and resume"
type = "feature"
priority = 2
depends_on = ["dag-scheduler", "oracle-agent"]
scope = ["internal/constellation/state.go", "internal/constellation/state_test.go"]
+++

## Problem

Constellation execution can span hours or days (multiple nebulas, each with multiple phases). If execution is interrupted — machine restart, Ctrl+C, resource limits — all progress tracking is lost. Without persistence, the only option is to re-run the entire constellation from scratch, wasting time and budget on already-completed nebulas.

The existing nebula state system (`internal/nebula/state.go`) persists per-nebula state as TOML. The constellation needs analogous persistence at the constellation level: which nebulas completed, which failed, oracle decisions made, total cost, and the current position in the DAG.

## Solution

### ConstellationState

```go
// internal/constellation/state.go

package constellation

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "time"
)

// ConstellationState tracks the execution progress of a constellation.
// It is persisted to disk after each nebula completes or fails, enabling
// resume from any point.
type ConstellationState struct {
    // Version is the state format version for forward compatibility.
    Version string `json:"version"`

    // ConstellationName identifies which constellation this state belongs to.
    ConstellationName string `json:"constellation_name"`

    // StartedAt is when constellation execution began.
    StartedAt time.Time `json:"started_at"`

    // UpdatedAt is when this state was last persisted.
    UpdatedAt time.Time `json:"updated_at"`

    // TotalCostUSD is the cumulative cost across all nebula executions.
    TotalCostUSD float64 `json:"total_cost_usd"`

    // Nebulas maps nebula name to its execution state.
    Nebulas map[string]*NebulaState `json:"nebulas"`

    // OracleDecisions records all oracle decisions for audit trail.
    OracleDecisions []OracleDecisionRecord `json:"oracle_decisions,omitempty"`

    // CurrentWave is the wave index being executed (for resume).
    CurrentWave int `json:"current_wave"`
}

// NebulaState tracks a single nebula's execution within the constellation.
type NebulaState struct {
    Status     NebulaStatus `json:"status"`
    StartedAt  *time.Time   `json:"started_at,omitempty"`
    FinishedAt *time.Time   `json:"finished_at,omitempty"`
    CostUSD    float64      `json:"cost_usd"`
    Attempts   int          `json:"attempts"`
    Error      string       `json:"error,omitempty"`
}

// OracleDecisionRecord is a timestamped oracle decision for the audit log.
type OracleDecisionRecord struct {
    Timestamp  time.Time      `json:"timestamp"`
    NebulaName string         `json:"nebula_name"`
    Decision   OracleDecision `json:"decision"`
}
```

### State file path

State is persisted alongside the constellation manifest:

```go
// StateFilePath returns the path to the constellation state file.
// Convention: <constellation-dir>/constellation.state.json
func StateFilePath(dir string) string {
    return filepath.Join(dir, "constellation.state.json")
}
```

### Load

```go
// LoadState reads constellation state from disk. Returns a fresh
// state if the file does not exist.
func LoadState(dir string, name string) (*ConstellationState, error) {
    path := StateFilePath(dir)
    data, err := os.ReadFile(path)
    if os.IsNotExist(err) {
        return NewState(name), nil
    }
    if err != nil {
        return nil, fmt.Errorf("read constellation state: %w", err)
    }
    var state ConstellationState
    if err := json.Unmarshal(data, &state); err != nil {
        return nil, fmt.Errorf("unmarshal constellation state: %w", err)
    }
    return &state, nil
}

// NewState creates a fresh constellation state.
func NewState(name string) *ConstellationState {
    return &ConstellationState{
        Version:           "1",
        ConstellationName: name,
        StartedAt:         time.Now(),
        Nebulas:           make(map[string]*NebulaState),
    }
}
```

### Save

```go
// SaveState persists constellation state to disk. Called after each
// nebula completes or fails.
func SaveState(dir string, state *ConstellationState) error {
    state.UpdatedAt = time.Now()
    data, err := json.MarshalIndent(state, "", "  ")
    if err != nil {
        return fmt.Errorf("marshal constellation state: %w", err)
    }
    path := StateFilePath(dir)
    if err := os.WriteFile(path, data, 0o644); err != nil {
        return fmt.Errorf("write constellation state: %w", err)
    }
    return nil
}
```

### State updates from scheduler

```go
// RecordNebulaStart marks a nebula as running in the state.
func (s *ConstellationState) RecordNebulaStart(name string) {
    now := time.Now()
    ns, ok := s.Nebulas[name]
    if !ok {
        ns = &NebulaState{}
        s.Nebulas[name] = ns
    }
    ns.Status = NebulaStatusRunning
    ns.StartedAt = &now
    ns.Attempts++
}

// RecordNebulaOutcome records the result of a nebula execution.
func (s *ConstellationState) RecordNebulaOutcome(outcome NebulaOutcome) {
    now := time.Now()
    ns := s.Nebulas[outcome.Name]
    if ns == nil {
        ns = &NebulaState{}
        s.Nebulas[outcome.Name] = ns
    }
    ns.Status = outcome.Status
    ns.FinishedAt = &now
    if outcome.Result != nil {
        ns.CostUSD += outcome.Result.TotalCostUSD()
        s.TotalCostUSD += outcome.Result.TotalCostUSD()
        if outcome.Result.Err != nil {
            ns.Error = outcome.Result.Err.Error()
        }
    }
}

// RecordOracleDecision appends an oracle decision to the audit log.
func (s *ConstellationState) RecordOracleDecision(decision OracleDecision) {
    s.OracleDecisions = append(s.OracleDecisions, OracleDecisionRecord{
        Timestamp:  time.Now(),
        NebulaName: decision.NebulaName,
        Decision:   decision,
    })
}
```

### Resume logic

When the scheduler starts with an existing state, it skips completed nebulas:

```go
// CompletedNebulas returns the names of nebulas that have already
// completed successfully. Used by the scheduler to skip them on resume.
func (s *ConstellationState) CompletedNebulas() []string {
    var completed []string
    for name, ns := range s.Nebulas {
        if ns.Status == NebulaStatusDone {
            completed = append(completed, name)
        }
    }
    return completed
}

// ShouldResume returns true if there is prior state with completed nebulas.
func (s *ConstellationState) ShouldResume() bool {
    return len(s.CompletedNebulas()) > 0
}
```

## Files

- `internal/constellation/state.go` — `ConstellationState`, `NebulaState`, `OracleDecisionRecord`, `LoadState`, `SaveState`, `NewState`, state update methods, resume helpers
- `internal/constellation/state_test.go` — tests for: save/load round-trip, fresh state for nonexistent file, record nebula start/outcome, completed nebulas filtering, oracle decision audit trail, cost accumulation, JSON is human-readable

## Acceptance Criteria

- [ ] State is persisted to `constellation.state.json` after each nebula completion
- [ ] `LoadState` returns a fresh state when no file exists
- [ ] `LoadState` correctly deserializes existing state files
- [ ] `RecordNebulaStart` increments attempt count and sets status to running
- [ ] `RecordNebulaOutcome` records cost, error, and completion timestamp
- [ ] `CompletedNebulas()` returns only done nebulas (not failed/skipped)
- [ ] `ShouldResume()` detects prior completed work
- [ ] Oracle decisions are recorded in the audit log with timestamps
- [ ] Total cost aggregates across all nebula executions
- [ ] JSON output is indented and human-readable
- [ ] `go test ./internal/constellation/...` passes
- [ ] `go vet ./internal/constellation/...` passes
