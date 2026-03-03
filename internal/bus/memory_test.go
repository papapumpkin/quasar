package bus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublishSubscribe(t *testing.T) {
	t.Parallel()

	b := NewMemoryBus()
	defer b.Close()

	sub := b.Subscribe("test", 0)
	defer sub.Unsubscribe()

	ev := New(KindInfo)
	ev.Message = "hello"

	ctx := context.Background()
	if err := b.Publish(ctx, ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case got := <-sub.Events():
		if got.Kind != KindInfo {
			t.Errorf("Kind = %q, want %q", got.Kind, KindInfo)
		}
		if got.Message != "hello" {
			t.Errorf("Message = %q, want %q", got.Message, "hello")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestFanOut(t *testing.T) {
	t.Parallel()

	b := NewMemoryBus()
	defer b.Close()

	sub1 := b.Subscribe("sub1", 0)
	defer sub1.Unsubscribe()

	sub2 := b.Subscribe("sub2", 0)
	defer sub2.Unsubscribe()

	ev := New(KindAgentStart)
	ev.Role = "coder"

	ctx := context.Background()
	if err := b.Publish(ctx, ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	for _, tc := range []struct {
		name string
		sub  Subscription
	}{
		{"sub1", sub1},
		{"sub2", sub2},
	} {
		select {
		case got := <-tc.sub.Events():
			if got.Kind != KindAgentStart {
				t.Errorf("%s: Kind = %q, want %q", tc.name, got.Kind, KindAgentStart)
			}
			if got.Role != "coder" {
				t.Errorf("%s: Role = %q, want %q", tc.name, got.Role, "coder")
			}
		case <-time.After(time.Second):
			t.Fatalf("%s: timed out waiting for event", tc.name)
		}
	}
}

func TestBackpressure(t *testing.T) {
	t.Parallel()

	b := NewMemoryBus()
	defer b.Close()

	// bufSize=1 means only 1 event fits before the channel blocks.
	sub := b.Subscribe("slow", 1)
	defer sub.Unsubscribe()

	ctx := context.Background()

	// First publish should succeed immediately (fills the buffer).
	if err := b.Publish(ctx, New(KindInfo)); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	// Second publish should block because the buffer is full.
	blocked := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(blocked)
		if err := b.Publish(ctx, New(KindInfo)); err != nil {
			t.Errorf("second Publish: %v", err)
		}
		close(done)
	}()

	<-blocked
	// Give the goroutine time to actually block on the channel send.
	time.Sleep(50 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("second Publish completed without the subscriber reading; expected backpressure")
	default:
		// Good — still blocked.
	}

	// Drain one event to unblock the publisher.
	<-sub.Events()

	select {
	case <-done:
		// Good — unblocked after read.
	case <-time.After(time.Second):
		t.Fatal("second Publish still blocked after subscriber read")
	}
}

func TestUnsubscribe(t *testing.T) {
	t.Parallel()

	b := NewMemoryBus()
	defer b.Close()

	sub := b.Subscribe("temp", 1)

	// Unsubscribe should remove the subscriber.
	sub.Unsubscribe()

	// Publishing should not block even though the subscriber had a small buffer.
	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		done <- b.Publish(ctx, New(KindInfo))
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Publish after Unsubscribe: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Publish blocked after subscriber unsubscribed")
	}

	// Calling Unsubscribe again should be safe (idempotent via sync.Once).
	sub.Unsubscribe()
}

func TestClosedBus(t *testing.T) {
	t.Parallel()

	b := NewMemoryBus()

	sub := b.Subscribe("before-close", 0)

	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Subscriber channel should be closed.
	select {
	case _, ok := <-sub.Events():
		if ok {
			t.Fatal("expected subscriber channel to be closed after bus Close")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscriber channel to close")
	}

	// Publish after Close returns ErrBusClosed.
	err := b.Publish(context.Background(), New(KindInfo))
	if err != ErrBusClosed {
		t.Fatalf("Publish after Close: got %v, want %v", err, ErrBusClosed)
	}

	// Close is idempotent.
	if err := b.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestSubscribeAfterClose(t *testing.T) {
	t.Parallel()

	b := NewMemoryBus()
	b.Close()

	sub := b.Subscribe("late", 0)

	// The channel should be immediately closeable (already closed).
	select {
	case _, ok := <-sub.Events():
		if ok {
			t.Fatal("expected subscriber channel to be closed on subscribe-after-close")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out; subscriber channel from closed bus should be immediately readable")
	}
}

func TestConcurrentPublish(t *testing.T) {
	t.Parallel()

	b := NewMemoryBus()
	defer b.Close()

	const publishers = 10
	const eventsPerPublisher = 100
	totalEvents := publishers * eventsPerPublisher

	sub := b.Subscribe("collector", totalEvents)
	defer sub.Unsubscribe()

	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(publishers)

	for i := 0; i < publishers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < eventsPerPublisher; j++ {
				if err := b.Publish(ctx, New(KindInfo)); err != nil {
					t.Errorf("Publish: %v", err)
				}
			}
		}()
	}

	wg.Wait()

	var received int
	timeout := time.After(5 * time.Second)
	for received < totalEvents {
		select {
		case <-sub.Events():
			received++
		case <-timeout:
			t.Fatalf("received %d/%d events before timeout", received, totalEvents)
		}
	}

	if received != totalEvents {
		t.Errorf("received = %d, want %d", received, totalEvents)
	}
}

func TestContextCancellation(t *testing.T) {
	t.Parallel()

	b := NewMemoryBus()
	defer b.Close()

	// bufSize=1 — fill it so the next Publish blocks.
	sub := b.Subscribe("slow", 1)
	defer sub.Unsubscribe()

	ctx := context.Background()
	if err := b.Publish(ctx, New(KindInfo)); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	// Cancel the context before the second Publish can deliver.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := b.Publish(ctx, New(KindInfo))
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if err != context.Canceled {
		t.Fatalf("Publish error = %v, want %v", err, context.Canceled)
	}
}

func TestMultipleSubscribersBackpressureIsolation(t *testing.T) {
	t.Parallel()

	b := NewMemoryBus()
	defer b.Close()

	// fast has a large buffer; slow has a tiny buffer.
	fast := b.Subscribe("fast", 100)
	defer fast.Unsubscribe()
	slow := b.Subscribe("slow", 1)
	defer slow.Unsubscribe()

	ctx := context.Background()

	// Fill the slow subscriber's buffer.
	if err := b.Publish(ctx, New(KindInfo)); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// The next Publish will block on the slow subscriber, but the fast
	// subscriber should still have received the first event.
	select {
	case <-fast.Events():
		// Good.
	case <-time.After(time.Second):
		t.Fatal("fast subscriber did not receive the event")
	}
}

func TestConcurrentSubscribeUnsubscribe(t *testing.T) {
	t.Parallel()

	b := NewMemoryBus()
	defer b.Close()

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent publishers.
	var pubCount atomic.Int64
	wg.Add(5)
	for i := 0; i < 5; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = b.Publish(ctx, New(KindInfo))
				pubCount.Add(1)
			}
		}()
	}

	// Concurrent subscribe/unsubscribe churn.
	wg.Add(5)
	for i := 0; i < 5; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				s := b.Subscribe("churn", 8)
				// Read a few events.
				for k := 0; k < 2; k++ {
					select {
					case <-s.Events():
					case <-time.After(10 * time.Millisecond):
					}
				}
				s.Unsubscribe()
			}
		}()
	}

	wg.Wait()
}
