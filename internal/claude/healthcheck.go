package claude

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/telemetry"
)

// HealthState is the coarse health classification of a running coder
// subprocess, derived from the multi-signal probe each tick.
type HealthState int

const (
	// Healthy means no signal is red.
	Healthy HealthState = iota
	// Degraded means exactly one signal is red — operator-visible, but the
	// coder may still recover, so it is not killed.
	Degraded
	// Dead means two or more signals are red, or the wall-clock cap was hit.
	// A Dead coder is terminated.
	Dead
)

// String renders the state for logs and telemetry.
func (s HealthState) String() string {
	switch s {
	case Healthy:
		return "healthy"
	case Degraded:
		return "degraded"
	case Dead:
		return "dead"
	default:
		return "unknown"
	}
}

// Signal names, used in telemetry and DeadCoderError.
const (
	signalWallClock = "wall_clock"
	signalWriteIdle = "write_idle"
	signalTokenRate = "token_rate"
	signalToolRatio = "tool_ratio"
	signalCPUIdle   = "cpu_idle"
)

// Runtime cadence constants for the healthcheck loop. The policy *thresholds*
// (wall-clock cap, idle caps, etc.) live in the agent package as
// agent.HealthPolicy; these two govern how often the probe runs and how long
// termination waits, which are invoker-internal mechanics, not policy.
const (
	// DefaultTick is the probe interval.
	DefaultTick = 5 * time.Second
	// terminateGrace is how long we wait after SIGTERM before escalating to
	// SIGKILL.
	terminateGrace = 5 * time.Second
)

// healthSnapshot is the set of instantaneous signal readings evaluated each
// tick. A signal with valid=false has no data yet (e.g. the token meter before
// the first event) and is excluded from the decision so an unwarmed signal
// never causes a false kill.
type healthSnapshot struct {
	elapsed time.Duration

	writeIdle  time.Duration
	writeValid bool

	tokenRate  float64
	tokenValid bool

	readEditRatio float64
	ratioValid    bool

	cpuIdle  time.Duration
	cpuValid bool
}

// evaluateHealth is the pure decision function: given a policy and a snapshot it
// returns the classification, a human-readable reason, and the red signals.
// Wall-clock is special-cased — it kills regardless of any other signal.
func evaluateHealth(p agent.HealthPolicy, s healthSnapshot) (HealthState, string, []float64Signal) {
	if s.elapsed > p.WallClockCap {
		return Dead, "wall-clock cap exceeded", []float64Signal{{Name: signalWallClock, Value: s.elapsed.Seconds()}}
	}

	var red []float64Signal
	if s.writeValid && s.writeIdle > p.FileWriteIdleCap {
		red = append(red, float64Signal{Name: signalWriteIdle, Value: s.writeIdle.Seconds()})
	}
	if s.tokenValid && s.tokenRate < p.TokenRateFloor {
		red = append(red, float64Signal{Name: signalTokenRate, Value: s.tokenRate})
	}
	if s.ratioValid && s.readEditRatio > p.ToolCallRatioCeiling {
		red = append(red, float64Signal{Name: signalToolRatio, Value: s.readEditRatio})
	}
	if s.cpuValid && s.cpuIdle > p.CPUIdleCap {
		red = append(red, float64Signal{Name: signalCPUIdle, Value: s.cpuIdle.Seconds()})
	}

	switch len(red) {
	case 0:
		return Healthy, "", nil
	case 1:
		return Degraded, fmt.Sprintf("signal red: %s", red[0].Name), red
	default:
		return Dead, "two or more signals red: " + signalNames(red), red
	}
}

// float64Signal pairs a red signal's name with its measured value for telemetry.
type float64Signal struct {
	Name  string
	Value float64
}

// signalNames joins the signal names for a combined reason string.
func signalNames(sigs []float64Signal) string {
	names := make([]string, len(sigs))
	for i, s := range sigs {
		names[i] = s.Name
	}
	return strings.Join(names, ", ")
}

// Healthcheck monitors a claude subprocess via the multi-signal probe and
// terminates it when it goes Dead. Run executes in the caller's goroutine
// alongside the subprocess; all state transitions (and the OnStateChange
// callback) happen on that single goroutine, so the callback is never invoked
// concurrently.
type Healthcheck struct {
	Cmd     *exec.Cmd
	Workdir string
	// Clock supplies the current time; injectable for deterministic tests.
	Clock func() time.Time
	// Tick is the probe interval. Zero uses DefaultTick.
	Tick time.Duration

	Policy        agent.HealthPolicy
	OnStateChange func(s HealthState, reason string)

	// GracePeriod is how long to wait after SIGTERM before escalating to
	// SIGKILL. Zero uses terminateGrace.
	GracePeriod time.Duration

	// InvocationID labels telemetry records.
	InvocationID string
	// Events, when non-nil, receives a record for every state transition and
	// every step of the termination handshake.
	Events *telemetry.HealthEventStore

	// Signal probes. The Invoker wires concrete implementations; tests inject
	// fakes. A nil probe yields valid=false (signal ignored).
	writeIdleFn func(now time.Time) (time.Duration, bool)
	tokenRateFn func(now time.Time) (float64, bool)
	toolRatioFn func() (float64, bool)
	cpuIdleFn   func(now time.Time) (time.Duration, bool)

	mu    sync.Mutex
	state HealthState
}

// clock returns the configured clock or time.Now.
func (h *Healthcheck) clock() time.Time {
	if h.Clock != nil {
		return h.Clock()
	}
	return time.Now()
}

// State returns the last classified state. Safe for concurrent reads.
func (h *Healthcheck) State() HealthState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state
}

// snapshot gathers the current signal readings.
func (h *Healthcheck) snapshot(start, now time.Time) healthSnapshot {
	s := healthSnapshot{elapsed: now.Sub(start)}
	if h.writeIdleFn != nil {
		s.writeIdle, s.writeValid = h.writeIdleFn(now)
	}
	if h.tokenRateFn != nil {
		s.tokenRate, s.tokenValid = h.tokenRateFn(now)
	}
	if h.toolRatioFn != nil {
		s.readEditRatio, s.ratioValid = h.toolRatioFn()
	}
	if h.cpuIdleFn != nil {
		s.cpuIdle, s.cpuValid = h.cpuIdleFn(now)
	}
	return s
}

// Run blocks until the subprocess exits (signaled by exited being closed), the
// context is cancelled, or the coder is declared Dead and terminated. It
// returns a *DeadCoderError only in the last case; otherwise nil.
func (h *Healthcheck) Run(ctx context.Context, exited <-chan struct{}) error {
	policy := h.Policy.WithDefaults()
	tick := h.Tick
	if tick <= 0 {
		tick = DefaultTick
	}
	start := h.clock()

	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-exited:
			return nil
		case <-ticker.C:
			now := h.clock()
			state, reason, reds := evaluateHealth(policy, h.snapshot(start, now))

			h.mu.Lock()
			changed := state != h.state
			h.state = state
			h.mu.Unlock()

			if !changed {
				continue
			}
			if h.OnStateChange != nil {
				h.OnStateChange(state, reason)
			}

			switch state {
			case Degraded:
				h.recordDegraded(ctx, reds)
			case Dead:
				return h.terminate(ctx, exited, now.Sub(start), reason, reds)
			}
		}
	}
}

// recordDegraded logs the single red signal that caused a degrade.
func (h *Healthcheck) recordDegraded(ctx context.Context, reds []float64Signal) {
	if h.Events == nil || len(reds) == 0 {
		return
	}
	_ = h.Events.Record(ctx, telemetry.HealthEvent{
		InvocationID: h.InvocationID,
		Event:        telemetry.HealthEventDegraded,
		Signal:       reds[0].Name,
		Value:        reds[0].Value,
	})
}

// terminate runs the SIGTERM → 5s grace → SIGKILL handshake, records each step,
// snapshots the partial worktree, and returns a populated *DeadCoderError. It
// waits on exited (closed when the caller's cmd.Wait returns) to know when the
// subprocess is actually gone.
func (h *Healthcheck) terminate(ctx context.Context, exited <-chan struct{}, elapsed time.Duration, reason string, reds []float64Signal) error {
	names := make([]string, len(reds))
	for i, s := range reds {
		names[i] = s.Name
	}

	if h.Events != nil {
		_ = h.Events.Record(ctx, telemetry.HealthEvent{
			InvocationID: h.InvocationID,
			Event:        telemetry.HealthEventDead,
			Signals:      names,
			Elapsed:      elapsed.String(),
			Reason:       reason,
		})
	}

	clean := h.killWithGrace(ctx, exited)

	if h.Events != nil {
		_ = h.Events.Record(ctx, telemetry.HealthEvent{
			InvocationID: h.InvocationID,
			Event:        telemetry.HealthEventExited,
			Clean:        clean,
		})
	}

	return &DeadCoderError{
		Reason:  reason,
		Signals: names,
		Elapsed: elapsed.String(),
		Workdir: h.Workdir,
	}
}

// killWithGrace sends SIGTERM, waits up to terminateGrace for the process to
// exit, then escalates to SIGKILL. Returns true if the process exited within
// the grace period (clean) rather than requiring SIGKILL.
func (h *Healthcheck) killWithGrace(ctx context.Context, exited <-chan struct{}) bool {
	if err := signalSubprocess(h.Cmd, termSignal()); err == nil && h.Events != nil {
		_ = h.Events.Record(ctx, telemetry.HealthEvent{
			InvocationID: h.InvocationID,
			Event:        telemetry.HealthEventSigterm,
		})
	}

	grace := h.GracePeriod
	if grace <= 0 {
		grace = terminateGrace
	}
	select {
	case <-exited:
		return true
	case <-time.After(grace):
	}

	if err := signalSubprocess(h.Cmd, killSignal()); err == nil && h.Events != nil {
		_ = h.Events.Record(ctx, telemetry.HealthEvent{
			InvocationID: h.InvocationID,
			Event:        telemetry.HealthEventSigkill,
		})
	}
	<-exited
	return false
}
