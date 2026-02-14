+++
id = "docs"
title = "Document agentmail architecture and usage"
depends_on = ["quasar-mcp-wiring", "integration-tests"]
+++

Add documentation for the agentmail MCP server to the project README and create
a standalone reference document.

## README section

Add an "AgentMail" section to the main `README.md` covering:

1. **What it is**: A shared mailbox MCP server that enables parallel quasar agents
   to coordinate via messages, file claims, and change announcements
2. **Architecture**: Dolt-backed SQL database, SSE/HTTP MCP transport, single
   server shared by all agents
3. **Prerequisites**: Dolt must be installed and running (`dolt sql-server`)
4. **Configuration**: Show `.quasar.yaml` snippet and nebula TOML `[execution]`
   settings
5. **Quick start**: Step-by-step to enable agentmail for a nebula run

## MCP tool reference

Create `docs/agentmail.md` with a complete reference for all 9 MCP tools:

| Tool | Input | Output | Description |
|------|-------|--------|-------------|
| `register` | `{name, role}` | `{agent_id}` | Register a new agent |
| `heartbeat` | `{agent_id}` | `{ok}` | Update agent heartbeat |
| `send_message` | `{agent_id, channel?, subject, body}` | `{message_id}` | Send a message |
| `read_messages` | `{agent_id, since?, channel?}` | `{messages[]}` | Read messages |
| `claim_files` | `{agent_id, files[]}` | `{claimed[], conflicts[]}` | Claim advisory file locks |
| `release_files` | `{agent_id, files[]}` | `{released[]}` | Release file claims |
| `get_file_claims` | `{files[]?}` | `{claims[]}` | Query current file claims |
| `announce_change` | `{agent_id, file_path, summary}` | `{change_id}` | Announce a file change |
| `get_changes` | `{since?, agent_id?}` | `{changes[]}` | Query recent changes |

For each tool, include:
- Full input schema with types and descriptions
- Example request/response JSON
- Error conditions
- Usage tips (e.g., "Call `get_file_claims` before `claim_files` to check for
  conflicts preemptively")

## Architecture diagram

Include a simple ASCII or Mermaid diagram showing:
```
┌─────────┐     ┌─────────┐
│ Coder-1 │     │ Coder-2 │
└────┬────┘     └────┬────┘
     │    SSE/HTTP   │
     └──────┬────────┘
       ┌────▼────┐
       │AgentMail│
       │MCP Server│
       └────┬────┘
       ┌────▼────┐
       │  Dolt   │
       │   DB    │
       └─────────┘
```
