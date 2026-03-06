package dialogue

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewSession(t *testing.T) {
	t.Parallel()
	req := Request{
		PhaseID: "p1",
		Title:   "Test Session",
		Context: "some context",
		Kind:    "escalation",
		Options: []string{"retry", "skip"},
	}
	s := NewSession(req)

	if s.ID() == "" {
		t.Fatal("expected non-empty ID")
	}
	if !strings.HasPrefix(s.ID(), "dlg-") {
		t.Fatalf("expected ID prefix dlg-, got %s", s.ID())
	}
	got := s.Request()
	if got.PhaseID != req.PhaseID || got.Title != req.Title || got.Context != req.Context || got.Kind != req.Kind {
		t.Fatalf("Request() mismatch: got %+v", got)
	}
	if len(s.Transcript()) != 0 {
		t.Fatalf("expected empty transcript, got %d messages", len(s.Transcript()))
	}
}

func TestUniqueIDs(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool)
	for range 100 {
		id := NewSession(Request{}).ID()
		if seen[id] {
			t.Fatalf("duplicate ID: %s", id)
		}
		seen[id] = true
	}
}

func TestSendReceiveRoundTrip(t *testing.T) {
	t.Parallel()
	s := NewSession(Request{Title: "round-trip"})
	ctx := context.Background()

	// Agent sends, display reads from ToHuman.
	go func() {
		if err := s.Send(ctx, "hello human"); err != nil {
			t.Errorf("Send: %v", err)
		}
	}()

	select {
	case msg := <-s.ToHuman():
		if msg.Role != RoleAgent {
			t.Fatalf("expected RoleAgent, got %s", msg.Role)
		}
		if msg.Content != "hello human" {
			t.Fatalf("expected 'hello human', got %q", msg.Content)
		}
		if msg.Time.IsZero() {
			t.Fatal("expected non-zero time")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for agent message")
	}

	// Display writes to FromHuman, agent receives.
	go func() {
		s.FromHuman() <- Message{Role: RoleHuman, Content: "hi agent", Time: time.Now()}
	}()

	reply, err := s.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if reply != "hi agent" {
		t.Fatalf("expected 'hi agent', got %q", reply)
	}
}

func TestTranscriptRecordsMessages(t *testing.T) {
	t.Parallel()
	s := NewSession(Request{})
	ctx := context.Background()

	// Send from agent.
	go func() { <-s.ToHuman() }() // drain
	if err := s.Send(ctx, "msg1"); err != nil {
		t.Fatal(err)
	}

	// Send from human.
	go func() {
		s.FromHuman() <- Message{Role: RoleHuman, Content: "msg2", Time: time.Now()}
	}()
	if _, err := s.Receive(ctx); err != nil {
		t.Fatal(err)
	}

	tr := s.Transcript()
	if len(tr) != 2 {
		t.Fatalf("expected 2 transcript entries, got %d", len(tr))
	}
	if tr[0].Content != "msg1" || tr[0].Role != RoleAgent {
		t.Fatalf("transcript[0] = %+v", tr[0])
	}
	if tr[1].Content != "msg2" || tr[1].Role != RoleHuman {
		t.Fatalf("transcript[1] = %+v", tr[1])
	}
}

func TestTranscriptReturnsCopy(t *testing.T) {
	t.Parallel()
	s := NewSession(Request{})
	ctx := context.Background()

	go func() { <-s.ToHuman() }()
	_ = s.Send(ctx, "original")

	tr := s.Transcript()
	tr[0].Content = "mutated"

	tr2 := s.Transcript()
	if tr2[0].Content != "original" {
		t.Fatal("Transcript should return a copy, but mutation leaked")
	}
}

func TestSendOnClosedSession(t *testing.T) {
	t.Parallel()
	s := NewSession(Request{})
	s.Close()

	// Fill the buffered channel so Send must select between channel and closed.
	for range cap(s.toHuman) {
		s.toHuman <- Message{}
	}

	err := s.Send(context.Background(), "too late")
	if err != ErrClosed {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

func TestReceiveOnClosedSession(t *testing.T) {
	t.Parallel()
	s := NewSession(Request{})
	s.Close()

	_, err := s.Receive(context.Background())
	if err != ErrClosed {
		t.Fatalf("expected ErrClosed, got %v", err)
	}
}

func TestSendCancelledContext(t *testing.T) {
	t.Parallel()
	s := NewSession(Request{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Fill the buffered channel so Send must select and notice the cancelled ctx.
	for range cap(s.toHuman) {
		s.toHuman <- Message{}
	}

	err := s.Send(ctx, "cancelled")
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestReceiveCancelledContext(t *testing.T) {
	t.Parallel()
	s := NewSession(Request{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Receive(ctx)
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	t.Parallel()
	s := NewSession(Request{})
	s.Close()
	s.Close() // must not panic
	s.Close()

	select {
	case <-s.Closed():
	default:
		t.Fatal("Closed() channel should be closed")
	}
}

func TestClosedChannel(t *testing.T) {
	t.Parallel()
	s := NewSession(Request{})

	select {
	case <-s.Closed():
		t.Fatal("Closed() should not be closed yet")
	default:
	}

	s.Close()

	select {
	case <-s.Closed():
	case <-time.After(time.Second):
		t.Fatal("Closed() should be closed after Close()")
	}
}

func TestConcurrentSendReceive(t *testing.T) {
	t.Parallel()
	s := NewSession(Request{})
	ctx := context.Background()
	const n = 50

	var wg sync.WaitGroup

	// Agent sends n messages.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range n {
			if err := s.Send(ctx, fmt.Sprintf("agent-%d", i)); err != nil {
				t.Errorf("Send %d: %v", i, err)
			}
		}
	}()

	// Display drains ToHuman.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range n {
			<-s.ToHuman()
		}
	}()

	// Display sends n messages.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range n {
			s.FromHuman() <- Message{
				Role:    RoleHuman,
				Content: fmt.Sprintf("human-%d", i),
				Time:    time.Now(),
			}
		}
	}()

	// Agent drains Receive.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range n {
			if _, err := s.Receive(ctx); err != nil {
				t.Errorf("Receive: %v", err)
			}
		}
	}()

	wg.Wait()

	tr := s.Transcript()
	if len(tr) != 2*n {
		t.Fatalf("expected %d transcript entries, got %d", 2*n, len(tr))
	}
}

func TestMultiTurnConversation(t *testing.T) {
	t.Parallel()
	s := NewSession(Request{Kind: "question", Title: "multi-turn"})
	ctx := context.Background()

	// Simulate a 3-turn conversation.
	go func() {
		for i := range 3 {
			// Agent speaks.
			msg := <-s.ToHuman()
			if msg.Role != RoleAgent {
				t.Errorf("turn %d: expected agent, got %s", i, msg.Role)
			}
			// Human replies.
			s.FromHuman() <- Message{
				Role:    RoleHuman,
				Content: fmt.Sprintf("reply-%d", i),
				Time:    time.Now(),
			}
		}
	}()

	for i := range 3 {
		if err := s.Send(ctx, fmt.Sprintf("question-%d", i)); err != nil {
			t.Fatalf("turn %d Send: %v", i, err)
		}
		reply, err := s.Receive(ctx)
		if err != nil {
			t.Fatalf("turn %d Receive: %v", i, err)
		}
		if reply != fmt.Sprintf("reply-%d", i) {
			t.Fatalf("turn %d: expected reply-%d, got %q", i, i, reply)
		}
	}

	tr := s.Transcript()
	if len(tr) != 6 {
		t.Fatalf("expected 6 transcript entries, got %d", len(tr))
	}
}

