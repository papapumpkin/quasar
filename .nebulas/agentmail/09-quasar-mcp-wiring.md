+++
id = "quasar-mcp-wiring"
title = "Wire agentmail MCP server into quasar lifecycle"
type = "task"
priority = 2
+++

Extend quasar to manage the agentmail MCP server lifecycle and pass the MCP
configuration to Claude Code agents.

## Agent struct changes

In `internal/agent/agent.go`, add an optional `MCPConfig` field to the `Agent`
struct:

```go
type MCPConfig struct {
    ConfigPath string // path to generated MCP config JSON
}
```

## Claude invocation changes

In `internal/claude/claude.go`, update `Invoke` to accept an optional MCP config
path. When set, pass `--mcp-config <path>` to the `claude -p` CLI invocation.

The generated MCP config JSON should look like:
```json
{
    "mcpServers": {
        "agentmail": {
            "url": "http://localhost:<port>/sse"
        }
    }
}
```

## Server lifecycle

In `cmd/nebula.go` (or wherever the nebula runner lives), add agentmail server
management:

1. Before starting workers: if agentmail is enabled in config, start the agentmail
   server process and wait for it to be healthy (HTTP GET to the SSE endpoint)
2. Generate the MCP config JSON to a temp file
3. Pass the config path to each agent
4. After all workers finish: gracefully shut down the agentmail server

## Configuration

In `internal/config/config.go`, add agentmail configuration:
```go
type AgentMailConfig struct {
    Enabled bool   `mapstructure:"enabled"`
    Port    int    `mapstructure:"port"`
    DoltDSN string `mapstructure:"dolt_dsn"`
}
```

Wire through Viper so it can be set via `.quasar.yaml`:
```yaml
agentmail:
  enabled: true
  port: 8391
  dolt_dsn: "root@tcp(127.0.0.1:3306)/agentmail"
```

And via nebula `[execution]`:
```toml
[execution]
agentmail = true
agentmail_port = 8391
```

## Important constraints

- Do NOT modify the existing CLI interface — this is purely additive
- The agentmail server should be optional; quasar works fine without it
- If agentmail is enabled but Dolt isn't running, fail fast with a clear error

## Tests

- Test that `Invoke` adds `--mcp-config` flag when MCPConfig is set
- Test that `Invoke` does NOT add the flag when MCPConfig is nil
- Test the MCP config JSON generation
- Test server health check logic
