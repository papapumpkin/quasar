+++
id = "file-claim-tools"
title = "Implement file claim MCP tools"
depends_on = ["dolt-client", "mcp-skeleton"]
+++

Implement the file claim MCP tools that let agents declare advisory locks on files.

## Tools

### `claim_files`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent_id` | string | yes | Claiming agent's ID |
| `files` | string[] | yes | File paths to claim |

Returns: `{ "claimed": ["path/a.go"], "conflicts": [{ "file": "path/b.go", "held_by": "agent-xyz" }] }`

Delegates to `Store.ClaimFiles`. For each file, attempt to claim it. If already
claimed by another agent, include it in `conflicts` with the current holder's ID.
A file already claimed by the same agent is a no-op success.

### `release_files`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent_id` | string | yes | Releasing agent's ID |
| `files` | string[] | yes | File paths to release |

Returns: `{ "released": ["path/a.go"] }`

Delegates to `Store.ReleaseFiles`. Only release files claimed by this agent.

### `get_file_claims`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `files` | string[] | no | Specific files to check (all if omitted) |

Returns: `{ "claims": [{ "file_path", "agent_id", "claimed_at" }] }`

Delegates to `Store.GetFileClaims`.

## Implementation

In `internal/agentmail/tools_fileclaim.go`:
- Define input structs with JSON tags
- Register all three tools with the MCP server, replacing stubs
- Normalize file paths (strip leading `./`, use forward slashes)

## Tests

In `internal/agentmail/tools_fileclaim_test.go`:
- Agent A claims files, agent B attempts same files — verify conflicts
- Agent A releases, agent B can now claim
- `get_file_claims` returns correct state after operations
- Verify path normalization (e.g., `./foo/bar.go` and `foo/bar.go` are the same)
