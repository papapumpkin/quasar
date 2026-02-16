+++
id = "agentmail-metrics"
title = "Query agentmail Dolt tables for coordination metrics"
type = "feature"
priority = 2
depends_on = ["metrics-types"]
scope = ["internal/nebula/metrics_agentmail.go"]
+++

## Problem

Runtime coordination data (file claim conflicts, claim wait patterns, change announcements) lives in agentmail's Dolt database. The metrics system needs to query this data after each wave to feed the adaptive concurrency controller.

## Solution

Create `internal/nebula/metrics_agentmail.go` with functions that query agentmail's Dolt tables and populate the Metrics struct.

### Data sources

Agentmail's Dolt schema has four tables with relevant coordination data:

| Table | Metric signal |
|-------|--------------|
| `file_claims` | Active claims, contention (multiple agents wanting same file) |
| `changes` | Change count per wave, files modified per phase |
| `agents` | Active worker count, stale agent detection |
| `messages` | Conflict resolution messages between agents |

### Functions

```go
// CollectAgentmailMetrics queries the agentmail Dolt database and populates
// coordination metrics for the given wave. Requires a database/sql connection.
// Returns nil error if agentmail is not configured (metrics left empty).
func CollectAgentmailMetrics(ctx context.Context, db *sql.DB, m *Metrics, waveNumber int) error
```

Specific queries:
- **Conflict count**: `SELECT COUNT(*) FROM file_claims WHERE file_path IN (SELECT file_path FROM file_claims GROUP BY file_path HAVING COUNT(DISTINCT agent_id) > 1)`
- **Claim duration**: time between claim and release for completed phases (join `file_claims` with `changes` timestamps)
- **Change volume**: `SELECT COUNT(*) FROM changes WHERE announced_at > ?` (wave start time)

### Integration point

Called from `WorkerGroup.Run` after each wave completes, before the adaptive controller's `Decide()`. Only called if a Dolt DSN is configured.

### Optional dependency

This phase has a soft dependency on agentmail being deployed. When agentmail is not configured (no DSN), the function is a no-op. Metrics from the orchestrator side (phase duration, cycles, cost) still work independently.

## Files to Modify

- `internal/nebula/metrics_agentmail.go` — New file: Dolt query functions for coordination metrics

## Acceptance Criteria

- [ ] Queries compile and return correct data against agentmail schema
- [ ] No-op when db is nil (agentmail not configured)
- [ ] Conflict count populated from file_claims contention
- [ ] Change volume populated from changes table
- [ ] `go vet ./...` passes
- [ ] Test with mock sql.DB or sqltest
