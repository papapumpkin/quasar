package agentmail

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestServerStartAndSSEReachable(t *testing.T) {
	t.Parallel()

	store := NewStore(nil) // nil db is fine for stub tools
	srv := NewServer(store, 0, nil)

	ctx := context.Background()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := srv.Stop(context.Background()); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	addr := srv.Addr()
	if addr == nil {
		t.Fatal("expected non-nil listener address after Start")
	}

	// Poll the SSE endpoint until it responds.
	sseURL := fmt.Sprintf("http://%s/sse", addr.String())
	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.Get(sseURL)
	if err != nil {
		t.Fatalf("GET %s: %v", sseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("SSE status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/event-stream")
	}
}

func TestServerGracefulShutdown(t *testing.T) {
	t.Parallel()

	store := NewStore(nil)
	srv := NewServer(store, 0, nil)

	ctx := context.Background()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	addr := srv.Addr()
	sseURL := fmt.Sprintf("http://%s/sse", addr.String())

	// Verify it's up.
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(sseURL)
	if err != nil {
		t.Fatalf("GET before shutdown: %v", err)
	}
	resp.Body.Close()

	// Shut down.
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Stop(shutCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Verify it's no longer accepting connections.
	_, err = client.Get(sseURL)
	if err == nil {
		t.Error("expected error after shutdown, got nil")
	}
}

func TestServerStopWithoutStart(t *testing.T) {
	t.Parallel()

	store := NewStore(nil)
	srv := NewServer(store, 0, nil)

	// Stop without Start should not panic or error.
	if err := srv.Stop(context.Background()); err != nil {
		t.Errorf("Stop without Start: %v", err)
	}
}
