package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// GCConfig is the [gc] block of .quasar.yaml. Garbage collection is global, not
// per-repo: the SQLite database the GC reaps is the single store shared across
// every registered repo, so its TTLs live in the global config rather than any
// one repo's file. Durations are parsed from strings like "168h" by viper's
// built-in StringToTimeDuration decode hook.
type GCConfig struct {
	// Enabled gates the background sweeper. When false, `gc.Engine.Run` returns
	// immediately; one-shot `quasar gc run` still works regardless.
	Enabled bool `mapstructure:"enabled"`

	// TickInterval is how often the background sweeper wakes to mark and sweep
	// the row categories.
	TickInterval time.Duration `mapstructure:"tick_interval"`

	// GraceWindow is how long after a row is soft-deleted (deleted_at stamped)
	// before it becomes eligible for hard deletion, leaving a recovery window.
	GraceWindow time.Duration `mapstructure:"grace_window"`

	// TTLs holds the per-category age thresholds after which a terminal row is
	// marked for deletion.
	TTLs GCTTLConfig `mapstructure:"ttls"`

	// Blobs configures the mark-and-sweep over the content-addressed blobstore,
	// which runs on its own slower cadence than the row sweep.
	Blobs GCBlobConfig `mapstructure:"blobs"`
}

// GCTTLConfig holds the time-to-live for each GC-able category, measured from
// the row's terminal-transition timestamp.
type GCTTLConfig struct {
	CompletedNebulas     time.Duration `mapstructure:"completed_nebulas"`
	FailedNebulas        time.Duration `mapstructure:"failed_nebulas"`
	ConstellationRuns    time.Duration `mapstructure:"constellation_runs"`
	SensorEvents         time.Duration `mapstructure:"sensor_events"`
	TriggerQueueConsumed time.Duration `mapstructure:"trigger_queue_consumed"`
	AuditLog             time.Duration `mapstructure:"audit_log"`
}

// GCBlobConfig configures the blob mark-and-sweep.
type GCBlobConfig struct {
	// SweepInterval is how often the (expensive) blob mark-and-sweep runs.
	SweepInterval time.Duration `mapstructure:"sweep_interval"`

	// MinAgeBeforeSweep protects blobs newer than this from being reaped — a
	// just-written blob may have an in-flight reference not yet committed.
	MinAgeBeforeSweep time.Duration `mapstructure:"min_age_before_sweep"`
}

// DefaultGCConfig returns the conservative built-in GC settings. These match the
// viper defaults registered by setGCDefaults so "no [gc] block" and "empty [gc]
// block" converge on identical behavior.
func DefaultGCConfig() GCConfig {
	return GCConfig{
		Enabled:      true,
		TickInterval: time.Hour,
		GraceWindow:  24 * time.Hour,
		TTLs: GCTTLConfig{
			CompletedNebulas:     168 * time.Hour, // 7d
			FailedNebulas:        720 * time.Hour, // 30d
			ConstellationRuns:    168 * time.Hour, // 7d
			SensorEvents:         720 * time.Hour, // 30d
			TriggerQueueConsumed: 24 * time.Hour,
			AuditLog:             8760 * time.Hour, // 1y
		},
		Blobs: GCBlobConfig{
			SweepInterval:     24 * time.Hour,
			MinAgeBeforeSweep: time.Hour,
		},
	}
}

// setGCDefaults registers the GC built-in defaults on v. Called from setDefaults
// so Load() and LoadFromPath() stay in sync.
func setGCDefaults(v *viper.Viper) {
	d := DefaultGCConfig()
	v.SetDefault("gc.enabled", d.Enabled)
	v.SetDefault("gc.tick_interval", d.TickInterval)
	v.SetDefault("gc.grace_window", d.GraceWindow)
	v.SetDefault("gc.ttls.completed_nebulas", d.TTLs.CompletedNebulas)
	v.SetDefault("gc.ttls.failed_nebulas", d.TTLs.FailedNebulas)
	v.SetDefault("gc.ttls.constellation_runs", d.TTLs.ConstellationRuns)
	v.SetDefault("gc.ttls.sensor_events", d.TTLs.SensorEvents)
	v.SetDefault("gc.ttls.trigger_queue_consumed", d.TTLs.TriggerQueueConsumed)
	v.SetDefault("gc.ttls.audit_log", d.TTLs.AuditLog)
	v.SetDefault("gc.blobs.sweep_interval", d.Blobs.SweepInterval)
	v.SetDefault("gc.blobs.min_age_before_sweep", d.Blobs.MinAgeBeforeSweep)
}

// Validate rejects nonsensical GC settings. A zero or negative tick interval or
// grace window would make the sweeper spin or hard-delete with no recovery
// window; a non-positive TTL would delete terminal rows the instant they land.
func (c GCConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.TickInterval <= 0 {
		return fmt.Errorf("config: gc.tick_interval must be positive, got %s", c.TickInterval)
	}
	if c.GraceWindow < 0 {
		return fmt.Errorf("config: gc.grace_window must not be negative, got %s", c.GraceWindow)
	}
	if c.Blobs.SweepInterval <= 0 {
		return fmt.Errorf("config: gc.blobs.sweep_interval must be positive, got %s", c.Blobs.SweepInterval)
	}
	if c.Blobs.MinAgeBeforeSweep < 0 {
		return fmt.Errorf("config: gc.blobs.min_age_before_sweep must not be negative, got %s", c.Blobs.MinAgeBeforeSweep)
	}
	ttls := map[string]time.Duration{
		"completed_nebulas":      c.TTLs.CompletedNebulas,
		"failed_nebulas":         c.TTLs.FailedNebulas,
		"constellation_runs":     c.TTLs.ConstellationRuns,
		"sensor_events":          c.TTLs.SensorEvents,
		"trigger_queue_consumed": c.TTLs.TriggerQueueConsumed,
		"audit_log":              c.TTLs.AuditLog,
	}
	for name, ttl := range ttls {
		if ttl < 0 {
			return fmt.Errorf("config: gc.ttls.%s must not be negative, got %s", name, ttl)
		}
	}
	return nil
}
