package cockpit

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireAuthRejectsMissing(t *testing.T) {
	h := requireAuth("secret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want redirect (303), got %d", w.Code)
	}
}

func TestRequireAuthAcceptsCookie(t *testing.T) {
	h := requireAuth("secret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: "secret"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestRequireAuthRejectsWrongToken(t *testing.T) {
	h := requireAuth("secret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: "wrong"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("want redirect for wrong token, got %d", w.Code)
	}
}

func TestRequireAuthSSEGets401(t *testing.T) {
	h := requireAuth("secret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	r := httptest.NewRequest("GET", "/sse", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("want 401 for /sse (no redirect for event streams), got %d", w.Code)
	}
}

func TestLoginSetsCookieOnValidToken(t *testing.T) {
	h := loginHandler("secret")
	r := httptest.NewRequest("POST", "/login", nil)
	r.Form = map[string][]string{"token": {"secret"}}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("valid login should redirect, got %d", w.Code)
	}
	found := false
	for _, c := range w.Result().Cookies() {
		if c.Name == cookieName && c.Value == "secret" && c.HttpOnly {
			found = true
		}
	}
	if !found {
		t.Fatal("valid login must set an HttpOnly cookie with the token")
	}
}

func TestLoginRejectsBadToken(t *testing.T) {
	h := loginHandler("secret")
	r := httptest.NewRequest("POST", "/login", nil)
	r.Form = map[string][]string{"token": {"nope"}}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("bad token should be 401, got %d", w.Code)
	}
}
