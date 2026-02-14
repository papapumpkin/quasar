+++
id = "mcp-skeleton"
title = "Create MCP server skeleton with SSE transport"
depends_on = ["go-module"]
+++

Create the MCP server using the official Go SDK with SSE/HTTP transport. This task
sets up the server framework — individual tools are registered in later tasks.

## Server setup

In `internal/agentmail/server.go`, create:

```go
type Server struct {
    store  *Store
    mcp    *server.MCPServer  // from the official Go SDK
    port   int
}

func NewServer(store *Store, port int) *Server
func (s *Server) Start(ctx context.Context) error
func (s *Server) Stop(ctx context.Context) error
```

### Requirements

- Use `server.NewMCPServer()` from the official Go SDK to create the MCP server
  instance with name "agentmail" and version from the quasar module
- Use SSE transport via `server.NewSSEServer()` for HTTP-based communication
- The server should listen on the configured port (default 8391)
- Register stub tools that return "not implemented" for all 9 tools:
  `register`, `heartbeat`, `send_message`, `read_messages`, `claim_files`,
  `release_files`, `get_file_claims`, `announce_change`, `get_changes`
- Support graceful shutdown via context cancellation

## Wire into cmd/agentmail/main.go

Update the entry point to:
1. Parse `--port` and `--dolt-dsn` flags
2. Connect to Dolt via `database/sql`
3. Call `InitDB` to ensure schema exists
4. Create `Store` and `Server`
5. Start the server and block until SIGINT/SIGTERM
6. Gracefully shut down

## Tests

Add `internal/agentmail/server_test.go` that:
- Starts the server on a random port
- Verifies the SSE endpoint is reachable via HTTP
- Verifies the server shuts down cleanly on context cancel
