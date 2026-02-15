+++
id = "integration-tests"
title = "End-to-end integration tests for agentmail"
depends_on = ["mailbox-tools", "file-claim-tools", "change-tools", "agent-lifecycle"]
+++

Create integration tests that exercise the full agentmail flow: Dolt database,
MCP server, and multiple simulated agents coordinating.

## Test scenarios

### 1. Two-agent file coordination
1. Start Dolt and the agentmail MCP server
2. Register agent-A (coder) and agent-B (coder)
3. Agent-A claims `internal/auth/auth.go` and `internal/auth/middleware.go`
4. Agent-B attempts to claim `internal/auth/auth.go` — verify conflict
5. Agent-B claims `internal/api/handler.go` — verify success
6. Agent-A announces change to `internal/auth/auth.go`
7. Agent-B calls `get_changes` — sees agent-A's change
8. Agent-A releases files
9. Agent-B successfully claims `internal/auth/auth.go`

### 2. Message passing
1. Register agent-A and agent-B
2. Agent-A sends a broadcast message: "Refactoring auth package"
3. Agent-B reads messages — sees the broadcast
4. Agent-A sends a directed message to agent-B's channel
5. Agent-B reads messages filtered by channel — sees the directed message
6. Agent-B reads broadcast channel — does NOT see the directed message

### 3. Stale agent cleanup
1. Register agent-A, have it claim files
2. Do NOT send heartbeats
3. Wait for cleanup interval (use short timeout in test config)
4. Verify agent-A's claims are released
5. Verify a broadcast message announces the cleanup

### 4. Concurrent claim race
1. Register agent-A and agent-B
2. Both attempt to claim the same file concurrently (use goroutines)
3. Verify exactly one succeeds and one gets a conflict

## Implementation

Create `internal/agentmail/integration_test.go` with a build tag:
```go
//go:build integration
```

Use a test helper that:
- Starts a Dolt instance (or skips if not available)
- Initializes the schema
- Starts the MCP server on a random port
- Returns a cleanup function

## Running

```bash
go test -tags=integration ./internal/agentmail/...
```

Document this in the test file header and in the project Makefile/README.
