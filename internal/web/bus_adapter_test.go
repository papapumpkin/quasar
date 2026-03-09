package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/bus"
)

func TestBusAdapter_Subscribe(t *testing.T) {
	t.Parallel()

	eventBus := bus.NewMemoryBus()
	defer eventBus.Close()

	adapter := NewBusAdapter(eventBus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, unsub := adapter.Subscribe(ctx)
	defer unsub()

	// Publish a bus event.
	ev := bus.NewPhase(bus.KindPhaseInfo, "test-phase")
	ev.Message = "hello adapter"
	if err := eventBus.Publish(ctx, ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Read the translated web event.
	select {
	case webEv := <-events:
		if webEv.Type != string(bus.KindPhaseInfo) {
			t.Errorf("Type = %q, want %q", webEv.Type, string(bus.KindPhaseInfo))
		}
		if !strings.Contains(webEv.Data, "test-phase") {
			t.Errorf("Data = %q, want to contain 'test-phase'", webEv.Data)
		}
		if !strings.Contains(webEv.Data, "hello adapter") {
			t.Errorf("Data = %q, want to contain 'hello adapter'", webEv.Data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestBusAdapter_CancelClosesChannel(t *testing.T) {
	t.Parallel()

	eventBus := bus.NewMemoryBus()
	defer eventBus.Close()

	adapter := NewBusAdapter(eventBus)

	ctx, cancel := context.WithCancel(context.Background())
	events, unsub := adapter.Subscribe(ctx)
	defer unsub()

	// Cancel the context.
	cancel()

	// The events channel should be closed.
	select {
	case _, ok := <-events:
		if ok {
			// Might get a buffered event, drain it.
			select {
			case _, ok2 := <-events:
				if ok2 {
					t.Error("expected channel to close after cancel")
				}
			case <-time.After(time.Second):
				t.Error("timed out waiting for channel close")
			}
		}
		// ok == false means channel was closed, which is correct.
	case <-time.After(time.Second):
		t.Error("timed out waiting for channel close")
	}
}

func TestBusAdapter_MultipleSubscribers(t *testing.T) {
	t.Parallel()

	eventBus := bus.NewMemoryBus()
	defer eventBus.Close()

	adapter := NewBusAdapter(eventBus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events1, unsub1 := adapter.Subscribe(ctx)
	defer unsub1()
	events2, unsub2 := adapter.Subscribe(ctx)
	defer unsub2()

	ev := bus.NewPhase(bus.KindPhaseTaskStarted, "p1")
	ev.Message = "started"
	if err := eventBus.Publish(ctx, ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Both subscribers should receive the event.
	for i, ch := range []<-chan Event{events1, events2} {
		select {
		case webEv := <-ch:
			if webEv.Type != string(bus.KindPhaseTaskStarted) {
				t.Errorf("subscriber %d: Type = %q, want %q", i, webEv.Type, string(bus.KindPhaseTaskStarted))
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("subscriber %d: timed out", i)
		}
	}
}

func TestJsonString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", `""`},
		{"plain", "hello", `"hello"`},
		{"quotes", `say "hi"`, `"say \"hi\""`},
		{"backslash", `path\to`, `"path\\to"`},
		{"newline", "line1\nline2", `"line1\nline2"`},
		{"tab", "col1\tcol2", `"col1\tcol2"`},
		{"carriage return", "a\rb", `"a\rb"`},
		{"null byte", "a\x00b", `"a\u0000b"`},
		{"formfeed", "a\fb", "\"a\\fb\""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := jsonString(tt.in)
			if got != tt.want {
				t.Errorf("jsonString(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
