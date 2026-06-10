package cmd

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/cockpit"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// TestServeCockpitEndToEnd exercises the serve wiring path: a temp fabric DB
// with a seeded awaiting nebula, the token read from a temp HOME, the cockpit
// server built and served on a loopback port, and the full login -> board
// flow (GET /login, POST token for the cookie, GET / with the cookie).
func TestServeCockpitEndToEnd(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// Seed a temp HOME with the cockpit token so readCockpitToken finds it.
	home := t.TempDir()
	t.Setenv("HOME", home)
	tokenDir := filepath.Join(home, ".quasar")
	if err := os.MkdirAll(tokenDir, 0o700); err != nil {
		t.Fatalf("mkdir token dir: %v", err)
	}
	const token = "deadbeefcafebabe"
	if err := os.WriteFile(filepath.Join(tokenDir, "cockpit-token"), []byte(token), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	// readCockpitToken must return the seeded token.
	got, err := readCockpitToken()
	if err != nil {
		t.Fatalf("readCockpitToken: %v", err)
	}
	if got != token {
		t.Fatalf("token = %q, want %q", got, token)
	}

	// Temp fabric DB with one registered repo and one awaiting nebula.
	fab, err := fabric.NewSQLiteFabric(ctx, filepath.Join(dir, "fabric.db"))
	if err != nil {
		t.Fatalf("NewSQLiteFabric: %v", err)
	}
	t.Cleanup(func() { _ = fab.Close() })

	db := fab.DB()
	repoPath := "/repos/papapumpkin/quasar"
	now := time.Now().Unix()
	if _, err := db.ExecContext(ctx,
		"INSERT INTO repos (path, name, status, added_at, updated_at, last_seen_at) VALUES (?, ?, 'active', ?, ?, ?)",
		repoPath, "quasar", now, now, now); err != nil {
		t.Fatalf("insert repo: %v", err)
	}
	blobs, err := blobstore.New(filepath.Join(dir, "blobs"), db)
	if err != nil {
		t.Fatalf("blobstore.New: %v", err)
	}
	if _, err := fabric.NewNebulaStore(db, blobs).Insert(ctx, fabric.NebulaRow{
		RepoPath:   repoPath,
		Name:       "Fix flaky sensor poll dedup",
		SourceName: "github",
		SourceID:   "papapumpkin/quasar#42",
		Status:     "awaiting_approval",
	}); err != nil {
		t.Fatalf("insert awaiting nebula: %v", err)
	}

	// Build the cockpit server through the same wiring serve.go uses.
	notifier := cockpit.NewNotifier(16)
	server, err := buildCockpitServer(fab, notifier)
	if err != nil {
		t.Fatalf("buildCockpitServer: %v", err)
	}

	// Serve on a loopback port. Run blocks, so launch it and tear down via ctx.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // hand the port to server.Run; small race window is acceptable in-test.

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	srvErr := make(chan error, 1)
	go func() { srvErr <- server.Run(runCtx, addr) }()

	base := "http://" + addr
	waitForServer(t, base+"/login")

	// Client that does NOT auto-follow redirects so we can inspect the cookie.
	jar := &cookieCapture{}
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	// GET /login -> 200 login page.
	resp, err := client.Get(base + "/login")
	if err != nil {
		t.Fatalf("GET /login: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /login status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// POST token -> 303 + Set-Cookie.
	resp, err = client.PostForm(base+"/login", url.Values{"token": {token}})
	if err != nil {
		t.Fatalf("POST /login: %v", err)
	}
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /login status = %d, want 303", resp.StatusCode)
	}
	jar.capture(resp.Cookies())
	_ = resp.Body.Close()
	if jar.value(cockpit.CookieName) == "" {
		t.Fatal("POST /login did not set the session cookie")
	}

	// GET / with the cookie -> 200 board containing a lane header.
	req, _ := http.NewRequest(http.MethodGet, base+"/", nil)
	req.AddCookie(&http.Cookie{Name: cockpit.CookieName, Value: jar.value(cockpit.CookieName)})
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d", resp.StatusCode)
	}
	body := readAll(t, resp)
	_ = resp.Body.Close()
	if !strings.Contains(body, "Awaiting") {
		t.Errorf("board missing a lane header; got %d bytes", len(body))
	}

	cancel()
	select {
	case err := <-srvErr:
		if err != nil {
			t.Fatalf("server.Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server.Run did not return after cancel")
	}
}

// TestReadCockpitTokenMissing confirms the actionable error when no token file
// exists.
func TestReadCockpitTokenMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := readCockpitToken()
	if err == nil {
		t.Fatal("expected error for missing token")
	}
	if !strings.Contains(err.Error(), "quasar cockpit token") {
		t.Errorf("error should hint at the token command; got %v", err)
	}
}

// waitForServer polls url until it responds or the deadline elapses.
func waitForServer(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec // loopback test URL
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s never became ready", url)
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return b.String()
}

// cookieCapture is a tiny helper to stash Set-Cookie values across requests
// without pulling in net/http/cookiejar's URL semantics.
type cookieCapture struct{ cookies []*http.Cookie }

func (c *cookieCapture) capture(cs []*http.Cookie) { c.cookies = append(c.cookies, cs...) }

func (c *cookieCapture) value(name string) string {
	for _, ck := range c.cookies {
		if ck.Name == name {
			return ck.Value
		}
	}
	return ""
}
