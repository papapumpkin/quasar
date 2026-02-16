# AgentMail MCP Tool Reference

AgentMail is a shared mailbox MCP server for inter-agent coordination. It provides 9 tools organized into four categories: lifecycle, messaging, file claims, and change announcements.

## Architecture

```
┌─────────┐     ┌─────────┐     ┌─────────┐
│ Coder-1 │     │ Coder-2 │     │Reviewer-1│
└────┬────┘     └────┬────┘     └────┬─────┘
     │               │              │
     │          SSE / HTTP           │
     └───────────┬───┴──────────────┘
           ┌─────▼─────┐
           │ AgentMail  │
           │ MCP Server │
           │ :8391      │
           └─────┬─────┘
           ┌─────▼─────┐
           │   Dolt DB  │
           │ :3306      │
           └────────────┘
```

Each agent connects to the same AgentMail server over SSE/HTTP. The server persists all coordination data in a Dolt database, providing versioned history and audit trails.

---

## Lifecycle Tools

### `register`

Register a new agent with the coordination server. Must be called before using any other tool.

**Input Schema**

| Field  | Type     | Required | Description                                  |
|--------|----------|----------|----------------------------------------------|
| `name` | `string` | Yes      | Human-readable agent name (e.g. `coder-1`)   |
| `role` | `string` | Yes      | Agent role (e.g. `coder` or `reviewer`)       |

**Output Schema**

| Field      | Type     | Description                          |
|------------|----------|--------------------------------------|
| `agent_id` | `string` | Unique identifier for the registered agent |

**Example Request**

```json
{
  "name": "coder-1",
  "role": "coder"
}
```

**Example Response**

```json
{
  "agent_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
}
```

**Error Conditions**

- `name is required` — empty name field
- `role is required` — empty role field

**Usage Tips**

- Call `register` once at the start of an agent's lifecycle. The returned `agent_id` is required by all other tools.
- The agent ID is a UUID generated server-side — agents do not choose their own ID.

---

### `heartbeat`

Send a heartbeat to indicate agent liveness. Agents that stop sending heartbeats are automatically cleaned up after 2 minutes — their file claims are released and a broadcast message is sent.

**Input Schema**

| Field      | Type     | Required | Description          |
|------------|----------|----------|----------------------|
| `agent_id` | `string` | Yes      | The agent's ID       |

**Output Schema**

| Field | Type   | Description                          |
|-------|--------|--------------------------------------|
| `ok`  | `bool` | `true` if the heartbeat was recorded |

**Example Request**

```json
{
  "agent_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
}
```

**Example Response**

```json
{
  "ok": true
}
```

**Error Conditions**

- `agent_id is required` — empty agent_id field
- `heartbeat: ...` — agent not found or database error

**Usage Tips**

- Send heartbeats at least every 60 seconds to avoid being marked stale (stale timeout is 2 minutes by default).
- The cleanup loop runs every 30 seconds. When a stale agent is detected, its file claims are released and a broadcast message is sent to notify other agents.

---

## Messaging Tools

### `send_message`

Send a message to other agents via a named channel.

**Input Schema**

| Field     | Type     | Required | Description                                            |
|-----------|----------|----------|--------------------------------------------------------|
| `agent_id`| `string` | Yes      | Sender's agent ID                                      |
| `channel` | `string` | No       | Target channel (defaults to `broadcast` if omitted)    |
| `subject` | `string` | Yes      | Message subject                                        |
| `body`    | `string` | Yes      | Message body                                           |

**Output Schema**

| Field        | Type    | Description                  |
|--------------|---------|------------------------------|
| `message_id` | `int64` | Unique ID of the sent message |

**Example Request**

```json
{
  "agent_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "channel": "coordination",
  "subject": "Starting work on auth module",
  "body": "I'm implementing JWT validation in internal/auth/. Will claim files shortly."
}
```

**Example Response**

```json
{
  "message_id": 42
}
```

**Error Conditions**

- `agent_id is required` — empty agent_id
- `subject is required` — empty subject
- `body is required` — empty body
- `agent "..." is not registered` — agent_id not found in the database

**Usage Tips**

- Use the `broadcast` channel (default) for messages all agents should see.
- Use named channels like `coordination` or `review` to organize conversations by topic.
- Pair with `announce_change` when finishing a file modification — send a message for discussion, announce the change for tracking.

---

### `read_messages`

Read messages from the coordination channel, optionally filtered by time and channel.

**Input Schema**

| Field     | Type     | Required | Description                                            |
|-----------|----------|----------|--------------------------------------------------------|
| `agent_id`| `string` | Yes      | Requesting agent ID (for authentication)               |
| `since`   | `string` | No       | ISO 8601 / RFC 3339 timestamp — only return messages after this time |
| `channel` | `string` | No       | Filter to a specific channel                           |

**Output Schema**

| Field      | Type              | Description       |
|------------|-------------------|--------------------|
| `messages` | `messageEntry[]`  | Array of messages  |

Each `messageEntry`:

| Field        | Type     | Description                  |
|--------------|----------|------------------------------|
| `id`         | `int64`  | Message ID                   |
| `sender_id`  | `string` | Agent ID of the sender       |
| `channel`    | `string` | Channel name                 |
| `subject`    | `string` | Message subject              |
| `body`       | `string` | Message body                 |
| `created_at` | `string` | RFC 3339 timestamp           |

**Example Request**

```json
{
  "agent_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "since": "2025-01-15T10:00:00Z",
  "channel": "broadcast"
}
```

**Example Response**

```json
{
  "messages": [
    {
      "id": 42,
      "sender_id": "x9y8z7w6-v5u4-3210-fedc-ba0987654321",
      "channel": "broadcast",
      "subject": "agent-stale",
      "body": "Agent coder-3 (role: coder) went stale and was removed. Its file claims have been released.",
      "created_at": "2025-01-15T10:05:23Z"
    }
  ]
}
```

**Error Conditions**

- `agent_id is required` — empty agent_id
- `agent "..." is not registered` — agent_id not found
- `parsing since timestamp: ...` — invalid RFC 3339 format

**Usage Tips**

- Use the `since` parameter to poll for new messages since your last read. Store the latest `created_at` and pass it on the next call.
- Omit `channel` to read messages from all channels at once.
- Watch for `agent-stale` subject messages on the `broadcast` channel — these indicate that another agent was cleaned up and its file claims are now available.

---

## File Claim Tools

### `claim_files`

Claim advisory locks on files to signal intent to modify them. Claims are advisory — they don't prevent filesystem access but allow agents to coordinate and avoid conflicts.

**Input Schema**

| Field      | Type       | Required | Description                     |
|------------|------------|----------|---------------------------------|
| `agent_id` | `string`   | Yes      | Claiming agent's ID             |
| `files`    | `string[]` | Yes      | File paths to claim             |

**Output Schema**

| Field       | Type              | Description                                  |
|-------------|-------------------|----------------------------------------------|
| `claimed`   | `string[]`        | File paths successfully claimed               |
| `conflicts` | `conflictEntry[]` | Files that couldn't be claimed (held by others) |

Each `conflictEntry`:

| Field     | Type     | Description                        |
|-----------|----------|------------------------------------|
| `file`    | `string` | The conflicting file path          |
| `held_by` | `string` | Agent ID of the current holder     |

**Example Request**

```json
{
  "agent_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "files": ["internal/auth/jwt.go", "internal/auth/jwt_test.go"]
}
```

**Example Response (success)**

```json
{
  "claimed": ["internal/auth/jwt.go", "internal/auth/jwt_test.go"],
  "conflicts": []
}
```

**Example Response (partial conflict)**

```json
{
  "claimed": ["internal/auth/jwt_test.go"],
  "conflicts": [
    {
      "file": "internal/auth/jwt.go",
      "held_by": "x9y8z7w6-v5u4-3210-fedc-ba0987654321"
    }
  ]
}
```

**Error Conditions**

- `agent_id is required` — empty agent_id
- `files is required` — empty files array

**Usage Tips**

- Call `get_file_claims` before `claim_files` to check for conflicts preemptively.
- File paths are normalized: `./foo/bar.go` and `foo/bar.go` are treated identically.
- Claims are released automatically when an agent goes stale (missed heartbeats for 2 minutes).
- If you get conflicts, coordinate with the holding agent via `send_message` or wait for them to release.

---

### `release_files`

Release advisory file locks that you previously claimed.

**Input Schema**

| Field      | Type       | Required | Description                     |
|------------|------------|----------|---------------------------------|
| `agent_id` | `string`   | Yes      | Releasing agent's ID            |
| `files`    | `string[]` | Yes      | File paths to release           |

**Output Schema**

| Field      | Type       | Description                      |
|------------|------------|----------------------------------|
| `released` | `string[]` | File paths that were released    |

**Example Request**

```json
{
  "agent_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "files": ["internal/auth/jwt.go"]
}
```

**Example Response**

```json
{
  "released": ["internal/auth/jwt.go"]
}
```

**Error Conditions**

- `agent_id is required` — empty agent_id
- `files is required` — empty files array

**Usage Tips**

- Always release files when you're done modifying them, even if you plan to claim others.
- Pair with `announce_change` — release the claim and announce what you changed so other agents can react.

---

### `get_file_claims`

Query current file claim status. Returns all active claims, optionally filtered to specific files.

**Input Schema**

| Field   | Type       | Required | Description                                        |
|---------|------------|----------|----------------------------------------------------|
| `files` | `string[]` | No       | Specific files to check (returns all if omitted)   |

**Output Schema**

| Field    | Type               | Description              |
|----------|--------------------|--------------------------|
| `claims` | `fileClaimEntry[]`  | Array of active claims   |

Each `fileClaimEntry`:

| Field        | Type     | Description                 |
|--------------|----------|-----------------------------|
| `file_path`  | `string` | The claimed file path       |
| `agent_id`   | `string` | Agent ID holding the claim  |
| `claimed_at` | `string` | RFC 3339 timestamp          |

**Example Request**

```json
{
  "files": ["internal/auth/jwt.go", "internal/auth/middleware.go"]
}
```

**Example Response**

```json
{
  "claims": [
    {
      "file_path": "internal/auth/jwt.go",
      "agent_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
      "claimed_at": "2025-01-15T10:02:15Z"
    }
  ]
}
```

**Error Conditions**

- Database errors are returned as tool errors.

**Usage Tips**

- Call with no `files` to get a full picture of what every agent is working on.
- Use this before `claim_files` to avoid conflicts preemptively.
- This tool does not require an `agent_id` — any caller can query claims.

---

## Change Announcement Tools

### `announce_change`

Announce a file change to other agents. Use this after modifying a file so other agents can detect potential conflicts and adjust their work.

**Input Schema**

| Field       | Type     | Required | Description                                    |
|-------------|----------|----------|------------------------------------------------|
| `agent_id`  | `string` | Yes      | Agent announcing the change                    |
| `file_path` | `string` | Yes      | Path of the modified file                      |
| `summary`   | `string` | Yes      | Human-readable description of the change       |

**Output Schema**

| Field       | Type    | Description                     |
|-------------|---------|---------------------------------|
| `change_id` | `int64` | Unique ID of the change record  |

**Example Request**

```json
{
  "agent_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "file_path": "internal/auth/jwt.go",
  "summary": "Added token expiration validation and refresh logic"
}
```

**Example Response**

```json
{
  "change_id": 17
}
```

**Error Conditions**

- `agent_id is required` — empty agent_id
- `file_path is required` — empty file_path
- `summary is required` — empty summary
- `agent "..." is not registered` — agent_id not found

**Usage Tips**

- Announce changes after each meaningful modification, not just at the end of a task.
- Other agents can poll `get_changes` to discover modifications that might affect their work.
- Combine with `release_files` — announce the change, then release the claim.

---

### `get_changes`

Query recent change announcements, optionally filtered by time and agent.

**Input Schema**

| Field      | Type     | Required | Description                                            |
|------------|----------|----------|--------------------------------------------------------|
| `since`    | `string` | No       | ISO 8601 / RFC 3339 timestamp — only changes after this |
| `agent_id` | `string` | No       | Filter to changes by a specific agent                  |

**Output Schema**

| Field     | Type             | Description            |
|-----------|------------------|------------------------|
| `changes` | `changeEntry[]`  | Array of change records |

Each `changeEntry`:

| Field          | Type     | Description                  |
|----------------|----------|------------------------------|
| `id`           | `int64`  | Change record ID             |
| `agent_id`     | `string` | Agent that made the change   |
| `file_path`    | `string` | Modified file path           |
| `summary`      | `string` | Description of the change    |
| `announced_at` | `string` | RFC 3339 timestamp           |

**Example Request**

```json
{
  "since": "2025-01-15T10:00:00Z"
}
```

**Example Response**

```json
{
  "changes": [
    {
      "id": 17,
      "agent_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
      "file_path": "internal/auth/jwt.go",
      "summary": "Added token expiration validation and refresh logic",
      "announced_at": "2025-01-15T10:12:45Z"
    }
  ]
}
```

**Error Conditions**

- `parsing since timestamp: ...` — invalid RFC 3339 format

**Usage Tips**

- Poll periodically with `since` set to the last seen `announced_at` to detect changes that might conflict with your current work.
- Filter by `agent_id` to see what a specific agent has been modifying.
- This tool does not require authentication — any caller can query changes.

---

## Database Schema

AgentMail uses four tables in Dolt:

| Table         | Purpose                                          |
|---------------|--------------------------------------------------|
| `agents`      | Registered agents with heartbeat timestamps      |
| `messages`    | Inter-agent messages organized by channel        |
| `file_claims` | Advisory file locks (one claim per file path)    |
| `changes`     | File change announcements with summaries         |

Tables are created automatically on first startup via `CREATE TABLE IF NOT EXISTS`.

## Typical Agent Workflow

```
1. register          → Get agent_id
2. heartbeat         → Send periodically (every ~60s)
3. get_file_claims   → Check what's claimed
4. claim_files       → Claim files you'll modify
5. (do work)
6. announce_change   → Tell others what you changed
7. release_files     → Release your claims
8. read_messages     → Check for coordination messages
9. (repeat 3-8)
```
