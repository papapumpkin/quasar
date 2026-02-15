+++
id = "mailbox-tools"
title = "Implement send_message and read_messages MCP tools"
depends_on = ["dolt-client", "mcp-skeleton"]
+++

Implement the mailbox MCP tools that allow agents to send and read messages.

## Tools

### `send_message`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent_id` | string | yes | Sender's agent ID |
| `channel` | string | no | Target channel (default: "broadcast") |
| `subject` | string | yes | Message subject |
| `body` | string | yes | Message body |

Returns: `{ "message_id": <int64> }`

Delegates to `Store.SendMessage`. Return an error if `agent_id` is not registered.

### `read_messages`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent_id` | string | yes | Requesting agent ID (for auth) |
| `since` | string | no | ISO 8601 timestamp — only return messages after this |
| `channel` | string | no | Filter to specific channel |

Returns: `{ "messages": [{ "id", "sender_id", "channel", "subject", "body", "created_at" }] }`

Delegates to `Store.ReadMessages`. Parse the `since` field as RFC 3339 if provided.

## Implementation

In `internal/agentmail/tools_mailbox.go`:
- Define input structs with JSON tags for each tool
- Register both tools with the MCP server, replacing the stubs from task 04
- Use `server.NewTool()` from the Go SDK with proper JSON schema descriptions

## Tests

In `internal/agentmail/tools_mailbox_test.go`:
- Send a message via the MCP tool, then read it back
- Test channel filtering (broadcast vs directed)
- Test `since` filtering returns only newer messages
- Test that sending from an unregistered agent returns an error
