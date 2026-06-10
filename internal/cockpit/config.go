// Package cockpit serves Quasar's browser-based fleet dashboard: a server-
// rendered (templ), real-time (SSE/Datastar) ops console that mirrors the TUI
// fleet view. It is additive to the TUI, off by default, and embedded behind a
// build tag so a binary without the cockpit carries none of its assets.
package cockpit

// Config is the cockpit's slice of .quasar.yaml ([cockpit]). The cockpit is
// disabled by default so a binary built with the cockpit tag still serves no UI
// until an operator opts in.
type Config struct {
	Enabled bool   `mapstructure:"enabled"`
	Addr    string `mapstructure:"addr"`
}

// DefaultConfig returns the cockpit defaults: disabled, bound to loopback.
func DefaultConfig() Config {
	return Config{Enabled: false, Addr: "127.0.0.1:7330"}
}
