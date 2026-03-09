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

	"github.com/papapumpkin/quasar/internal/nebula"
)

//go:embed templates/*.html templates/partials/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// ServerConfig holds the configuration for a web dashboard Server.
type ServerConfig struct {
	// Source provides SSE events to broadcast. May be nil in read-only or
	// static-dashboard mode.
	Source EventSource

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

	// Per-phase event accumulator for detail page rendering.
	accumulator *PhaseAccumulator

	// Gate prompting for web-based gate decisions.
	gater *WebGater

	// Per-page template sets, each with its own copy of the layout to avoid
	// conflicting block definitions across pages.
	pageTmpls map[string]*template.Template

	// SSE connection tracking for graceful drain.
	sseClients   map[chan Event]struct{}
	sseMu        sync.Mutex
	sseCloseOnce sync.Once
}

// templateFuncMap provides custom functions available in all templates.
var templateFuncMap = template.FuncMap{
	"add": func(a, b int) int { return a + b },
	"truncate": func(s string, max int) string {
		if len(s) <= max {
			return s
		}
		return s[:max-1] + "\u2026"
	},
}

// pageTemplateFiles lists the page template files that each get their own
// clone of the layout to avoid conflicting block definitions.
var pageTemplateFiles = []string{
	"templates/dashboard.html",
	"templates/dag.html",
	"templates/phase_detail.html",
	"templates/gate_list.html",
	"templates/diff.html",
}

// NewServer creates a new web dashboard Server with the given configuration.
// It registers HTTP routes and parses embedded templates but does not start
// listening.
func NewServer(cfg ServerConfig) (*Server, error) {
	pageTmpls, err := parsePageTemplates()
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	s := &Server{
		cfg:         cfg,
		mux:         http.NewServeMux(),
		templates:   pageTmpls["dashboard.html"],
		pageTmpls:   pageTmpls,
		sseClients:  make(map[chan Event]struct{}),
		accumulator: NewPhaseAccumulator(),
	}
	s.gater = NewWebGater(s)
	s.registerRoutes()
	return s, nil
}

// parsePageTemplates builds a per-page template map where each page gets its
// own clone of the shared layout. This prevents conflicting block definitions
// (e.g. "title", "content") from overriding each other across pages.
// The map key is the template filename (e.g. "dashboard.html").
func parsePageTemplates() (map[string]*template.Template, error) {
	// Parse shared layout and partials first as the base template.
	layout, err := template.New("layout").Funcs(templateFuncMap).ParseFS(
		templateFS, "templates/layout.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse layout: %w", err)
	}

	// Also parse partials into layout if they exist.
	partials, _ := templateFS.ReadDir("templates/partials")
	if len(partials) > 0 {
		layout, err = layout.ParseFS(templateFS, "templates/partials/*.html")
		if err != nil {
			return nil, fmt.Errorf("parse partials: %w", err)
		}
	}

	tmpls := make(map[string]*template.Template, len(pageTemplateFiles))
	for _, page := range pageTemplateFiles {
		// Clone the layout so each page gets independent block definitions.
		pageTmpl, err := layout.Clone()
		if err != nil {
			return nil, fmt.Errorf("clone layout for %s: %w", page, err)
		}

		pageTmpl, err = pageTmpl.ParseFS(templateFS, page)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", page, err)
		}

		// Extract filename for the map key.
		name := page[len("templates/"):]
		tmpls[name] = pageTmpl
	}

	return tmpls, nil
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

	// Start the phase accumulator if an event source is available.
	if s.cfg.Source != nil {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.accumulator.Start(ctx, s.cfg.Source)
		}()
	}

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

// Gater returns the server's WebGater for use as a nebula.GatePrompter.
func (s *Server) Gater() *WebGater {
	return s.gater
}

// registerRoutes sets up the HTTP route handlers.
func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/", s.handleDashboard)
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc("/events", s.handleSSE)
	s.mux.HandleFunc("/phase/{id}", s.handlePhaseDetail)
	s.mux.HandleFunc("GET /dag", s.handleDAG)
	s.mux.HandleFunc("GET /gates", s.handleGateList)
	s.mux.HandleFunc("POST /gate/{id}", s.handleGateResolve)
	s.mux.Handle("/static/", http.FileServerFS(staticFS))
}

// handleHealthz serves a simple health check endpoint.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok")
}
