+++
id = "http-skeleton"
title = "HTTP server skeleton with SSE endpoint and bus subscriber"
type = "feature"
priority = 1
depends_on = []
scope = ["internal/web/**"]
+++

## Problem

Quasar currently only supports a BubbleTea TUI (`internal/tui/`) and a stderr printer (`internal/ui/`) for monitoring nebula execution. There is no way to observe or interact with a running nebula from a web browser. The Pulsar event bus will provide a stream of typed events, but there is no HTTP layer to bridge those events to browser clients via SSE.

## Solution

Create the `internal/web/` package with a `Server` struct that wraps `http.ServeMux`, accepts an event bus subscriber interface, and exposes an SSE endpoint at `/events`. The server should follow the same dependency-injection patterns as the rest of the codebase: define a small `EventSource` interface where it is consumed (in `internal/web/`), not where it is implemented.

### Server struct

```go
// internal/web/server.go

// EventSource provides a stream of typed events for SSE broadcasting.
// Implemented by the Pulsar event bus subscriber.
type EventSource interface {
    // Subscribe returns a channel that receives JSON-encoded event payloads.
    // The returned cancel function unsubscribes and closes the channel.
    Subscribe(ctx context.Context) (events <-chan Event, cancel func())
}

// Event represents a single SSE event with a named type and JSON data.
type Event struct {
    Type string // SSE event name: "phase-status", "progress", "agent-done", etc.
    Data string // JSON-encoded payload
}

// Server is the web dashboard HTTP server.
type Server struct {
    mux      *http.ServeMux
    source   EventSource
    addr     string
    templates *template.Template
    nebula   *nebula.Nebula   // current nebula for template rendering
    state    *nebula.State    // current state for template rendering
    mu       sync.RWMutex    // protects nebula/state
}

// New creates a Server bound to the given address with an event source.
func New(addr string, source EventSource, n *nebula.Nebula, state *nebula.State) *Server {
    s := &Server{
        mux:    http.NewServeMux(),
        source: source,
        addr:   addr,
        nebula: n,
        state:  state,
    }
    s.templates = template.Must(template.ParseFS(templateFS, "templates/*.html"))
    s.routes()
    return s
}
```

### SSE endpoint

```go
// internal/web/sse.go

// handleSSE streams server-sent events to the connected client.
// Each event from the EventSource is written as a named SSE message.
// The connection stays open until the client disconnects or the
// server's context is cancelled.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "streaming not supported", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    events, cancel := s.source.Subscribe(r.Context())
    defer cancel()

    for {
        select {
        case <-r.Context().Done():
            return
        case evt, ok := <-events:
            if !ok {
                return
            }
            fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, evt.Data)
            flusher.Flush()
        }
    }
}
```

### Static file serving

Use `embed.FS` to bundle HTMX and a minimal CSS file:

```go
//go:embed static/*
var staticFS embed.FS

//go:embed templates/*
var templateFS embed.FS
```

### Route registration

```go
func (s *Server) routes() {
    s.mux.HandleFunc("GET /", s.handleDashboard)
    s.mux.HandleFunc("GET /events", s.handleSSE)
    s.mux.HandleFunc("GET /phase/{id}", s.handlePhaseDetail)
    s.mux.Handle("GET /static/", http.FileServerFS(staticFS))
}
```

### Lifecycle

```go
// Start begins listening. It blocks until the context is cancelled,
// then gracefully shuts down with a 5-second deadline.
func (s *Server) Start(ctx context.Context) error {
    srv := &http.Server{Addr: s.addr, Handler: s.mux}

    go func() {
        <-ctx.Done()
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        srv.Shutdown(shutdownCtx)
    }()

    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        return fmt.Errorf("web server error: %w", err)
    }
    return nil
}
```

## Files

- `internal/web/server.go` — `Server` struct, `EventSource` interface, `New()` constructor, `routes()`, `Start()`
- `internal/web/server_test.go` — test server creation, route registration, graceful shutdown
- `internal/web/sse.go` — `handleSSE` handler, SSE formatting
- `internal/web/sse_test.go` — test SSE streaming with mock EventSource, client disconnect handling
- `internal/web/event.go` — `Event` type definition
- `internal/web/static/htmx.min.js` — HTMX library (vendored)
- `internal/web/static/htmx-sse.js` — HTMX SSE extension
- `internal/web/static/style.css` — base CSS with dark theme matching Quasar's galactic aesthetic

## Acceptance Criteria

- [ ] `Server` struct compiles and accepts an `EventSource` interface
- [ ] `GET /events` returns `Content-Type: text/event-stream` and streams events
- [ ] SSE connections are cleaned up when the client disconnects
- [ ] `Server.Start(ctx)` shuts down gracefully when context is cancelled
- [ ] Static files are served from embedded FS at `/static/`
- [ ] All exported types and functions have GoDoc comments
- [ ] `go test ./internal/web/...` passes with no failures
- [ ] `go vet ./internal/web/...` reports no issues
