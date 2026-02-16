// Command agentmail runs the agentmail MCP server for inter-agent coordination.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/go-sql-driver/mysql"

	"github.com/aaronsalm/quasar/internal/agentmail"
)

func main() {
	port := flag.Int("port", 8391, "port to listen on")
	doltDSN := flag.String("dolt-dsn", "root@tcp(127.0.0.1:3306)/agentmail", "Dolt database DSN")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := sql.Open("mysql", *doltDSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentmail: failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := agentmail.InitDB(ctx, db); err != nil {
		fmt.Fprintf(os.Stderr, "agentmail: %v\n", err)
		os.Exit(1)
	}

	store := agentmail.NewStore(db)
	srv := agentmail.NewServer(store, *port, nil)

	fmt.Fprintf(os.Stderr, "agentmail: starting MCP server on port %d\n", *port)
	if err := srv.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "agentmail: %v\n", err)
		os.Exit(1)
	}

	// Block until signal.
	<-ctx.Done()
	fmt.Fprintf(os.Stderr, "agentmail: shutting down...\n")

	if err := srv.Stop(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "agentmail: shutdown error: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "agentmail: stopped\n")
}
