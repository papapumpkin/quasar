package cockpit

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header. It returns the token and true when the header is well-formed, or
// ("", false) otherwise. The scheme match is case-insensitive per RFC 7235.
func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	const prefix = "bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	return strings.TrimSpace(h[len(prefix):]), true
}

// requireToken wraps next with bearer-token authentication. Requests without a
// well-formed Authorization header, or whose token does not match the server's
// token, receive 401. The comparison is constant-time to avoid leaking the
// token via timing. An empty server token rejects every request (fail closed).
func requireToken(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := bearerToken(r)
		if !ok || token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}
