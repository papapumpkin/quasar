+++
id = "agent-lifecycle"
title = "Implement agent registration and heartbeat tools"
depends_on = ["dolt-client", "mcp-skeleton"]
+++

Implement the agent lifecycle MCP tools for registration, heartbeat, and stale
agent cleanup.

## Tools

### `register`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Human-readable agent name (e.g., "coder-1") |
| `role` | string | yes | Agent role (e.g., "coder", "reviewer") |

Returns: `{ "agent_id": "<uuid>" }`

Delegates to `Store.RegisterAgent`. Generate a UUID v4 for the agent ID.

### `heartbeat`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent_id` | string | yes | The agent's ID |

Returns: `{ "ok": true }`

Delegates to `Store.Heartbeat`. Return an error if the agent ID doesn't exist.

## Stale agent cleanup

Add a background goroutine to the `Server` that runs `Store.CleanupStaleAgents`
every 30 seconds with a configurable timeout (default: 2 minutes). When an agent
is cleaned up:

1. All its file claims are released
2. A broadcast message is sent: "Agent <name> (role: <role>) went stale and was
   removed. Its file claims have been released."

This goroutine should respect the server's context and stop on shutdown.

## Implementation

In `internal/agentmail/tools_lifecycle.go`:
- Define input structs with JSON tags
- Register both tools with the MCP server, replacing stubs

In `internal/agentmail/server.go`:
- Add the cleanup goroutine to `Server.Start`
- Make the cleanup interval and stale timeout configurable via `ServerConfig`

## Tests

In `internal/agentmail/tools_lifecycle_test.go`:
- Register an agent, verify it gets a valid UUID
- Heartbeat updates the timestamp
- Heartbeat with invalid ID returns error
- Stale cleanup releases claims (use a short timeout for testing)
- Stale cleanup sends a broadcast message
