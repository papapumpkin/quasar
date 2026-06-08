// Package cockpit implements the optional read-mostly HTTP/WebSocket API that
// backs the Quasar cockpit UI. It lives in the supervisor process: a single
// binary serves JSON under /api/v1, a delta-event WebSocket at
// /api/v1/subscribe, and (when built with the `cockpit` tag) the embedded React
// bundle. The TUI remains the canonical headless interface; the cockpit is
// purely additive and shares the same Notifier as its source of truth.
//
// The server depends on narrow, consumer-defined interfaces (RepoLister,
// NebulaService, RunService, RuntimeController) that the concrete fabric and
// repos stores already satisfy, keeping handlers unit-testable with fakes.
package cockpit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/repos"
)

// RepoLister lists registered repos. Satisfied by *repos.Registry.
type RepoLister interface {
	List(ctx context.Context, statusFilter string) ([]*repos.Repo, error)
}

// NebulaService is the read/write surface over nebulas the cockpit needs.
// Satisfied by *fabric.NebulaStore.
type NebulaService interface {
	List(ctx context.Context, filter fabric.ListFilter) ([]*fabric.NebulaSummary, error)
	Get(ctx context.Context, id string) (*fabric.Nebula, error)
	SetStatus(ctx context.Context, id, newStatus string) error
	Undelete(ctx context.Context, id string) error
}

// RunService is the read surface over constellation runs the cockpit needs.
// Satisfied by *fabric.ConstellationRunStore.
type RunService interface {
	ListByState(ctx context.Context, state string) ([]*fabric.RunRow, error)
	GetRun(ctx context.Context, id string) (*fabric.RunRow, error)
	InvocationsForRun(ctx context.Context, runID string) ([]*fabric.StarInvocationRow, error)
}

// RuntimeController exposes the run lifecycle actions the cockpit forwards. It
// is optional: when nil, the pause/resume/kill endpoints return 503. The
// supervisor wires a real implementation once available.
type RuntimeController interface {
	Pause(ctx context.Context, runID string) error
	Resume(ctx context.Context, runID string) error
	Kill(ctx context.Context, runID string) error
}

// Opts configures a Server. Repos/Nebulas/Runs are required when Enabled is
// true; Runtime and Notifier are optional (a nil Notifier is replaced with a
// fresh one so Subscribe always works).
type Opts struct {
	Enabled  bool
	Token    string
	Repos    RepoLister
	Nebulas  NebulaService
	Runs     RunService
	Runtime  RuntimeController
	Notifier *Notifier
	Logger   io.Writer
}

// Server serves the cockpit JSON+WebSocket API and the embedded static bundle.
type Server struct {
	enabled  bool
	token    string
	repos    RepoLister
	nebulas  NebulaService
	runs     RunService
	runtime  RuntimeController
	notifier *Notifier
	logger   io.Writer
}

// New constructs a Server from opts. It returns an error if the cockpit is
// enabled but a required data service is missing.
func New(opts Opts) (*Server, error) {
	if opts.Enabled {
		if opts.Repos == nil || opts.Nebulas == nil || opts.Runs == nil {
			return nil, errors.New("cockpit: Repos, Nebulas, and Runs are required when enabled")
		}
	}
	n := opts.Notifier
	if n == nil {
		n = NewNotifier()
	}
	return &Server{
		enabled:  opts.Enabled,
		token:    opts.Token,
		repos:    opts.Repos,
		nebulas:  opts.Nebulas,
		runs:     opts.Runs,
		runtime:  opts.Runtime,
		notifier: n,
		logger:   opts.Logger,
	}, nil
}

// Notifier returns the server's event notifier so the runtime, scheduler, and
// TUI fleet view can publish to and subscribe from the same source of truth.
func (s *Server) Notifier() *Notifier { return s.notifier }

// Routes returns the HTTP handler for the cockpit. When the cockpit is
// disabled, every route — API and bundle alike — returns 404 so the feature
// leaves no surface. Otherwise the authenticated JSON API is mounted under
// /api/v1 and the unauthenticated static bundle (when compiled in) under /.
func (s *Server) Routes() http.Handler {
	if !s.enabled {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.NotFound(w, nil)
			w.WriteHeader(http.StatusNotFound)
		})
	}

	api := http.NewServeMux()
	s.registerAPI(api)

	root := http.NewServeMux()
	// The API is bearer-token protected; the bundle is public so the login
	// page can load and prompt for the token.
	root.Handle("/api/v1/", requireToken(s.token, http.StripPrefix("/api/v1", api)))
	root.Handle("/", s.staticHandler())
	return root
}

// registerAPI mounts the JSON endpoints on mux (already stripped of the
// /api/v1 prefix). The WebSocket subscribe endpoint is mounted here too because
// it shares the same auth and prefix.
func (s *Server) registerAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /repos", s.handleListRepos)
	mux.HandleFunc("GET /fleet", s.handleFleet)
	mux.HandleFunc("GET /nebulas", s.handleListNebulas)
	mux.HandleFunc("GET /nebulas/{id}", s.handleGetNebula)
	mux.HandleFunc("GET /nebulas/{id}/phases", s.handleNebulaPhases)
	mux.HandleFunc("POST /nebulas/{id}/approve", s.handleApprove)
	mux.HandleFunc("POST /nebulas/{id}/reject", s.handleReject)
	mux.HandleFunc("POST /nebulas/{id}/undelete", s.handleUndelete)

	mux.HandleFunc("GET /runs", s.handleListRuns)
	mux.HandleFunc("GET /runs/{id}", s.handleGetRun)
	mux.HandleFunc("GET /runs/{id}/tail", s.handleTail)
	mux.HandleFunc("POST /runs/{id}/pause", s.handlePause)
	mux.HandleFunc("POST /runs/{id}/resume", s.handleResume)
	mux.HandleFunc("POST /runs/{id}/kill", s.handleKill)

	mux.HandleFunc("GET /subscribe", s.handleSubscribe)
}

// Run starts the HTTP server on addr and serves until ctx is cancelled, at
// which point it shuts down gracefully. It returns nil on clean shutdown.
func (s *Server) Run(ctx context.Context, addr string) error {
	srv := &http.Server{Addr: addr, Handler: s.Routes()}

	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			return fmt.Errorf("cockpit: shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		return err
	}
}

// staticHandler serves the embedded React bundle. With no bundle compiled in
// (the default build), it 404s every path. With a bundle, unknown non-asset
// paths fall back to index.html so the SPA router can handle them.
func (s *Server) staticHandler() http.Handler {
	b := bundle()
	if b == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "cockpit bundle not built", http.StatusNotFound)
		})
	}
	fileServer := http.FileServer(http.FS(b))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fileServer.ServeHTTP(w, r)
	})
}

// handleSubscribe upgrades to a WebSocket and streams delta events for the
// requested topics (?topics=fleet,runs; empty means all). It blocks until the
// client disconnects or the connection errors, then unsubscribes.
func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	topics := parseTopics(r.URL.Query().Get("topics"))
	conn, err := handshake(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "websocket upgrade failed: "+err.Error())
		return
	}
	defer conn.Close()

	events, unsub := s.notifier.Subscribe(topics)
	defer unsub()

	done := make(chan struct{})
	go conn.readLoop(done)

	for {
		select {
		case <-done:
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if err := conn.writeJSON(ev); err != nil {
				return
			}
		}
	}
}

// --- shared JSON helpers ---

// writeJSON encodes v as JSON with the given status code. Encode errors are
// logged (the header is already sent, so the response can't be corrected).
func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil && s.logger != nil {
		fmt.Fprintf(s.logger, "cockpit: encode response: %v\n", err)
	}
}

// errorBody is the JSON shape returned for every error response.
type errorBody struct {
	Error string `json:"error"`
}

// writeError writes a JSON error body with the given status code. It is a
// package function (not a method) so the auth middleware can call it before a
// Server is in scope.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Error: msg})
}

// writeStoreError maps a store error to an HTTP response: not-found sentinels
// become 404, everything else 500.
func (s *Server) writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fabric.ErrNebulaNotFound), errors.Is(err, fabric.ErrRunNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// parseTopics splits a comma-separated topics query value, trimming blanks. An
// empty value yields a nil slice (subscribe to all topics).
func parseTopics(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for _, t := range strings.Split(raw, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}
