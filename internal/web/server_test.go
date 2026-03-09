package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestNewServer(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{
		NebulaDir: "/tmp/test-nebula",
		Port:      0,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
}

func TestServerStartAndShutdown(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{
		NebulaDir: "/tmp/test-nebula",
		Port:      0,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	addr, err := srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if addr == "" {
		t.Fatal("expected non-empty address")
	}
	t.Logf("server listening on %s", addr)

	// Verify the server is responding.
	resp, err := http.Get(fmt.Sprintf("http://%s/healthz", addr))
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("healthz body = %q, want %q", string(body), "ok")
	}

	// Cancel context to trigger shutdown.
	cancel()
	srv.Wait()

	// Verify server is no longer responding.
	_, err = http.Get(fmt.Sprintf("http://%s/healthz", addr))
	if err == nil {
		t.Error("expected error after shutdown, got nil")
	}
}

func TestServerAutoAssignsPort(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{
		NebulaDir: "/tmp/test-nebula",
		Port:      0,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, err := srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		cancel()
		srv.Wait()
	}()

	if addr == "" {
		t.Fatal("expected non-empty auto-assigned address")
	}
	if !strings.Contains(addr, ":") {
		t.Errorf("address %q does not contain port separator", addr)
	}
}

func TestServerAddrMatchesStartReturn(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{
		NebulaDir: "/tmp/test-nebula",
		Port:      0,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, err := srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		cancel()
		srv.Wait()
	}()

	if srv.Addr() != addr {
		t.Errorf("Addr() = %q, want %q", srv.Addr(), addr)
	}
}

func TestServerDashboard(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{
		NebulaDir: "/tmp/test-nebula",
		Port:      0,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, err := srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		cancel()
		srv.Wait()
	}()

	resp, err := http.Get(fmt.Sprintf("http://%s/", addr))
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("dashboard status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Quasar Dashboard") {
		t.Error("dashboard page should contain 'Quasar Dashboard'")
	}
}

func TestServerNotFound(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{
		NebulaDir: "/tmp/test-nebula",
		Port:      0,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, err := srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		cancel()
		srv.Wait()
	}()

	resp, err := http.Get(fmt.Sprintf("http://%s/nonexistent", addr))
	if err != nil {
		t.Fatalf("GET /nonexistent: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestServerReadOnlyConfig(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{
		NebulaDir: "/tmp/test-nebula",
		Port:      0,
		ReadOnly:  true,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if !srv.cfg.ReadOnly {
		t.Error("expected ReadOnly to be true")
	}
}

func TestServerAddrBeforeStart(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{
		NebulaDir: "/tmp/test-nebula",
		Port:      0,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	if addr := srv.Addr(); addr != "" {
		t.Errorf("Addr() before Start should be empty, got %q", addr)
	}
}

func TestServerSetNebula(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{
		NebulaDir: "/tmp/test-nebula",
		Port:      0,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// SetNebula should not panic with nil values.
	srv.SetNebula(nil, nil)
	if !srv.startTime.IsZero() {
		t.Error("startTime should remain zero when nebula is nil")
	}
}
