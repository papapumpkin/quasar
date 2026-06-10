# Quasar Cockpit — Live Fleet Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a browser-based, real-time "Mission Control" fleet dashboard for Quasar — server-rendered Go, live updates over SSE, one-click approve/reject — as a single binary, off by default.

**Architecture:** A new `internal/cockpit/` package runs an HTTP server (stdlib `net/http`) that renders [`templ`](https://templ.guide) views and pushes DOM-fragment patches over Server-Sent Events using [Datastar](https://data-star.dev). A `Notifier` fans runtime state-change events out to connected operators. Auth is a bearer token in a cookie. CSS is Tailwind compiled by its standalone CLI; all assets are embedded behind a `cockpit` build tag and a config flag.

**Tech Stack:** Go (stdlib net/http, `html/template`-free), `github.com/a-h/templ`, Tailwind standalone CLI, Datastar (vendored JS), `modernc.org/sqlite` (already in repo via fabric), SSE.

**Visual reference (exact target):** `docs/superpowers/specs/2026-06-10-quasar-cockpit-mockup.html` — open in a browser. The templ views reproduce this markup and Tailwind classes. The spec is `docs/superpowers/specs/2026-06-10-quasar-cockpit-design.md`.

**Core shared types** (defined in Task 3, referenced everywhere):

```go
// internal/cockpit/notifier.go
type Event struct {
    Topic string         // "fleet" | "runs"
    Type  string         // "nebula_status_changed" | "step_started" | "step_completed" | "resync"
    Data  map[string]any // event-specific payload
}
```

```go
// internal/cockpit/server.go
type Opts struct {
    DB       *sql.DB
    Runtime  RuntimeActions   // interface, Task 10
    Notifier *Notifier
    GitHub   GitHubBadger     // interface, nil-safe, Task 8
    Token    string
    Assets   fs.FS            // embedded; empty without build tag
    Logf     func(string, ...any)
}
```

---

## File Structure

```
internal/cockpit/
  notifier.go / notifier_test.go     # Event fan-out (Task 3)
  embed.go / embed_disabled.go       # build-tag asset FS (Task 4)
  config.go                          # CockpitConfig + load (Task 2)
  auth.go / auth_test.go             # token cookie middleware + login (Task 5)
  server.go                          # Server, Opts, New, Run (Task 6)
  routes.go                          # ServeMux wiring (Task 6)
  fleet.go / fleet_test.go           # read-side queries → view models (Task 7)
  sse.go / sse_test.go               # SSE handler + Datastar patch format (Task 9)
  handlers.go / handlers_test.go     # GET / and POST actions (Task 8, 10)
  views/                             # templ
    page.templ shell.templ rack.templ lane.templ card.templ run.templ badges.templ
  assets/
    cockpit.css                      # Tailwind output (generated, committed)
    datastar.js                      # vendored (committed)
    input.css                        # Tailwind source (committed)
  tailwind.config.js                 # Tailwind config (committed)
cmd/
  serve.go                           # `quasar serve [--cockpit]` (Task 11)
  cockpit_token.go                   # `quasar cockpit token` (Task 5)
scripts/
  build-cockpit.sh                   # templ generate + tailwind + go build -tags cockpit (Task 1)
docs/cockpit.md                      # toolchain + ops note (Task 13)
```

Reuse: `internal/tui/fleet` defines the lane/card data shape but is coupled to Bubble Tea — Task 7 writes cockpit's own thin read queries (the SQL is small) rather than importing it, to avoid pulling `tea` into the web package. GitHub badge lookups go through a local `GitHubBadger` interface, nil-safe.

---

## Task 1: Toolchain + vendored assets

**Files:**
- Create: `scripts/build-cockpit.sh`, `internal/cockpit/assets/input.css`, `internal/cockpit/assets/datastar.js`, `internal/cockpit/tailwind.config.js`
- Modify: `go.mod` (add `github.com/a-h/templ`)

- [ ] **Step 1: Install the templ generator + add the runtime dep**

Run:
```bash
go install github.com/a-h/templ/cmd/templ@v0.2.793
go get github.com/a-h/templ@v0.2.793
```
Expected: `templ version` prints; `go.mod` gains the require line.

- [ ] **Step 2: Vendor Datastar (pinned)**

Run:
```bash
curl -fsSL https://cdn.jsdelivr.net/npm/@starfederation/datastar@1.0.0-beta.11/dist/datastar.js \
  -o internal/cockpit/assets/datastar.js
test -s internal/cockpit/assets/datastar.js && echo OK
```
Expected: `OK` (a ~40KB JS file). If the version 404s, pick the latest `1.0.0-beta.*` from the jsdelivr listing and pin it here.

- [ ] **Step 3: Download the Tailwind standalone CLI (not committed; used at build time)**

Run (macOS arm64 shown; pick the asset for the build host):
```bash
curl -fsSL https://github.com/tailwindlabs/tailwindcss/releases/download/v3.4.17/tailwindcss-macos-arm64 \
  -o /tmp/tailwindcss && chmod +x /tmp/tailwindcss && /tmp/tailwindcss --help | head -1
```
Expected: prints Tailwind usage. (`build-cockpit.sh` resolves the binary by `$TAILWIND_BIN` or PATH.)

- [ ] **Step 4: Tailwind source + config**

`internal/cockpit/assets/input.css`:
```css
@tailwind base;
@tailwind components;
@tailwind utilities;
```
`internal/cockpit/tailwind.config.js`:
```js
module.exports = {
  content: ["./internal/cockpit/views/**/*.templ"],
  theme: { extend: {
    fontFamily: { mono: ["ui-monospace","SFMono-Regular","Menlo","monospace"] },
    colors: { cyan:{DEFAULT:"#22d3ee"}, green:{DEFAULT:"#34d399"}, amber:{DEFAULT:"#fbbf24"},
              red:{DEFAULT:"#f87171"}, violet:{DEFAULT:"#a78bfa"} },
  } },
};
```

- [ ] **Step 5: build-cockpit.sh**

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
TAILWIND="${TAILWIND_BIN:-tailwindcss}"
echo "templ generate…" >&2 ; templ generate ./internal/cockpit/...
echo "tailwind…" >&2 ; "$TAILWIND" -c internal/cockpit/tailwind.config.js \
  -i internal/cockpit/assets/input.css -o internal/cockpit/assets/cockpit.css --minify
echo "go build -tags cockpit…" >&2 ; go build -tags cockpit -o quasar .
echo "built ./quasar with cockpit" >&2
```
`chmod +x scripts/build-cockpit.sh`. (cockpit.css is generated in later tasks once views exist; create an empty placeholder now: `echo "/* generated by build-cockpit.sh */" > internal/cockpit/assets/cockpit.css`.)

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum scripts/build-cockpit.sh internal/cockpit/assets/ internal/cockpit/tailwind.config.js
git commit -m "cockpit: vendor datastar + tailwind toolchain and build script"
```

---

## Task 2: Cockpit config

**Files:** Create `internal/cockpit/config.go`; Test `internal/cockpit/config_test.go`. Reference `internal/config/config.go` for the Viper pattern.

- [ ] **Step 1: Failing test** — `config_test.go`:
```go
func TestCockpitConfigDefaults(t *testing.T) {
    c := DefaultConfig()
    if c.Enabled { t.Error("cockpit must default to disabled") }
    if c.Addr != "127.0.0.1:7330" { t.Errorf("addr = %q", c.Addr) }
}
```
- [ ] **Step 2: Run** `go test ./internal/cockpit/ -run TestCockpitConfig` → FAIL (undefined).
- [ ] **Step 3: Implement** `config.go`:
```go
package cockpit

type Config struct {
    Enabled bool   `mapstructure:"enabled"`
    Addr    string `mapstructure:"addr"`
}
func DefaultConfig() Config { return Config{Enabled: false, Addr: "127.0.0.1:7330"} }
```
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `git commit -am "cockpit: config (disabled by default, addr 127.0.0.1:7330)"`

(Wiring `[cockpit]` into the main Viper load happens in Task 11 where `cmd/serve.go` reads it.)

---

## Task 3: Notifier (the live-update crux)

**Files:** Create `internal/cockpit/notifier.go`, `internal/cockpit/notifier_test.go`.

Buffered per-subscriber broadcast. A slow subscriber whose buffer is full drops the oldest queued event and is flagged to receive a single `resync` event (so the client re-fetches the whole board) instead of blocking publishers.

- [ ] **Step 1: Failing tests** — `notifier_test.go`:
```go
func TestNotifierDelivers(t *testing.T) {
    n := NewNotifier(8)
    _, ch, cancel := n.Subscribe([]string{"fleet"})
    defer cancel()
    n.Publish(Event{Topic: "fleet", Type: "nebula_status_changed"})
    select {
    case e := <-ch:
        if e.Type != "nebula_status_changed" { t.Fatalf("got %q", e.Type) }
    case <-time.After(time.Second):
        t.Fatal("no event")
    }
}

func TestNotifierTopicFilter(t *testing.T) {
    n := NewNotifier(8)
    _, ch, cancel := n.Subscribe([]string{"runs"})
    defer cancel()
    n.Publish(Event{Topic: "fleet", Type: "x"}) // not subscribed
    select {
    case <-ch: t.Fatal("should not receive other topic")
    case <-time.After(50 * time.Millisecond):
    }
}

func TestNotifierSlowSubscriberResync(t *testing.T) {
    n := NewNotifier(2) // tiny buffer
    _, ch, cancel := n.Subscribe([]string{"runs"})
    defer cancel()
    for i := 0; i < 10; i++ { n.Publish(Event{Topic: "runs", Type: "step_completed"}) }
    // Drain; a resync must appear because the buffer overflowed.
    saw := map[string]bool{}
    for i := 0; i < 3; i++ {
        select { case e := <-ch: saw[e.Type] = true; case <-time.After(100*time.Millisecond): }
    }
    if !saw["resync"] { t.Fatal("expected a resync hint after overflow") }
}

func TestNotifierCancelRemoves(t *testing.T) {
    n := NewNotifier(8)
    _, _, cancel := n.Subscribe([]string{"fleet"})
    cancel()
    n.Publish(Event{Topic: "fleet", Type: "x"}) // must not panic / block
    if n.count() != 0 { t.Fatal("subscriber not removed") }
}
```
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** `notifier.go`:
```go
package cockpit

import (
    "strconv"
    "sync"
)

type Event struct {
    Topic string
    Type  string
    Data  map[string]any
}

type subscriber struct {
    topics map[string]bool
    ch     chan Event
    needResync bool
}

type Notifier struct {
    mu   sync.Mutex
    subs map[string]*subscriber
    buf  int
    seq  int
}

func NewNotifier(buffer int) *Notifier {
    if buffer < 1 { buffer = 64 }
    return &Notifier{subs: map[string]*subscriber{}, buf: buffer}
}

func (n *Notifier) Subscribe(topics []string) (id string, ch <-chan Event, cancel func()) {
    n.mu.Lock(); defer n.mu.Unlock()
    n.seq++
    id = strconv.Itoa(n.seq)
    tset := make(map[string]bool, len(topics))
    for _, t := range topics { tset[t] = true }
    s := &subscriber{topics: tset, ch: make(chan Event, n.buf)}
    n.subs[id] = s
    return id, s.ch, func() {
        n.mu.Lock(); defer n.mu.Unlock()
        if _, ok := n.subs[id]; ok { delete(n.subs, id); close(s.ch) }
    }
}

func (n *Notifier) Publish(e Event) {
    n.mu.Lock(); defer n.mu.Unlock()
    for _, s := range n.subs {
        if !s.topics[e.Topic] { continue }
        n.deliver(s, e)
    }
}

// deliver is non-blocking. On a full buffer it drops the oldest event and marks
// the subscriber for a single resync, which is sent ahead of the new event.
func (n *Notifier) deliver(s *subscriber, e Event) {
    if s.needResync { e = Event{Topic: e.Topic, Type: "resync"}; s.needResync = false }
    select {
    case s.ch <- e:
    default:
        select { case <-s.ch: default: } // drop oldest
        s.needResync = true
        select { case s.ch <- Event{Topic: e.Topic, Type: "resync"}: default: }
    }
}

func (n *Notifier) count() int { n.mu.Lock(); defer n.mu.Unlock(); return len(n.subs) }
```
- [ ] **Step 4: Run** `go test ./internal/cockpit/ -run TestNotifier -race` → PASS.
- [ ] **Step 5: Commit** `git commit -am "cockpit: Notifier with buffered fan-out + drop-oldest resync"`

---

## Task 4: Embed + feature-flag build tags

**Files:** Create `internal/cockpit/embed.go`, `internal/cockpit/embed_disabled.go`.

- [ ] **Step 1: Implement `embed.go`**:
```go
//go:build cockpit

package cockpit

import "embed"

//go:embed assets/cockpit.css assets/datastar.js
var assetsFS embed.FS

// Assets returns the embedded asset filesystem (css + datastar js).
func Assets() embed.FS { return assetsFS }
```
- [ ] **Step 2: Implement `embed_disabled.go`**:
```go
//go:build !cockpit

package cockpit

import "io/fs"

// Assets returns an empty filesystem; the cockpit build tag embeds the real one.
func Assets() fs.FS { return emptyFS{} }

type emptyFS struct{}
func (emptyFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }
```
- [ ] **Step 3: Verify both build tags compile**

Run: `go build ./internal/cockpit/ && go build -tags cockpit ./internal/cockpit/`
Expected: both succeed (cockpit.css + datastar.js exist from Task 1).
- [ ] **Step 4: Commit** `git commit -am "cockpit: build-tag gated asset embedding"`

Note: `Assets()` return types differ by tag (`embed.FS` vs `fs.FS`), both satisfy `fs.FS`. Callers (Task 6) store it as `fs.FS`.

---

## Task 5: Auth — token cookie + login + `quasar cockpit token`

**Files:** Create `internal/cockpit/auth.go`, `internal/cockpit/auth_test.go`, `cmd/cockpit_token.go`.

- [ ] **Step 1: Failing tests** — `auth_test.go`:
```go
func TestRequireAuthRejectsMissing(t *testing.T) {
    h := requireAuth("secret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request){ w.WriteHeader(200) }))
    r := httptest.NewRequest("GET", "/", nil)
    w := httptest.NewRecorder(); h.ServeHTTP(w, r)
    if w.Code != http.StatusSeeOther { t.Fatalf("want redirect, got %d", w.Code) }
}
func TestRequireAuthAcceptsCookie(t *testing.T) {
    h := requireAuth("secret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request){ w.WriteHeader(200) }))
    r := httptest.NewRequest("GET", "/", nil)
    r.AddCookie(&http.Cookie{Name: cookieName, Value: "secret"})
    w := httptest.NewRecorder(); h.ServeHTTP(w, r)
    if w.Code != 200 { t.Fatalf("want 200, got %d", w.Code) }
}
func TestRequireAuthFragmentGets401(t *testing.T) {
    h := requireAuth("secret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request){ w.WriteHeader(200) }))
    r := httptest.NewRequest("GET", "/sse", nil)
    r.Header.Set("datastar-request", "true") // datastar/SSE requests want 401 not redirect
    w := httptest.NewRecorder(); h.ServeHTTP(w, r)
    if w.Code != 401 { t.Fatalf("want 401, got %d", w.Code) }
}
```
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** `auth.go`:
```go
package cockpit

import (
    "crypto/subtle"
    "net/http"
)

const cookieName = "quasar_cockpit"

func requireAuth(token string, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        c, _ := r.Cookie(cookieName)
        if c != nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(token)) == 1 {
            next.ServeHTTP(w, r); return
        }
        if r.Header.Get("datastar-request") != "" || r.URL.Path == "/sse" {
            http.Error(w, "unauthorized", http.StatusUnauthorized); return
        }
        http.Redirect(w, r, "/login", http.StatusSeeOther)
    })
}

// loginHandler renders a token-entry page (GET) and sets the cookie (POST).
func loginHandler(token string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if r.Method == http.MethodPost {
            if subtle.ConstantTimeCompare([]byte(r.FormValue("token")), []byte(token)) == 1 {
                http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/",
                    HttpOnly: true, SameSite: http.SameSiteLaxMode})
                http.Redirect(w, r, "/", http.StatusSeeOther); return
            }
            w.WriteHeader(http.StatusUnauthorized)
        }
        loginPage(r.Method == http.MethodPost).Render(r.Context(), w) // templ, Task 7
    }
}
```
- [ ] **Step 4: Run** → PASS (stub `loginPage` returns a templ component; for the test it isn't reached). If the linker needs it, add a minimal `views/login.templ` now or defer compile until Task 7 by making `loginHandler` unused in the test build. Simplest: write `views/login.templ` in this task.
- [ ] **Step 5: `quasar cockpit token`** — `cmd/cockpit_token.go`:
```go
// Generates a 32-byte hex token at <data-dir>/cockpit-token (0600).
func runCockpitToken(cmd *cobra.Command, _ []string) error {
    b := make([]byte, 32)
    if _, err := rand.Read(b); err != nil { return err }
    tok := hex.EncodeToString(b)
    path := filepath.Join(dataDir(), "cockpit-token")
    if err := os.WriteFile(path, []byte(tok), 0o600); err != nil { return err }
    fmt.Fprintf(cmd.OutOrStdout(), "%s\n", path)
    return nil
}
```
(Reuse the existing data-dir helper; grep `XDG`/`dataDir` in `cmd/`/`internal/config`. Register the cobra command in `cmd/cockpit_token.go`'s `init`.)
- [ ] **Step 6: Commit** `git commit -am "cockpit: bearer-token cookie auth + quasar cockpit token"`

---

## Task 6: Server + routes skeleton

**Files:** Create `internal/cockpit/server.go`, `internal/cockpit/routes.go`.

- [ ] **Step 1: Implement `server.go`** (`Server`, `Opts`, `New`, `Run`):
```go
package cockpit

import ("context"; "database/sql"; "fmt"; "io/fs"; "net/http"; "os")

type Server struct {
    db *sql.DB; rt RuntimeActions; notifier *Notifier
    github GitHubBadger; token string; assets fs.FS
    logf func(string, ...any)
}

func New(o Opts) (*Server, error) {
    if o.DB == nil { return nil, fmt.Errorf("cockpit: DB required") }
    if o.Token == "" { return nil, fmt.Errorf("cockpit: token required") }
    if o.Notifier == nil { o.Notifier = NewNotifier(128) }
    lf := o.Logf; if lf == nil { lf = func(string, ...any){} }
    return &Server{db:o.DB, rt:o.Runtime, notifier:o.Notifier, github:o.GitHub,
        token:o.Token, assets:o.Assets, logf:lf}, nil
}

func (s *Server) Run(ctx context.Context, addr string) error {
    srv := &http.Server{Addr: addr, Handler: s.Routes()}
    go func(){ <-ctx.Done(); _ = srv.Close() }()
    fmt.Fprintf(os.Stderr, "cockpit: listening on http://%s\n", addr)
    if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed { return err }
    return nil
}
```
- [ ] **Step 2: Implement `routes.go`** (Go 1.22 ServeMux):
```go
func (s *Server) Routes() http.Handler {
    mux := http.NewServeMux()
    // public
    mux.HandleFunc("GET /login", loginHandler(s.token))
    mux.HandleFunc("POST /login", loginHandler(s.token))
    mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(s.assets))))
    // authed
    authed := http.NewServeMux()
    authed.HandleFunc("GET /{$}", s.handleFleet)        // Task 8
    authed.HandleFunc("GET /sse", s.handleSSE)          // Task 9
    authed.HandleFunc("POST /nebulas/{id}/approve", s.handleApprove) // Task 10
    authed.HandleFunc("POST /nebulas/{id}/reject", s.handleReject)   // Task 10
    mux.Handle("/", requireAuth(s.token, authed))
    return mux
}
```
- [ ] **Step 3: Compile** `go build -tags cockpit ./internal/cockpit/` (handlers are stubs until their tasks; add temporary `func (s *Server) handleFleet(...)` etc. returning 501 so it compiles, replaced in later tasks). Expected: compiles.
- [ ] **Step 4: Commit** `git commit -am "cockpit: server + ServeMux routing skeleton"`

---

## Task 7: templ views + fleet read model

**Files:** Create `internal/cockpit/views/*.templ`, `internal/cockpit/fleet.go`, `internal/cockpit/fleet_test.go`. The committed mockup `docs/superpowers/specs/2026-06-10-quasar-cockpit-mockup.html` is the exact markup/Tailwind target — port its structure into templ components.

- [ ] **Step 1: Read model + failing test** — `fleet_test.go` (uses an in-memory fabric DB seeded with a repo, an awaiting nebula, a running run, a recent merged nebula; assert the view model groups by repo into three lanes):
```go
func TestLoadFleetGroupsByRepoAndLane(t *testing.T) {
    db := newTestFabric(t) // helper: open modernc sqlite + run migrations + seed
    f, err := LoadFleet(context.Background(), db)
    if err != nil { t.Fatal(err) }
    if len(f.Repos) != 1 { t.Fatalf("repos = %d", len(f.Repos)) }
    r := f.Repos[0]
    if len(r.Awaiting) != 1 || len(r.InFlight) != 1 || len(r.Recent) != 1 {
        t.Fatalf("lanes a=%d f=%d r=%d", len(r.Awaiting), len(r.InFlight), len(r.Recent))
    }
}
```
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement `fleet.go`** — view-model structs (`Fleet`, `RepoLane`, `NebulaCard`, `RunCard`) and `LoadFleet(ctx, db)` running the same three queries the TUI's `internal/tui/fleet/fleet.go` uses (`listRepos`, `awaiting`, `inFlight` JOIN, `recent`). Copy the SQL from there; do NOT import the package (avoids Bubble Tea). Keep structs UI-shaped (status string, PR number, issue URL, current_node, step flow, cost, cycle).
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Write templ components** mirroring the mockup, one file per unit:
  - `shell.templ` — `<html>`, `<head>` linking `/assets/cockpit.css` and `/assets/datastar.js`, `<body data-on-load="@get('/sse')">` (Datastar opens the SSE stream on load).
  - `page.templ` — topbar (fleet spend, running count, presence) + the board (range `Fleet.Repos` → `rack`).
  - `rack.templ`, `lane.templ`, `card.templ`, `run.templ`, `badges.templ` — port the mockup markup; each run card has `id={ "run-" + run.ID }` so SSE patches target it; each card's approve button is `data-on-click={ "@post('/nebulas/"+card.ID+"/approve')" }`.
  - `login.templ` — token input form POSTing to `/login`.

  Wrap each run/card fragment in an element with a stable `id` so Datastar's fragment merge replaces it in place.
- [ ] **Step 6: Generate + build** `templ generate ./internal/cockpit/... && go build -tags cockpit ./internal/cockpit/` → compiles; commit generated `*_templ.go`.
- [ ] **Step 7: Commit** `git commit -am "cockpit: fleet read model + Mission Control templ views"`

---

## Task 8: Fleet page handler

**Files:** Modify `internal/cockpit/handlers.go`; Test `internal/cockpit/handlers_test.go`.

- [ ] **Step 1: Failing test** — render `GET /` over an authed `httptest` server with a seeded DB; assert the body contains a seeded nebula title and a lane header.
```go
func TestHandleFleetRendersBoard(t *testing.T) {
    s := newTestServer(t)        // helper: New(Opts{DB: seeded, Token:"t", Assets: emptyFS{}})
    r := httptest.NewRequest("GET", "/", nil)
    r.AddCookie(&http.Cookie{Name: cookieName, Value: "t"})
    w := httptest.NewRecorder(); s.Routes().ServeHTTP(w, r)
    if w.Code != 200 { t.Fatalf("code %d", w.Code) }
    if !strings.Contains(w.Body.String(), "Awaiting approval") { t.Fatal("no lane header") }
}
```
- [ ] **Step 2: Run** → FAIL (stub returns 501).
- [ ] **Step 3: Implement `handleFleet`**: `LoadFleet` → enrich with GitHub badges if `s.github != nil` (best-effort; ignore errors) → `views.Page(fleet).Render(r.Context(), w)`.
- [ ] **Step 4: GitHubBadger interface** (`fleet.go` or `server.go`):
```go
type GitHubBadger interface {
    PRStatus(ctx context.Context, repo string, number int) (string, error) // "open"|"draft"|"merged"|"closed"
}
```
A thin adapter over `internal/sensors/github` lands in Task 11; tests pass `nil`.
- [ ] **Step 5: Run** → PASS. **Commit** `git commit -am "cockpit: fleet page handler renders the live board"`

---

## Task 9: SSE handler (Datastar patch contract)

**Files:** Create `internal/cockpit/sse.go`, `internal/cockpit/sse_test.go`.

Datastar consumes SSE events named `datastar-merge-fragments` whose `data:` lines carry `fragments <html>`. The handler subscribes to the Notifier and, per event, renders the affected templ fragment and writes it as a merge.

- [ ] **Step 1: Failing test** — connect to `/sse` with a cancelable context; publish a `step_completed`; assert the response stream contains `event: datastar-merge-fragments` and the run's element id.
```go
func TestSSEEmitsFragmentOnPublish(t *testing.T) {
    s := newTestServer(t)
    pr, pw := io.Pipe()
    req := httptest.NewRequest("GET", "/sse", nil).WithContext(...)
    req.AddCookie(&http.Cookie{Name: cookieName, Value: "t"})
    // serve in a goroutine writing to a flushable recorder; publish; read first frames
    // assert frames contain "datastar-merge-fragments"
}
```
(Use a small `flushRecorder` implementing `http.Flusher`; helper in the test file.)
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement `sse.go`**:
```go
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
    f, ok := w.(http.Flusher); if !ok { http.Error(w, "no flush", 500); return }
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    _, ch, cancel := s.notifier.Subscribe([]string{"fleet", "runs"})
    defer cancel()
    ticker := time.NewTicker(20 * time.Second); defer ticker.Stop() // heartbeat
    for {
        select {
        case <-r.Context().Done(): return
        case <-ticker.C: fmt.Fprint(w, ": ping\n\n"); f.Flush()
        case e, ok := <-ch:
            if !ok { return }
            frag, err := s.renderEvent(r.Context(), e) // returns fragment HTML or "" 
            if err != nil || frag == "" {
                if e.Type == "resync" { writeRedirect(w); f.Flush() } // datastar-execute-script location.reload, or merge whole board
                continue
            }
            fmt.Fprint(w, "event: datastar-merge-fragments\n")
            for _, line := range strings.Split("fragments "+frag, "\n") { fmt.Fprintf(w, "data: %s\n", line) }
            fmt.Fprint(w, "\n"); f.Flush()
        }
    }
}
```
`renderEvent` maps an event to the right templ fragment: `step_started`/`step_completed` → re-render that `run.templ` (looked up by `run_id` from the DB); `nebula_status_changed` → re-render the affected repo `rack.templ` (lane membership changed). `resync` → instruct a full reload.
- [ ] **Step 4: Run** `-race` → PASS.
- [ ] **Step 5: Commit** `git commit -am "cockpit: SSE handler emitting Datastar fragment merges"`

---

## Task 10: Actions + runtime Publish wiring

**Files:** Modify `internal/cockpit/handlers.go`; Create the `RuntimeActions` interface; wire `Publish` at runtime state-change sites.

- [ ] **Step 1: Define `RuntimeActions`** (interface, defined where consumed):
```go
type RuntimeActions interface {
    Approve(ctx context.Context, nebulaID string) error
    Reject(ctx context.Context, nebulaID, reason string) error
}
```
- [ ] **Step 2: Failing handler test** — POST `/nebulas/{id}/approve` with a fake `RuntimeActions` recording the call; assert 200 and the fake saw the id.
- [ ] **Step 3: Implement `handleApprove`/`handleReject`**: parse `{id}` via `r.PathValue("id")`, call `s.rt.Approve/Reject`, then `s.notifier.Publish(Event{Topic:"fleet", Type:"nebula_status_changed", Data: map[string]any{"id": id}})`, and respond with the re-rendered card fragment for the actor (optimistic).
- [ ] **Step 4: Adapter** — in Task 11, implement `RuntimeActions` over the existing nebula store + trigger queue (approve = set status `approved` + enqueue architect; reject = set status + reason). Locate the existing approve path the TUI uses (grep `Approve`/`awaiting_approval` in `internal/tui` + `internal/fabric`) and reuse it.
- [ ] **Step 5: Wire Notifier into the runtime emit sites** — where the runtime/scheduler persist a transition, call `notifier.Publish`. Concretely, inject an optional `Publisher` (interface `Publish(Event)`) into:
  - `internal/constellations/runtime.go` `persistTransition` / `terminate` → `step_completed` with `{run_id, node, cost_usd}`.
  - `internal/constellations/dispatch_star.go` start → `step_started`.
  - the sensor scheduler seed path → `nebula_status_changed`.
  Define a `Publisher` interface in `constellations` (nil-safe; a no-op when the cockpit is off) so this compiles and runs without the cockpit. Add a test asserting a transition calls Publish.
- [ ] **Step 6: Run** all `go test ./internal/cockpit/... ./internal/constellations/... -race` → PASS. **Commit** `git commit -am "cockpit: approve/reject actions + runtime Publish wiring"`

---

## Task 11: `cmd/serve.go` + config wiring + GitHub adapter

**Files:** Create `cmd/serve.go`; the GitHub badge adapter; wire `[cockpit]` config.

- [ ] **Step 1:** Add `[cockpit]` to the Viper config load (mirror an existing section) → `cockpit.Config`.
- [ ] **Step 2:** `cmd/serve.go` — `quasar serve [--cockpit]`: builds the fabric DB + runtime + Notifier (shared with the supervisor), reads the token from `<data-dir>/cockpit-token` (error with a hint to run `quasar cockpit token` if missing), constructs `cockpit.New(Opts{...Assets: cockpit.Assets()...})`, and runs it alongside the supervisor when `cockpit.enabled || --cockpit`. When disabled, the cockpit isn't started.
- [ ] **Step 3:** GitHub badge adapter implementing `GitHubBadger` over `internal/sensors/github` (read-only `gh pr view`); nil when no GitHub client configured.
- [ ] **Step 4: Test** — `serve --cockpit-only` (a test flag that starts only the cockpit) against a temp DB; hit `/login`, post the token, GET `/`, assert 200 + board. Mark it as an integration test.
- [ ] **Step 5: Build both tags** `go build ./... && go build -tags cockpit ./...` → both compile. **Commit** `git commit -am "cockpit: quasar serve wiring + GitHub badge adapter"`

---

## Task 12: End-to-end smoke + polish pass

- [ ] **Step 1:** `scripts/build-cockpit.sh` → `./quasar cockpit token` → `./quasar serve --cockpit` → open `http://127.0.0.1:7330`, paste token, confirm the board renders matching the mockup. Trigger a run; confirm the in-flight card updates live without reload.
- [ ] **Step 2:** Visual diff against the mockup; tune Tailwind classes in the templ files until the rendered board matches the committed reference (spacing, neon accents, pulse animation, mono data).
- [ ] **Step 3:** `go vet ./...`, `go test ./... -race`, `scripts/lint.sh` → all green. Both build tags compile.
- [ ] **Step 4: Commit** `git commit -am "cockpit: e2e smoke + visual polish to match the reference"`

---

## Task 13: Docs

- [ ] **Step 1:** `docs/cockpit.md` — what it is, `cockpit.enabled` flag, `quasar cockpit token`, `scripts/build-cockpit.sh` (templ + tailwind), the SSE/Datastar contract, and that the TUI remains canonical. Link from `docs/README.md` and `CLAUDE.md`'s package table (add `internal/cockpit/`).
- [ ] **Step 2: Commit** `git commit -am "docs: cockpit operations + toolchain note"`

---

## Self-review notes (coverage)

- Spec "server renders HTML, no JSON API" → Tasks 7–9 (templ + SSE fragments). ✓
- "Notifier single source of truth, slow-subscriber drop+resync" → Task 3 + Task 10 wiring. ✓
- "Bearer token, login, 401 on fragment/SSE" → Task 5. ✓
- "go:embed behind build tag + flag, both builds compile" → Tasks 4, 11, 12. ✓
- "Mission Control visual" → Task 7 against the committed mockup; Task 12 polish. ✓
- "approve/reject wired, <1s multi-operator" → Tasks 9, 10. ✓
- Deferred (run-detail, live-tail viewer, nebula-detail) → explicitly out of these tasks. ✓

**Toolchain risk note:** Tasks 1, 7, 11, 12 depend on `templ` and the Tailwind CLI being available on the build host. `build-cockpit.sh` is the single source of those invocations; generated `*_templ.go` and `cockpit.css` are committed so `go build -tags cockpit` works without re-running the tools.
