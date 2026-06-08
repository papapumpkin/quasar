package claude

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/telemetry"
)

func TestHealthPolicyEvaluate(t *testing.T) {
	t.Parallel()
	p := agent.DefaultHealthPolicy()

	tests := []struct {
		name      string
		snap      healthSnapshot
		wantState HealthState
		wantSigs  []string
	}{
		{
			name:      "all green is healthy",
			snap:      healthSnapshot{elapsed: time.Minute},
			wantState: Healthy,
		},
		{
			name: "wall-clock alone kills",
			snap: healthSnapshot{
				elapsed: p.WallClockCap + time.Second,
				// every other signal green
				writeValid: true, writeIdle: 0,
				tokenValid: true, tokenRate: 100,
				cpuValid: true, cpuIdle: 0,
			},
			wantState: Dead,
			wantSigs:  []string{signalWallClock},
		},
		{
			name: "single write-idle red is degraded",
			snap: healthSnapshot{
				elapsed:    time.Minute,
				writeValid: true, writeIdle: p.FileWriteIdleCap + time.Minute,
			},
			wantState: Degraded,
			wantSigs:  []string{signalWriteIdle},
		},
		{
			name: "two signals red is dead",
			snap: healthSnapshot{
				elapsed:    time.Minute,
				writeValid: true, writeIdle: p.FileWriteIdleCap + time.Minute,
				tokenValid: true, tokenRate: p.TokenRateFloor - 1,
			},
			wantState: Dead,
			wantSigs:  []string{signalWriteIdle, signalTokenRate},
		},
		{
			name: "invalid signals are ignored",
			snap: healthSnapshot{
				elapsed: time.Minute,
				// idle is huge but writeValid=false → not counted
				writeValid: false, writeIdle: time.Hour,
				tokenValid: false, tokenRate: 0,
			},
			wantState: Healthy,
		},
		{
			name: "tool-ratio plus cpu-idle is dead",
			snap: healthSnapshot{
				elapsed:    time.Minute,
				ratioValid: true, readEditRatio: p.ToolCallRatioCeiling + 1,
				cpuValid: true, cpuIdle: p.CPUIdleCap + time.Minute,
			},
			wantState: Dead,
			wantSigs:  []string{signalToolRatio, signalCPUIdle},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state, _, reds := evaluateHealth(p, tt.snap)
			if state != tt.wantState {
				t.Fatalf("state = %v, want %v", state, tt.wantState)
			}
			gotSigs := make([]string, len(reds))
			for i, r := range reds {
				gotSigs[i] = r.Name
			}
			if len(gotSigs) != len(tt.wantSigs) {
				t.Fatalf("signals = %v, want %v", gotSigs, tt.wantSigs)
			}
			for i := range gotSigs {
				if gotSigs[i] != tt.wantSigs[i] {
					t.Fatalf("signals = %v, want %v", gotSigs, tt.wantSigs)
				}
			}
		})
	}
}

func TestHealthPolicyWithDefaults(t *testing.T) {
	t.Parallel()
	// A partial override keeps the rest at defaults.
	p := agent.HealthPolicy{WallClockCap: time.Hour}.WithDefaults()
	if p.WallClockCap != time.Hour {
		t.Errorf("WallClockCap = %v, want 1h (override preserved)", p.WallClockCap)
	}
	if p.FileWriteIdleCap != agent.DefaultFileWriteIdleCap {
		t.Errorf("FileWriteIdleCap = %v, want default %v", p.FileWriteIdleCap, agent.DefaultFileWriteIdleCap)
	}
	if p.CPUIdleCap != agent.DefaultCPUIdleCap {
		t.Errorf("CPUIdleCap = %v, want default %v", p.CPUIdleCap, agent.DefaultCPUIdleCap)
	}
}

func TestDefaultWallClockCapIs25Minutes(t *testing.T) {
	t.Parallel()
	if got := agent.DefaultHealthPolicy().WallClockCap; got != 25*time.Minute {
		t.Fatalf("default WallClockCap = %v, want 25m", got)
	}
}

// startSleeper launches a sleep subprocess in its own session and returns the
// cmd plus an exited channel closed when it is reaped.
func startSleeper(t *testing.T, args ...string) (*exec.Cmd, chan struct{}) {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if len(args) > 0 {
		cmd = exec.Command(args[0], args[1:]...)
	}
	cmd.SysProcAttr = sessionAttr()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	return cmd, exited
}

func TestHealthcheckTwoSignalsKills(t *testing.T) {
	t.Parallel()
	cmd, exited := startSleeper(t)

	hc := &Healthcheck{
		Cmd:         cmd,
		Workdir:     t.TempDir(),
		Tick:        5 * time.Millisecond,
		GracePeriod: 2 * time.Second,
		// Two signals permanently red → Dead on first tick.
		writeIdleFn: func(time.Time) (time.Duration, bool) { return time.Hour, true },
		cpuIdleFn:   func(time.Time) (time.Duration, bool) { return time.Hour, true },
	}

	err := hc.Run(context.Background(), exited)
	var dead *DeadCoderError
	if !errors.As(err, &dead) {
		t.Fatalf("Run err = %v, want *DeadCoderError", err)
	}
	if len(dead.Signals) != 2 {
		t.Fatalf("dead.Signals = %v, want 2", dead.Signals)
	}
	if dead.Workdir != hc.Workdir {
		t.Errorf("Workdir = %q, want %q", dead.Workdir, hc.Workdir)
	}
	<-exited // subprocess was reaped
}

func TestHealthcheckSingleSignalDegradesNotKills(t *testing.T) {
	t.Parallel()
	cmd, exited := startSleeper(t)
	defer func() {
		_ = signalSubprocess(cmd, killSignal())
		<-exited
	}()

	var lastState atomic.Int32
	hc := &Healthcheck{
		Cmd:         cmd,
		Workdir:     t.TempDir(),
		Tick:        5 * time.Millisecond,
		writeIdleFn: func(time.Time) (time.Duration, bool) { return time.Hour, true },
		OnStateChange: func(s HealthState, _ string) {
			lastState.Store(int32(s))
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := hc.Run(ctx, exited); err != nil {
		t.Fatalf("Run err = %v, want nil (degraded, not killed)", err)
	}
	if HealthState(lastState.Load()) != Degraded {
		t.Fatalf("state = %v, want Degraded", HealthState(lastState.Load()))
	}
}

func TestHealthcheckWallClockKillsRegardless(t *testing.T) {
	t.Parallel()
	cmd, exited := startSleeper(t)

	// Clock returns start on first call, then far past the cap thereafter, so
	// the wall-clock signal trips even with every probe green.
	var calls atomic.Int64
	base := time.Now()
	hc := &Healthcheck{
		Cmd:         cmd,
		Workdir:     t.TempDir(),
		Tick:        5 * time.Millisecond,
		GracePeriod: 2 * time.Second,
		Policy:      agent.HealthPolicy{WallClockCap: 10 * time.Minute},
		Clock: func() time.Time {
			if calls.Add(1) == 1 {
				return base
			}
			return base.Add(30 * time.Minute)
		},
	}

	err := hc.Run(context.Background(), exited)
	var dead *DeadCoderError
	if !errors.As(err, &dead) {
		t.Fatalf("Run err = %v, want *DeadCoderError", err)
	}
	if len(dead.Signals) != 1 || dead.Signals[0] != signalWallClock {
		t.Fatalf("dead.Signals = %v, want [wall_clock]", dead.Signals)
	}
	<-exited
}

func TestHealthcheckSigkillEscalation(t *testing.T) {
	t.Parallel()
	// Every process in the group must ignore SIGTERM so the group-wide SIGTERM
	// does not reap it, forcing the SIGKILL escalation path. `sh` traps TERM and
	// loops on the `:` builtin (no killable child like `sleep` to receive the
	// group signal). The loop sleeps via `read -t` is unavailable in plain sh, so
	// a short subshell-free busy-pause keeps CPU bounded with `kill -0` polling.
	cmd, exited := startSleeper(t, "sh", "-c",
		"trap '' TERM; i=0; while [ $i -lt 600 ]; do sleep 0.1 & wait $!; i=$((i+1)); done")

	store := telemetry.NewHealthEventStore(filepath.Join(t.TempDir(), "h.jsonl"))
	hc := &Healthcheck{
		Cmd:         cmd,
		Workdir:     t.TempDir(),
		Tick:        5 * time.Millisecond,
		GracePeriod: 120 * time.Millisecond,
		Events:      store,
		writeIdleFn: func(time.Time) (time.Duration, bool) { return time.Hour, true },
		cpuIdleFn:   func(time.Time) (time.Duration, bool) { return time.Hour, true },
	}

	err := hc.Run(context.Background(), exited)
	var dead *DeadCoderError
	if !errors.As(err, &dead) {
		t.Fatalf("Run err = %v, want *DeadCoderError", err)
	}
	<-exited

	// The escalation must have recorded a sigkill_sent event, and the final
	// exited event must report an unclean (SIGKILL-forced) shutdown.
	events, rerr := store.Since(context.Background(), time.Time{})
	if rerr != nil {
		t.Fatalf("Since: %v", rerr)
	}
	var sawSigkill, sawUncleanExit bool
	for _, e := range events {
		if e.Event == telemetry.HealthEventSigkill {
			sawSigkill = true
		}
		if e.Event == telemetry.HealthEventExited && !e.Clean {
			sawUncleanExit = true
		}
	}
	if !sawSigkill {
		t.Errorf("no sigkill_sent event; SIGKILL escalation not exercised. events=%v", events)
	}
	if !sawUncleanExit {
		t.Errorf("expected unclean exited event after SIGKILL")
	}
}

func TestHealthcheckExitsWhenSubprocessExits(t *testing.T) {
	t.Parallel()
	// A short-lived process that exits on its own → Run returns nil promptly.
	cmd, exited := startSleeper(t, "sh", "-c", "exit 0")

	hc := &Healthcheck{
		Cmd:     cmd,
		Workdir: t.TempDir(),
		Tick:    time.Second, // long; the exited channel should win
	}
	done := make(chan error, 1)
	go func() { done <- hc.Run(context.Background(), exited) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run err = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after subprocess exit")
	}
}

// TestHealthcheckOnStateChangeSingleGoroutine asserts the callback is never
// invoked concurrently: it runs only on Run's single ticker goroutine.
func TestHealthcheckOnStateChangeSingleGoroutine(t *testing.T) {
	t.Parallel()
	cmd, exited := startSleeper(t)
	defer func() {
		_ = signalSubprocess(cmd, killSignal())
		<-exited
	}()

	var mu sync.Mutex
	inCallback := false
	var raced bool
	hc := &Healthcheck{
		Cmd:     cmd,
		Workdir: t.TempDir(),
		Tick:    time.Millisecond,
		// Flip-flop the signal so the state changes repeatedly.
		writeIdleFn: func(now time.Time) (time.Duration, bool) {
			if now.UnixNano()%2 == 0 {
				return time.Hour, true
			}
			return 0, true
		},
		OnStateChange: func(HealthState, string) {
			mu.Lock()
			if inCallback {
				raced = true
			}
			inCallback = true
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			inCallback = false
			mu.Unlock()
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_ = hc.Run(ctx, exited)
	if raced {
		t.Fatal("OnStateChange was invoked concurrently")
	}
}
