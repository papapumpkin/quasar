package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/bus"
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

func TestServerSSEWithBus(t *testing.T) {
	t.Parallel()

	eventBus := bus.NewMemoryBus()
	defer eventBus.Close()

	srv, err := NewServer(ServerConfig{
		Bus:       eventBus,
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

	// Use a client without an overall timeout so the streaming connection
	// stays open. We control the deadline per-read instead.
	client := &http.Client{}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/events", addr), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	// Give the SSE handler a moment to subscribe to the bus.
	time.Sleep(50 * time.Millisecond)

	// Publish an event and read it from SSE.
	ev := bus.NewPhase(bus.KindPhaseInfo, "test-phase")
	ev.Message = "hello from test"
	if pubErr := eventBus.Publish(ctx, ev); pubErr != nil {
		t.Fatalf("Publish: %v", pubErr)
	}

	// Read from SSE stream with a deadline.
	done := make(chan string, 1)
	go func() {
		buf := make([]byte, 1024)
		n, readErr := resp.Body.Read(buf)
		if readErr != nil {
			done <- ""
			return
		}
		done <- string(buf[:n])
	}()

	select {
	case data := <-done:
		if !strings.Contains(data, "hello from test") {
			t.Errorf("SSE data = %q, want to contain 'hello from test'", data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for SSE event")
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

func TestServerDrainSSE(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{
		NebulaDir: "/tmp/test-nebula",
		Port:      0,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Add mock SSE clients.
	ch1 := make(chan bus.Event, 1)
	ch2 := make(chan bus.Event, 1)
	srv.addSSEClient(ch1)
	srv.addSSEClient(ch2)

	if len(srv.sseClients) != 2 {
		t.Fatalf("expected 2 SSE clients, got %d", len(srv.sseClients))
	}

	// Drain should close all channels.
	srv.drainSSE()

	// Verify channels are closed.
	select {
	case _, ok := <-ch1:
		if ok {
			t.Error("ch1 should be closed")
		}
	default:
		t.Error("ch1 should be readable (closed)")
	}

	select {
	case _, ok := <-ch2:
		if ok {
			t.Error("ch2 should be closed")
		}
	default:
		t.Error("ch2 should be readable (closed)")
	}

	if len(srv.sseClients) != 0 {
		t.Errorf("expected 0 SSE clients after drain, got %d", len(srv.sseClients))
	}
}

func TestServerDrainSSEIdempotent(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{
		NebulaDir: "/tmp/test-nebula",
		Port:      0,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ch := make(chan bus.Event, 1)
	srv.addSSEClient(ch)

	// Multiple drains should not panic.
	srv.drainSSE()
	srv.drainSSE()
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
