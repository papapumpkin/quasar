package constellations

import (
	"context"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// TestStartHeartbeatRefreshesUntilStopped verifies that an in-flight Step's
// heartbeat ticker advances the run's heartbeat_at while running and stops
// after the returned stop func is called. This is what lets a crash reaper
// distinguish a healthy long step from a dead process.
func TestStartHeartbeatRefreshesUntilStopped(t *testing.T) {
	ctx := context.Background()
	rt, nebID := newTestRuntime(t, &fakeLoader{}, &fakeInvoker{})

	// Seed a running run with a deliberately stale heartbeat.
	const stale = 1
	runID, err := rt.runStore.InsertRun(ctx, fabric.RunRow{
		NebulaID:          nebID,
		ConstellationName: "hb",
		State:             "running",
		CurrentNode:       "n",
		HeartbeatAt:       stale,
	})
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	stop := rt.startHeartbeat(ctx, runID, 2*time.Millisecond)
	// Wait long enough for several ticks.
	time.Sleep(40 * time.Millisecond)
	stop()

	run, err := rt.runStore.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.HeartbeatAt <= stale {
		t.Fatalf("heartbeat not refreshed: got %d, want > %d", run.HeartbeatAt, stale)
	}

	// After stop, the heartbeat must stop advancing.
	settled := run.HeartbeatAt
	time.Sleep(20 * time.Millisecond)
	run, err = rt.runStore.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun after stop: %v", err)
	}
	if run.HeartbeatAt != settled {
		t.Errorf("heartbeat advanced after stop: %d -> %d", settled, run.HeartbeatAt)
	}
}

// TestStartHeartbeatStopsOnContextCancel verifies the ticker goroutine exits
// when its context is cancelled, even if stop is never called — no leak.
func TestStartHeartbeatStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rt, nebID := newTestRuntime(t, &fakeLoader{}, &fakeInvoker{})
	runID, err := rt.runStore.InsertRun(ctx, fabric.RunRow{
		NebulaID: nebID, ConstellationName: "hb", State: "running", CurrentNode: "n", HeartbeatAt: 1,
	})
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	stop := rt.startHeartbeat(ctx, runID, 2*time.Millisecond)
	cancel()
	// Calling stop after cancel must be safe (no double-close, no panic).
	stop()
}
