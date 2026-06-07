package agent

import "time"

// Default healthcheck policy thresholds. Conservative by design: a 25-minute
// wall-clock cap never kills legitimate work early while still reclaiming the
// 60–90 minute silent burns the dead-coder detector exists to prevent.
const (
	DefaultWallClockCap     = 25 * time.Minute
	DefaultFileWriteIdleCap = 5 * time.Minute
	DefaultTokenRateFloor   = 5.0
	DefaultTokenRateWindow  = 60 * time.Second
	DefaultToolRatioCeiling = 12.0
	DefaultToolCallWindow   = 20
	DefaultCPUIdleCap       = 90 * time.Second
)

// HealthPolicy is the per-invocation/per-star configuration for the dead-coder
// healthcheck: the thresholds the claude invoker evaluates each tick to decide
// whether a subprocess is stalled or thrashing. It mirrors ContextBudget — the
// type lives here (consumed by the invoker), and a star's [health] TOML block
// is converted into it before reaching the invoker. A nil *HealthPolicy on an
// Agent means "no per-invocation override"; the invoker may still apply its own
// default policy.
type HealthPolicy struct {
	// WallClockCap is the absolute upper bound on subprocess lifetime.
	WallClockCap time.Duration
	// FileWriteIdleCap is the longest stretch without a write under the workdir
	// (excluding .git, node_modules) before the coder is considered stalled.
	FileWriteIdleCap time.Duration
	// TokenRateFloor is the minimum stream-token rate (tokens/sec averaged over
	// TokenRateWindow) below which the coder is "stuck reasoning".
	TokenRateFloor float64
	// TokenRateWindow is the averaging window for the token-rate signal.
	TokenRateWindow time.Duration
	// ToolCallRatioCeiling is the max Read:Edit ratio over the last
	// ToolCallWindow tool calls before the coder is in an "explore loop".
	ToolCallRatioCeiling float64
	// ToolCallWindow is how many recent tool calls feed the ratio signal.
	ToolCallWindow int
	// CPUIdleCap is the longest stretch the subprocess can sit below 1% CPU
	// before it is declared hung.
	CPUIdleCap time.Duration
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

// WithDefaults returns p with any zero-valued field filled from the defaults,
// so a partial override (e.g. only WallClockCap set) keeps sane values for the
// rest.
func (p HealthPolicy) WithDefaults() HealthPolicy {
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
