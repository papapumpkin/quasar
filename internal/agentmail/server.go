package agentmail

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Default cleanup configuration values.
const (
	DefaultCleanupInterval = 30 * time.Second
	DefaultStaleTimeout    = 2 * time.Minute
)

// ServerConfig holds optional configuration for the agentmail Server.
type ServerConfig struct {
	// CleanupInterval is how often the stale-agent cleanup runs. Zero uses the default (30s).
	CleanupInterval time.Duration
	// StaleTimeout is the duration after which an agent without a heartbeat is
	// considered stale and removed. Zero uses the default (2m).
	StaleTimeout time.Duration
}

// Version is the agentmail server version, matching the quasar module.
const Version = "0.1.0"

// Server is the in-process agentmail MCP server. It registers coordination
// tools and serves them over SSE/HTTP.
type Server struct {
	store           *Store
	mcp             *mcp.Server
	port            int
	srv             *http.Server
	ln              net.Listener
	cleanupInterval time.Duration
	staleTimeout    time.Duration
	cancelCleanup   context.CancelFunc
	cleanupDone     chan struct{}
}

// NewServer creates a new agentmail MCP server with coordination tool
// registrations. Pass nil for cfg to use default configuration.
func NewServer(store *Store, port int, cfg *ServerConfig) *Server {
	mcpServer := mcp.NewServer(
		&mcp.Implementation{
			Name:    "agentmail",
			Version: Version,
		},
		nil,
	)

	cleanupInterval := DefaultCleanupInterval
	staleTimeout := DefaultStaleTimeout
	if cfg != nil {
		if cfg.CleanupInterval > 0 {
			cleanupInterval = cfg.CleanupInterval
		}
		if cfg.StaleTimeout > 0 {
			staleTimeout = cfg.StaleTimeout
		}
	}

	s := &Server{
		store:           store,
		mcp:             mcpServer,
		port:            port,
		cleanupInterval: cleanupInterval,
		staleTimeout:    staleTimeout,
	}

	s.registerTools()

	return s
}

// registerTools registers coordination tools with the MCP server.
func (s *Server) registerTools() {
	s.registerLifecycleTools()
	s.registerMailboxTools()
	s.registerChangeTools()
	s.registerFileClaimTools()
}

// Start begins serving the MCP server over SSE/HTTP on the configured port.
// It also starts the stale-agent cleanup goroutine. It blocks until the server
// is ready to accept connections.
func (s *Server) Start(ctx context.Context) error {
	handler := mcp.NewSSEHandler(func(_ *http.Request) *mcp.Server {
		return s.mcp
	}, nil)

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("agentmail: listen on port %d: %w", s.port, err)
	}
	s.ln = ln

	s.srv = &http.Server{Handler: handler}

	go func() {
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "agentmail: serve error: %v\n", err)
		}
	}()

	// Start stale-agent cleanup goroutine.
	cleanupCtx, cancel := context.WithCancel(ctx)
	s.cancelCleanup = cancel
	s.cleanupDone = make(chan struct{})
	go s.runCleanupLoop(cleanupCtx)

	return nil
}

// Addr returns the listener address, useful for tests with port 0.
func (s *Server) Addr() net.Addr {
	if s.ln != nil {
		return s.ln.Addr()
	}
	return nil
}

// Stop gracefully shuts down the HTTP server and the cleanup goroutine.
func (s *Server) Stop(ctx context.Context) error {
	if s.cancelCleanup != nil {
		s.cancelCleanup()
		<-s.cleanupDone
	}
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

// runCleanupLoop periodically runs CleanupStaleAgents and broadcasts a message
// for each removed agent. It stops when the context is canceled.
func (s *Server) runCleanupLoop(ctx context.Context) {
	defer close(s.cleanupDone)

	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runCleanup(ctx)
		}
	}
}

// runCleanup executes a single stale-agent cleanup pass. It finds stale
// agents, releases their claims, sends broadcast notifications (while the
// agent row still exists for foreign-key integrity), then deletes the agent.
func (s *Server) runCleanup(ctx context.Context) {
	stale, err := s.store.FindStaleAgents(ctx, s.staleTimeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentmail: cleanup find error: %v\n", err)
		return
	}

	for _, a := range stale {
		// Release file claims.
		if err := s.store.ReleaseAllAgentClaims(ctx, a.ID); err != nil {
			fmt.Fprintf(os.Stderr, "agentmail: cleanup release claims for %s: %v\n", a.ID, err)
			continue
		}

		// Broadcast while agent row still exists (FK on messages.sender_id).
		body := fmt.Sprintf("Agent %s (role: %s) went stale and was removed. Its file claims have been released.", a.Name, a.Role)
		if _, err := s.store.SendMessage(ctx, a.ID, "broadcast", "agent-stale", body); err != nil {
			fmt.Fprintf(os.Stderr, "agentmail: cleanup broadcast for %s: %v\n", a.ID, err)
		}

		// Delete the agent row.
		if err := s.store.DeleteAgent(ctx, a.ID); err != nil {
			fmt.Fprintf(os.Stderr, "agentmail: cleanup delete agent %s: %v\n", a.ID, err)
		}
	}
}
