package cockpit

import (
	"net/http"
)

// Routes returns the cockpit's HTTP handler tree using Go 1.22 method+pattern
// routing. Public routes (login, assets) are accessible without a session
// cookie; all others are gate-kept by requireAuth.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Public routes.
	lh := loginHandler(s.token)
	mux.Handle("GET /login", lh)
	mux.Handle("POST /login", lh)

	if s.assets != nil {
		mux.Handle("GET /assets/",
			http.StripPrefix("/assets/", http.FileServer(http.FS(s.assets))))
	}

	// Authenticated routes.
	mux.Handle("GET /{$}", requireAuth(s.token, http.HandlerFunc(s.handleFleet)))
	mux.Handle("GET /sse", requireAuth(s.token, http.HandlerFunc(s.handleSSE)))
	mux.Handle("POST /nebulas/{id}/approve",
		requireAuth(s.token, http.HandlerFunc(s.handleApprove)))
	mux.Handle("POST /nebulas/{id}/reject",
		requireAuth(s.token, http.HandlerFunc(s.handleReject)))

	return mux
}
