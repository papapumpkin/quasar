package claude

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

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

// Default healthcheck policy values. Conservative by design: a 25-minute
// wall-clock cap means a coder doing legitimate work is never killed early,
// while a stalled one is reclaimed long before the 60–90 minute silent burns
// this detector exists to prevent.
const (
	DefaultWallClockCap     = 25 * time.Minute
	DefaultFileWriteIdleCap = 5 * time.Minute
	DefaultTokenRateFloor   = 5.0
	DefaultTokenRateWindow  = 60 * time.Second
	DefaultToolRatioCeiling = 12.0
	DefaultToolCallWindow   = 20
	DefaultCPUIdleCap       = 90 * time.Second
	DefaultTick             = 5 * time.Second
	// terminateGrace is how long we wait after SIGTERM before escalating to
	// SIGKILL.
	terminateGrace = 5 * time.Second
)

// HealthPolicy is the set of thresholds the healthcheck evaluates each tick.
// The TOML tags allow a star's [health] frontmatter block (or a nebula's
// [execution] block) to override the defaults.
type HealthPolicy struct {
	// WallClockCap is the absolute upper bound on subprocess lifetime.
	WallClockCap time.Duration `toml:"wall_clock_cap"`
	// FileWriteIdleCap is the longest stretch without a write under the
	// workdir (excluding .git, node_modules) before the coder is stalled.
	FileWriteIdleCap time.Duration `toml:"file_write_idle_cap"`
	// TokenRateFloor is the minimum stream-token rate (tokens/sec averaged
	// over TokenRateWindow) below which the coder is "stuck reasoning".
	TokenRateFloor float64 `toml:"token_rate_floor"`
	// TokenRateWindow is the averaging window for the token-rate signal.
	TokenRateWindow time.Duration `toml:"token_rate_window"`
	// ToolCallRatioCeiling is the max Read:Edit ratio over the last
	// ToolCallWindow tool calls before the coder is in an "explore loop".
	ToolCallRatioCeiling float64 `toml:"tool_call_ratio_ceiling"`
	// ToolCallWindow is how many recent tool calls feed the ratio signal.
	ToolCallWindow int `toml:"tool_call_window"`
	// CPUIdleCap is the longest stretch the subprocess can sit below 1% CPU
	// before it is declared hung.
	CPUIdleCap time.Duration `toml:"cpu_idle_cap"`
}

// DefaultHealthPolicy returns the conservative built-in policy.
func DefaultHealthPolicy() HealthPolicy {
	return HealthPolicy{
		WallClockCap:         DefaultWallClockCap,
		FileWriteIdleCap:     DefaultFileWriteIdleCap,
		TokenRateFloor:       DefaultTokenRateFloor,
		TokenRateWindow:      DefaultTokenRateWindow,
		ToolCallRatioCeiling: DefaultToolRatioCeiling,
		ToolCallWindow:       DefaultToolCallWindow,
		CPUIdleCap:           DefaultCPUIdleCap,
	}
}

// withDefaults returns p with any zero-valued field filled from the defaults,
// so a partial override (e.g. only wall_clock_cap set) keeps sane values for
// the rest.
func (p HealthPolicy) withDefaults() HealthPolicy {
	d := DefaultHealthPolicy()
	if p.WallClockCap <= 0 {
		p.WallClockCap = d.WallClockCap
	}
	if p.FileWriteIdleCap <= 0 {
		p.FileWriteIdleCap = d.FileWriteIdleCap
	}
	if p.TokenRateFloor <= 0 {
		p.TokenRateFloor = d.TokenRateFloor
	}
	if p.TokenRateWindow <= 0 {
		p.TokenRateWindow = d.TokenRateWindow
	}
	if p.ToolCallRatioCeiling <= 0 {
		p.ToolCallRatioCeiling = d.ToolCallRatioCeiling
	}
	if p.ToolCallWindow <= 0 {
		p.ToolCallWindow = d.ToolCallWindow
	}
	if p.CPUIdleCap <= 0 {
		p.CPUIdleCap = d.CPUIdleCap
	}
	return p
}

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

// evaluate is the pure decision function: given a snapshot it returns the
// classification, a human-readable reason, and the list of red signal names.
// Wall-clock is special-cased — it kills regardless of any other signal.
func (p HealthPolicy) evaluate(s healthSnapshot) (HealthState, string, []float64Signal) {
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

	Policy        HealthPolicy
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
	policy := h.Policy.withDefaults()
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
			state, reason, reds := policy.evaluate(h.snapshot(start, now))

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
		Reason:         reason,
		Signals:        names,
		Elapsed:        elapsed.String(),
		PartialWorkdir: h.Workdir,
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
