// Command agentmail runs the agentmail MCP server for inter-agent coordination.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	_ "github.com/aaronsalm/quasar/internal/agentmail"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	port := flag.Int("port", 8391, "port to listen on")
	doltDSN := flag.String("dolt-dsn", "root@tcp(127.0.0.1:3306)/agentmail", "Dolt database DSN")
	flag.Parse()

	// TODO: implement full MCP server using doltDSN for persistence.
	_ = *doltDSN

	addr := fmt.Sprintf(":%d", *port)
	fmt.Fprintf(os.Stderr, "agentmail: listening on %s (stub — MCP handlers not yet implemented)\n", addr)

	// Serve a minimal SSE endpoint so the health check succeeds.
	http.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	})

	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Fprintf(os.Stderr, "agentmail: %v\n", err)
		os.Exit(1)
	}
}
