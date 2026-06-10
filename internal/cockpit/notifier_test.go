package cockpit

import (
	"testing"
	"time"
)

func TestNotifierDelivers(t *testing.T) {
	n := NewNotifier(8)
	_, ch, cancel := n.Subscribe([]string{"fleet"})
	defer cancel()
	n.Publish(Event{Topic: "fleet", Type: "nebula_status_changed"})
	select {
	case e := <-ch:
		if e.Type != "nebula_status_changed" {
			t.Fatalf("got %q", e.Type)
		}
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
	case <-ch:
		t.Fatal("should not receive other topic")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNotifierSlowSubscriberResync(t *testing.T) {
	n := NewNotifier(2) // tiny buffer
	_, ch, cancel := n.Subscribe([]string{"runs"})
	defer cancel()
	for i := 0; i < 10; i++ {
		n.Publish(Event{Topic: "runs", Type: "step_completed"})
	}
	saw := map[string]bool{}
	for i := 0; i < 3; i++ {
		select {
		case e := <-ch:
			saw[e.Type] = true
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !saw["resync"] {
		t.Fatal("expected a resync hint after overflow")
	}
}

func TestNotifierCancelRemoves(t *testing.T) {
	n := NewNotifier(8)
	_, _, cancel := n.Subscribe([]string{"fleet"})
	cancel()
	n.Publish(Event{Topic: "fleet", Type: "x"}) // must not panic / block
	if n.count() != 0 {
		t.Fatal("subscriber not removed")
	}
}
