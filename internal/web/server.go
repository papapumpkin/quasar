// Package web provides an HTTP server that serves a live dashboard for
// nebula execution. It bridges bus events to the browser via SSE and
// renders phase status, cost, and cycle information.
package web

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/papapumpkin/quasar/internal/bus"
	"github.com/papapumpkin/quasar/internal/nebula"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// ServerConfig holds the configuration for a web dashboard Server.
type ServerConfig struct {
	// Bus is the event bus to subscribe to for SSE streaming. May be nil
	// in read-only mode.
	Bus bus.Bus

	// NebulaDir is the path to the nebula directory being viewed.
	NebulaDir string

	// Port is the TCP port to listen on. 0 means auto-assign a free port.
	Port int

	// ReadOnly disables execution controls (used by cockpit --web).
	ReadOnly bool
}

// Server is the web dashboard HTTP server. It holds all dependencies and
// manages the SSE connection lifecycle. Safe for concurrent use.
type Server struct {
	cfg        ServerConfig
	mux        *http.ServeMux
	httpServer *http.Server
	templates  *template.Template
	addr       string
	wg         sync.WaitGroup

	// Nebula state for dashboard rendering, guarded by mu.
	nebula    *nebula.Nebula
	state     *nebula.State
	startTime time.Time
	mu        sync.RWMutex

	// SSE connection tracking for graceful drain.
	sseClients   map[chan bus.Event]struct{}
	sseMu        sync.Mutex
	sseCloseOnce sync.Once
}

// NewServer creates a new web dashboard Server with the given configuration.
// It registers HTTP routes and parses embedded templates but does not start
// listening.
func NewServer(cfg ServerConfig) (*Server, error) {
	tmpl, err := template.ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	s := &Server{
		cfg:        cfg,
		mux:        http.NewServeMux(),
		templates:  tmpl,
		sseClients: make(map[chan bus.Event]struct{}),
	}
	s.registerRoutes()
	return s, nil
}

// SetNebula updates the nebula and state used for dashboard rendering.
// Thread-safe; can be called while the server is running.
func (s *Server) SetNebula(n *nebula.Nebula, st *nebula.State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nebula = n
	s.state = st
	if n != nil && s.startTime.IsZero() {
		s.startTime = time.Now()
	}
}

// Start begins listening on the configured port (0 = auto-assign).
// Returns the resolved address (e.g. "127.0.0.1:8080"). The server
// shuts down gracefully when ctx is cancelled.
func (s *Server) Start(ctx context.Context) (string, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.Port))
	if err != nil {
		return "", fmt.Errorf("listen on port %d: %w", s.cfg.Port, err)
	}
	s.addr = ln.Addr().String()

	s.httpServer = &http.Server{Handler: s.mux}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "[web] server error: %v\n", err)
		}
	}()

	// Shutdown on context cancellation.
	go func() {
		<-ctx.Done()
		s.drainSSE()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "[web] shutdown error: %v\n", err)
		}
	}()

	return s.addr, nil
}

// Wait blocks until the server has fully shut down.
func (s *Server) Wait() {
	s.wg.Wait()
}

// Addr returns the resolved listen address after Start. Returns empty
// string if the server has not been started.
func (s *Server) Addr() string {
	return s.addr
}

// registerRoutes sets up the HTTP route handlers.
func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/", s.handleDashboard)
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc("/events", s.handleSSE)
	s.mux.Handle("/static/", http.FileServerFS(staticFS))
}

// handleHealthz serves a simple health check endpoint.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok")
}

// handleSSE streams bus events to the client as Server-Sent Events.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Flush headers immediately so the client sees the 200 response
	// and Content-Type before any events arrive.
	flusher.Flush()

	// Create a per-client event channel.
	ch := make(chan bus.Event, 64)
	s.addSSEClient(ch)
	defer s.removeSSEClient(ch)

	// Subscribe to bus if available.
	var sub bus.Subscription
	if s.cfg.Bus != nil {
		sub = s.cfg.Bus.Subscribe("web-sse", 128)
		defer sub.Unsubscribe()

		// Forward bus events to the client channel. The channel is
		// closed by drainSSE during shutdown — do not close here to
		// avoid double-close panics.
		go func() {
			for ev := range sub.Events() {
				select {
				case ch <- ev:
				default:
					// Drop event if client is slow.
				}
			}
		}()
	}

	// Stream events to the client.
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: [%s] %s %s\n\n", ev.Kind, ev.PhaseID, ev.Message)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// addSSEClient registers a client channel for SSE drain tracking.
func (s *Server) addSSEClient(ch chan bus.Event) {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	s.sseClients[ch] = struct{}{}
}

// removeSSEClient unregisters a client channel.
func (s *Server) removeSSEClient(ch chan bus.Event) {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	delete(s.sseClients, ch)
}

// drainSSE closes all active SSE client channels to unblock handlers
// during graceful shutdown.
func (s *Server) drainSSE() {
	s.sseCloseOnce.Do(func() {
		s.sseMu.Lock()
		defer s.sseMu.Unlock()
		for ch := range s.sseClients {
			close(ch)
			delete(s.sseClients, ch)
		}
	})
}
