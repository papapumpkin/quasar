// Command agentmail runs the agentmail MCP server for inter-agent coordination.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/aaronsalm/quasar/internal/agentmail"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	port := flag.Int("port", 8391, "port to listen on")
	doltDSN := flag.String("dolt-dsn", "root@tcp(127.0.0.1:3306)/agentmail", "Dolt database DSN")
	flag.Parse()

	// TODO: start MCP server using port and doltDSN
	fmt.Fprintf(os.Stderr, "agentmail: not yet implemented (port=%d, dsn=%s)\n", *port, *doltDSN)
	log.Fatal("agentmail server not yet implemented")
}
