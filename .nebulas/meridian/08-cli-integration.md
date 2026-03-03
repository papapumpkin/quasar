+++
id = "cli-integration"
title = "CLI integration: --web flag, auto-open browser, graceful shutdown"
type = "feature"
priority = 2
depends_on = ["gate-forms", "dag-visualization"]
scope = ["cmd/nebula_apply.go", "cmd/tui.go", "internal/web/server.go"]
allow_scope_overlap = true
+++

## Problem

The web dashboard exists as a standalone package (`internal/web/`) but has no CLI integration. Users cannot launch it. The dashboard needs to be wirable from `quasar nebula apply --web` (run alongside execution) and `quasar cockpit --web` (standalone dashboard for browsing nebula state). The web server must coordinate with the nebula execution lifecycle: start before workers begin, stay alive during execution, and shut down gracefully when execution completes or the user interrupts.

## Solution

### Flag registration

Add `--web` and `--web-port` flags to both `nebula apply` and `cockpit` commands:

```go
// In cmd/nebula_apply.go addNebulaApplyFlags:
cmd.Flags().Bool("web", false, "Launch web dashboard alongside execution")
cmd.Flags().Int("web-port", 0, "Port for web dashboard (0 = auto-assign)")

// In cmd/tui.go init:
cockpitCmd.Flags().Bool("web", false, "Open web dashboard instead of TUI")
cockpitCmd.Flags().Int("web-port", 0, "Port for web dashboard (0 = auto-assign)")
```

### Web server lifecycle in `nebula apply --web`

When `--web` is passed, the CLI starts the web server before engine execution and passes it the event bus:

```go
func runWithWeb(ctx context.Context, engine *nebula.Engine, b bus.Bus, cfg nebula.EngineConfig, port int) error {
    // Create web server with bus subscriber.
    srv, err := web.NewServer(web.ServerConfig{
        Bus:       b,
        NebulaDir: cfg.NebulaDir,
        Port:      port,
    })
    if err != nil {
        return fmt.Errorf("create web server: %w", err)
    }

    // Start server in background.
    srvCtx, srvCancel := context.WithCancel(ctx)
    defer srvCancel()
    addr, err := srv.Start(srvCtx)
    if err != nil {
        return fmt.Errorf("start web server: %w", err)
    }
    fmt.Fprintf(os.Stderr, "[web] dashboard at http://%s\n", addr)

    // Auto-open browser.
    openBrowser(fmt.Sprintf("http://%s", addr))

    // Run engine (blocking).
    result := engine.Run(ctx)

    // Keep server alive briefly for final SSE delivery, then shut down.
    time.AfterFunc(2*time.Second, srvCancel)
    srv.Wait()

    if result.Err != nil {
        return result.Err
    }
    return nil
}
```

### Browser auto-open

```go
// openBrowser opens the given URL in the default browser.
// Errors are logged but not fatal (headless environments).
func openBrowser(url string) {
    var cmd *exec.Cmd
    switch runtime.GOOS {
    case "darwin":
        cmd = exec.Command("open", url)
    case "linux":
        cmd = exec.Command("xdg-open", url)
    default:
        return // Unsupported platform.
    }
    if err := cmd.Start(); err != nil {
        fmt.Fprintf(os.Stderr, "[web] could not open browser: %v\n", err)
    }
}
```

### Server.Start and graceful shutdown

Extend `Server` with lifecycle methods:

```go
// Start begins listening on the configured port (0 = auto-assign).
// Returns the resolved address. The server shuts down when ctx is cancelled.
func (s *Server) Start(ctx context.Context) (string, error) {
    ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.Port))
    if err != nil {
        return "", err
    }
    s.addr = ln.Addr().String()

    s.httpServer = &http.Server{Handler: s.mux}

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
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        s.drainSSE()
        s.httpServer.Shutdown(shutdownCtx)
    }()

    return s.addr, nil
}

// Wait blocks until the server has fully shut down.
func (s *Server) Wait() {
    s.wg.Wait()
}
```

### Cockpit `--web` mode

In `cmd/tui.go`, when `--web` is passed, skip the BubbleTea TUI entirely and launch the web dashboard:

```go
func runTUI(cmd *cobra.Command, _ []string) error {
    useWeb, _ := cmd.Flags().GetBool("web")
    if useWeb {
        return runCockpitWeb(cmd)
    }
    // ... existing TUI path ...
}

func runCockpitWeb(cmd *cobra.Command) error {
    port, _ := cmd.Flags().GetInt("web-port")
    baseDir := resolveBaseDir(cmd)

    srv, err := web.NewServer(web.ServerConfig{
        NebulaDir: baseDir,
        Port:      port,
        ReadOnly:  true, // No execution, just viewing.
    })
    if err != nil {
        return err
    }

    ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
    defer cancel()

    addr, err := srv.Start(ctx)
    if err != nil {
        return err
    }
    fmt.Fprintf(os.Stderr, "[cockpit] web dashboard at http://%s\n", addr)
    openBrowser(fmt.Sprintf("http://%s", addr))

    // Block until interrupt.
    <-ctx.Done()
    srv.Wait()
    return nil
}
```

### Coexistence with TUI

When `--web` is used with `nebula apply`, the TUI is disabled (web replaces it). Both cannot run simultaneously because the TUI takes over the terminal. The web server writes status messages to stderr but does not use alternate screen buffer.

If neither `--web` nor `--no-tui` is set and the terminal is a TTY, the existing TUI behavior is preserved unchanged.

## Files

- `cmd/nebula_apply.go` — add `--web`/`--web-port` flags, add `runWithWeb` function, wire into `runNebulaApply` branching
- `cmd/tui.go` — add `--web`/`--web-port` flags to cockpit, add `runCockpitWeb`, modify `runTUI` to branch on web flag
- `cmd/browser.go` — `openBrowser(url string)` utility function
- `internal/web/server.go` — add `Start(ctx) (string, error)`, `Wait()`, `ServerConfig.ReadOnly`, graceful shutdown logic
- `internal/web/server_test.go` — test server start/shutdown lifecycle, port assignment, SSE drain on shutdown

## Acceptance Criteria

- [ ] `quasar nebula apply .nebulas/foo --auto --web` starts web server, opens browser, runs execution, shuts down on completion
- [ ] `quasar nebula apply .nebulas/foo --auto --web --web-port=8080` uses the specified port
- [ ] `quasar cockpit --web` opens web dashboard for browsing nebula state without execution
- [ ] `quasar cockpit --web --web-port=9090` uses the specified port
- [ ] Browser auto-opens on macOS and Linux (no error on unsupported platforms)
- [ ] Web server shuts down gracefully when execution completes (SSE connections drained)
- [ ] Web server shuts down gracefully on SIGINT
- [ ] `--web` disables TUI (no alternate screen buffer)
- [ ] Without `--web`, existing TUI behavior is completely unchanged
- [ ] Port 0 auto-assigns a free port and prints the resolved address
- [ ] `go build ./...` and `go vet ./...` pass
- [ ] `go test ./...` passes
