+++
id = "change-tools"
title = "Implement change announcement MCP tools"
depends_on = ["dolt-client", "mcp-skeleton"]
+++

Implement the change announcement MCP tools that let agents declare modifications
and query what other agents have changed.

## Tools

### `announce_change`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent_id` | string | yes | Agent announcing the change |
| `file_path` | string | yes | Path of the modified file |
| `summary` | string | yes | Human-readable description of the change |

Returns: `{ "change_id": <int64> }`

Delegates to `Store.AnnounceChange`. Return an error if `agent_id` is not
registered.

### `get_changes`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `since` | string | no | ISO 8601 timestamp — only changes after this |
| `agent_id` | string | no | Filter to changes by a specific agent |

Returns: `{ "changes": [{ "id", "agent_id", "file_path", "summary", "announced_at" }] }`

Delegates to `Store.GetChangesSince`. Parse `since` as RFC 3339 if provided.

## Implementation

In `internal/agentmail/tools_changes.go`:
- Define input structs with JSON tags
- Register both tools with the MCP server, replacing stubs
- Use `server.NewTool()` with descriptive JSON schema

## Tests

In `internal/agentmail/tools_changes_test.go`:
- Announce a change, then retrieve it via `get_changes`
- Test `since` filtering
- Test `agent_id` filtering
- Verify that announcing from an unregistered agent returns an error
