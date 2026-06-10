package cockpit

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
)

// RuntimeActions is the cockpit's write surface onto the runtime. Implemented in
// a later task; the fleet page does not use it.
type RuntimeActions interface {
	Approve(ctx context.Context, nebulaID string) error
	Reject(ctx context.Context, nebulaID, reason string) error
}

// GitHubBadger optionally resolves PR status for badges. Nil-safe: when nil, the
// cockpit renders without live PR badges.
type GitHubBadger interface {
	PRStatus(ctx context.Context, repo string, number int) (string, error)
}

// PageRenderer renders the fleet dashboard HTML into the response writer.
// Injecting this as a function breaks the import cycle between package cockpit
// and its child package views (which imports cockpit for its data types).
// The canonical implementation is views.RenderPage, wired by the cmd layer.
// If nil, New substitutes a no-op that writes an empty 200.
type PageRenderer func(ctx context.Context, w http.ResponseWriter, f Fleet) error

// RunRenderer renders a single RunCard fragment into an io.Writer.
// Injecting this as a function avoids the import cycle with package views.
// The canonical implementation is views.RunCardView(rc).Render(ctx, w),
// wired by the cmd layer. If nil, New substitutes a no-op.
type RunRenderer func(ctx context.Context, w io.Writer, rc RunCard) error

// RunDetailRenderer renders the run-detail page into the response writer.
// Injecting this as a function breaks the import cycle between package cockpit
// and its child package views (which imports cockpit for its data types).
// The canonical implementation is views.RunDetailPage(d).Render(ctx, w),
// wired by the cmd layer. If nil, New substitutes a no-op that writes an
// empty 200.
type RunDetailRenderer func(ctx context.Context, w http.ResponseWriter, d RunDetail) error

// NebulaDetailRenderer renders the nebula-detail page into the response writer.
// Injecting this as a function breaks the import cycle between package cockpit
// and its child package views (which imports cockpit for its data types).
// The canonical implementation is views.NebulaDetailPage(d).Render(ctx, w),
// wired by the cmd layer. If nil, New substitutes a no-op that writes an
// empty 200.
type NebulaDetailRenderer func(ctx context.Context, w http.ResponseWriter, d NebulaDetail) error

// RunTailRenderer renders the live-stdout-tail fragment (a Datastar-mergeable
// <pre> with id "run-tail") into the response writer. Injected as a function to
// preserve the views→cockpit one-directional import. The canonical
// implementation is views.RunTailFragment(lines).Render(ctx, w), wired by the
// cmd layer. If nil, New substitutes a no-op that writes an empty 200.
type RunTailRenderer func(ctx context.Context, w http.ResponseWriter, lines string) error

// Opts holds the dependencies required to construct a Server.
type Opts struct {
	DB                 *sql.DB
	Runtime            RuntimeActions
	Notifier           *Notifier
	GitHub             GitHubBadger
	Token              string
	Assets             fs.FS
	RenderPage         PageRenderer
	RenderRun          RunRenderer
	RenderRunDetail    RunDetailRenderer
	RenderNebulaDetail NebulaDetailRenderer
	RenderRunTail      RunTailRenderer
	// TailDir is the directory holding per-run stdout tail logs (<run-id>.log),
	// the same directory the runtime tees into. Empty disables the tail endpoint
	// (it returns an empty fragment).
	TailDir string
	Logf    func(string, ...any)
}

// Server is the cockpit HTTP server. It serves the fleet dashboard and
// provides SSE, approve, and reject endpoints.
type Server struct {
	db                 *sql.DB
	rt                 RuntimeActions
	notifier           *Notifier
	github             GitHubBadger
	token              string
	assets             fs.FS
	renderPage         PageRenderer
	renderRun          RunRenderer
	renderRunDetail    RunDetailRenderer
	renderNebulaDetail NebulaDetailRenderer
	renderRunTail      RunTailRenderer
	tailDir            string
	logf               func(string, ...any)
}

// New constructs a Server from the given Opts. DB and Token are required.
// If Notifier is nil a default one is created. If Logf is nil a no-op is used.
func New(o Opts) (*Server, error) {
	if o.DB == nil {
		return nil, fmt.Errorf("cockpit: DB required")
	}
	if o.Token == "" {
		return nil, fmt.Errorf("cockpit: token required")
	}
	if o.Notifier == nil {
		o.Notifier = NewNotifier(128)
	}
	lf := o.Logf
	if lf == nil {
		lf = func(string, ...any) {}
	}
	rp := o.RenderPage
	if rp == nil {
		rp = func(_ context.Context, w http.ResponseWriter, _ Fleet) error {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, err := fmt.Fprint(w, "<html><body>cockpit (no renderer configured)</body></html>")
			return err
		}
	}
	rr := o.RenderRun
	if rr == nil {
		rr = func(_ context.Context, _ io.Writer, _ RunCard) error { return nil }
	}
	rrd := o.RenderRunDetail
	if rrd == nil {
		rrd = func(_ context.Context, w http.ResponseWriter, _ RunDetail) error {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, err := fmt.Fprint(w, "<html><body>cockpit (no run-detail renderer configured)</body></html>")
			return err
		}
	}
	rnd := o.RenderNebulaDetail
	if rnd == nil {
		rnd = func(_ context.Context, w http.ResponseWriter, _ NebulaDetail) error {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, err := fmt.Fprint(w, "<html><body>cockpit (no nebula-detail renderer configured)</body></html>")
			return err
		}
	}
	rt := o.RenderRunTail
	if rt == nil {
		rt = func(_ context.Context, w http.ResponseWriter, _ string) error {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			return nil
		}
	}
	return &Server{
		db:                 o.DB,
		rt:                 o.Runtime,
		notifier:           o.Notifier,
		github:             o.GitHub,
		token:              o.Token,
		assets:             o.Assets,
		renderPage:         rp,
		renderRun:          rr,
		renderRunDetail:    rrd,
		renderNebulaDetail: rnd,
		renderRunTail:      rt,
		tailDir:            o.TailDir,
		logf:               lf,
	}, nil
}

// Run starts the HTTP server on addr and blocks until ctx is canceled or the
// server encounters a non-close error.
func (s *Server) Run(ctx context.Context, addr string) error {
	srv := &http.Server{Addr: addr, Handler: s.Routes()}
	go func() { <-ctx.Done(); _ = srv.Close() }()
	fmt.Fprintf(os.Stderr, "cockpit: listening on http://%s\n", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
