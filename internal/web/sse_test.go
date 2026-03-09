package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// mockEventSource is a test double for EventSource that sends a fixed
// set of events and then blocks until the context is cancelled.
type mockEventSource struct {
	events []Event
}

// Subscribe implements EventSource. It sends all pre-configured events
// into the channel, then blocks until ctx is done.
func (m *mockEventSource) Subscribe(ctx context.Context) (<-chan Event, func()) {
	ch := make(chan Event, len(m.events)+1)
	for _, ev := range m.events {
		ch <- ev
	}

	cancelCh := make(chan struct{})
	cancel := func() { close(cancelCh) }

	// Close the event channel when the context is cancelled or cancel is called.
	go func() {
		select {
		case <-ctx.Done():
		case <-cancelCh:
		}
		close(ch)
	}()

	return ch, cancel
}

func TestHandleSSE_ContentType(t *testing.T) {
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
}

func TestHandleSSE_StreamsEvents(t *testing.T) {
	t.Parallel()

	source := &mockEventSource{
		events: []Event{
			{Type: "phase-status", Data: `{"id":"p1","status":"done"}`},
		},
	}

	srv, err := NewServer(ServerConfig{
		Source:    source,
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
		// Verify named SSE event format: "event: <type>\ndata: <json>\n\n"
		if !strings.Contains(data, "event: phase-status") {
			t.Errorf("SSE data = %q, want to contain 'event: phase-status'", data)
		}
		if !strings.Contains(data, `data: {"id":"p1","status":"done"}`) {
			t.Errorf("SSE data = %q, want to contain JSON payload", data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for SSE event")
	}
}

func TestHandleSSE_ClientDisconnect(t *testing.T) {
	t.Parallel()

	// Source that sends events forever until cancelled.
	source := &slowEventSource{}

	srv, err := NewServer(ServerConfig{
		Source:    source,
		NebulaDir: "/tmp/test-nebula",
		Port:      0,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	addr, err := srv.Start(serverCtx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		serverCancel()
		srv.Wait()
	}()

	// Connect with a context we cancel quickly to simulate disconnect.
	clientCtx, clientCancel := context.WithCancel(context.Background())

	client := &http.Client{}
	req, err := http.NewRequestWithContext(clientCtx, http.MethodGet, fmt.Sprintf("http://%s/events", addr), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}

	// Cancel the client context to disconnect.
	clientCancel()
	resp.Body.Close()

	// Give the handler a moment to clean up.
	time.Sleep(100 * time.Millisecond)

	// Verify the SSE client was removed.
	srv.sseMu.Lock()
	count := len(srv.sseClients)
	srv.sseMu.Unlock()

	if count != 0 {
		t.Errorf("expected 0 SSE clients after disconnect, got %d", count)
	}
}

func TestDrainSSE(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{
		NebulaDir: "/tmp/test-nebula",
		Port:      0,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// Add mock SSE clients.
	ch1 := make(chan Event, 1)
	ch2 := make(chan Event, 1)
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

func TestDrainSSEIdempotent(t *testing.T) {
	t.Parallel()

	srv, err := NewServer(ServerConfig{
		NebulaDir: "/tmp/test-nebula",
		Port:      0,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	ch := make(chan Event, 1)
	srv.addSSEClient(ch)

	// Multiple drains should not panic.
	srv.drainSSE()
	srv.drainSSE()
}

func TestHandleSSE_NoSource(t *testing.T) {
	t.Parallel()

	// Server with no event source — SSE should still return valid headers.
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

	clientCtx, clientCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer clientCancel()

	req, err := http.NewRequestWithContext(clientCtx, http.MethodGet, fmt.Sprintf("http://%s/events", addr), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
}

// slowEventSource sends events at a fixed interval until cancelled.
type slowEventSource struct{}

func (s *slowEventSource) Subscribe(ctx context.Context) (<-chan Event, func()) {
	ch := make(chan Event, 1)
	cancelCh := make(chan struct{})
	cancel := func() { close(cancelCh) }

	go func() {
		defer close(ch)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-cancelCh:
				return
			case <-ticker.C:
				select {
				case ch <- Event{Type: "tick", Data: `{}`}:
				default:
				}
			}
		}
	}()

	return ch, cancel
}
