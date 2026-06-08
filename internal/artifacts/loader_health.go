package artifacts

import (
	"fmt"
	"time"
)

// constellationFile is the TOML decode target for a constellation definition.
type constellationFile struct {
	Name        string         `toml:"name"`
	Description string         `toml:"description"`
	Meta        map[string]any `toml:"meta"`
	Nodes       []struct {
		ID     string            `toml:"id"`
		Type   string            `toml:"type"`
		Star   string            `toml:"star"`
		Ref    string            `toml:"ref"`
		Op     string            `toml:"op"`
		Inputs map[string]string `toml:"inputs"`
	} `toml:"nodes"`
	Edges []struct {
		From string `toml:"from"`
		To   string `toml:"to"`
		When string `toml:"when"`
	} `toml:"edges"`
	Outputs map[string]string `toml:"outputs"`
}

// sensorFile is the TOML decode target for a sensor instance definition.
type sensorFile struct {
	Name         string         `toml:"name"`
	Type         string         `toml:"type"`
	PollInterval string         `toml:"poll_interval"`
	MaxInflight  int            `toml:"max_inflight"`
	Config       map[string]any `toml:"config"`
	Triggers     []struct {
		Constellation string `toml:"constellation"`
		When          string `toml:"when"`
	} `toml:"triggers"`
}

// parseStarHealth converts the string-valued duration fields of a star's
// [health] block into a typed StarHealthPolicy, reporting a clear error for any
// malformed duration so a typo fails loudly at load rather than silently
// disabling the healthcheck.
func parseStarHealth(sf starFrontmatter, src string) (StarHealthPolicy, error) {
	h := StarHealthPolicy{
		TokenRateFloor:       sf.Health.TokenRateFloor,
		ToolCallRatioCeiling: sf.Health.ToolCallRatioCeiling,
		ToolCallWindow:       sf.Health.ToolCallWindow,
	}
	for _, f := range []struct {
		name string
		raw  string
		dst  *time.Duration
	}{
		{"wall_clock_cap", sf.Health.WallClockCap, &h.WallClockCap},
		{"file_write_idle_cap", sf.Health.FileWriteIdleCap, &h.FileWriteIdleCap},
		{"token_rate_window", sf.Health.TokenRateWindow, &h.TokenRateWindow},
		{"cpu_idle_cap", sf.Health.CPUIdleCap, &h.CPUIdleCap},
	} {
		if f.raw == "" {
			continue
		}
		d, err := time.ParseDuration(f.raw)
		if err != nil {
			return StarHealthPolicy{}, fmt.Errorf("%s: invalid [health] %s %q: %w", src, f.name, f.raw, err)
		}
		*f.dst = d
	}
	return h, nil
}

// parseStarCheckpoint converts a star's [checkpoint] block into a typed
// StarCheckpointPolicy. An absent enabled key defaults to true (checkpointing is
// opt-out).
func parseStarCheckpoint(sf starFrontmatter) StarCheckpointPolicy {
	enabled := true
	if sf.Checkpoint.Enabled != nil {
		enabled = *sf.Checkpoint.Enabled
	}
	return StarCheckpointPolicy{Enabled: enabled}
}
