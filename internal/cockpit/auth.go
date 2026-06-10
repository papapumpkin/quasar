package cockpit

import (
	"crypto/subtle"
	"fmt"
	"net/http"
)

// cookieName is the name of the session cookie carrying the bearer token.
const cookieName = "quasar_cockpit"

// requireAuth is an HTTP middleware that gate-keeps next behind the bearer
// token stored in the cookieName cookie. Token comparison is constant-time to
// prevent timing attacks.
//
// SSE endpoints (path == "/sse" or the Datastar request header set) return 401
// rather than redirecting, because EventSource cannot follow a redirect with
// credentials.
func requireAuth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(token)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		// Event streams cannot follow redirects — return 401 instead.
		if r.URL.Path == "/sse" || r.Header.Get("datastar-request") != "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}

// loginHandler returns an http.Handler that serves the login page (GET) and
// processes token submission (POST).
//
// On a matching POST the handler sets an HttpOnly, SameSite=Lax session cookie
// and redirects to "/". A wrong token writes 401.
func loginHandler(token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			submitted := r.FormValue("token")
			if subtle.ConstantTimeCompare([]byte(submitted), []byte(token)) == 1 {
				http.SetCookie(w, &http.Cookie{
					Name:     cookieName,
					Value:    token,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
				})
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		serveLoginPage(w)
	})
}

// serveLoginPage writes a minimal self-contained HTML login page.
func serveLoginPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Quasar Cockpit — Login</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{background:#0d1117;color:#e6edf3;font-family:system-ui,sans-serif;
  display:flex;align-items:center;justify-content:center;min-height:100vh}
.card{background:#161b22;border:1px solid #30363d;border-radius:8px;
  padding:2rem;width:100%;max-width:360px}
h1{font-size:1.1rem;font-weight:600;margin-bottom:1.5rem;color:#f0f6fc}
label{display:block;font-size:.85rem;color:#8b949e;margin-bottom:.4rem}
input{width:100%;padding:.6rem .75rem;background:#0d1117;border:1px solid #30363d;
  border-radius:6px;color:#e6edf3;font-size:.95rem;outline:none}
input:focus{border-color:#58a6ff}
button{margin-top:1rem;width:100%;padding:.65rem;background:#238636;
  border:none;border-radius:6px;color:#fff;font-size:.95rem;
  font-weight:600;cursor:pointer}
button:hover{background:#2ea043}
</style>
</head>
<body>
<div class="card">
  <h1>Quasar Cockpit</h1>
  <form method="POST" action="/login">
    <label for="token">Access token</label>
    <input id="token" name="token" type="password" autocomplete="current-password" autofocus required>
    <button type="submit">Sign in</button>
  </form>
</div>
</body>
</html>`)
}
