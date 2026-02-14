+++
id = "go-module"
title = "Add dependencies and package scaffolding"
type = "task"
priority = 1
+++

Set up the Go package structure and add required dependencies for agentmail.

## Dependencies to add

Run:
```bash
go get github.com/go-sql-driver/mysql
go get github.com/modelcontextprotocol/go-sdk
```

## Package structure

Create the following directory and file scaffolding:

### `internal/agentmail/`
The core agentmail library. Create a `doc.go` with a package comment:
```go
// Package agentmail implements an MCP server for inter-agent communication
// backed by a Dolt database.
package agentmail
```

### `cmd/agentmail/`
The standalone binary entry point. Create `cmd/agentmail/main.go` with a minimal
`main()` that parses `--port` (default 8391) and `--dolt-dsn` (default
`root@tcp(127.0.0.1:3306)/agentmail`) flags, then exits with a TODO log message.

## Verification

- `go build ./cmd/agentmail/` compiles without errors
- `go build ./internal/agentmail/` compiles without errors
- `go mod tidy` leaves no unused dependencies
