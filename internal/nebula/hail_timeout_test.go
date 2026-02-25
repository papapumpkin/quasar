package nebula

import (
	"testing"
	"time"
)

func TestExecution_ParsedHailTimeout(t *testing.T) {
	t.Parallel()

	t.Run("empty string returns default", func(t *testing.T) {
		t.Parallel()
		e := Execution{}
		got := e.ParsedHailTimeout()
		if got != DefaultHailTimeout {
			t.Errorf("ParsedHailTimeout() = %v, want %v", got, DefaultHailTimeout)
		}
	})

	t.Run("zero disables timeout", func(t *testing.T) {
		t.Parallel()
		e := Execution{HailTimeout: "0"}
		got := e.ParsedHailTimeout()
		if got != 0 {
			t.Errorf("ParsedHailTimeout() = %v, want 0", got)
		}
	})

	t.Run("valid duration string", func(t *testing.T) {
		t.Parallel()
		e := Execution{HailTimeout: "10m"}
		got := e.ParsedHailTimeout()
		want := 10 * time.Minute
		if got != want {
			t.Errorf("ParsedHailTimeout() = %v, want %v", got, want)
		}
	})

	t.Run("valid sub-minute duration", func(t *testing.T) {
		t.Parallel()
		e := Execution{HailTimeout: "30s"}
		got := e.ParsedHailTimeout()
		want := 30 * time.Second
		if got != want {
			t.Errorf("ParsedHailTimeout() = %v, want %v", got, want)
		}
	})

	t.Run("valid compound duration", func(t *testing.T) {
		t.Parallel()
		e := Execution{HailTimeout: "1h30m"}
		got := e.ParsedHailTimeout()
		want := 90 * time.Minute
		if got != want {
			t.Errorf("ParsedHailTimeout() = %v, want %v", got, want)
		}
	})

	t.Run("invalid duration returns default", func(t *testing.T) {
		t.Parallel()
		e := Execution{HailTimeout: "not-a-duration"}
		got := e.ParsedHailTimeout()
		if got != DefaultHailTimeout {
			t.Errorf("ParsedHailTimeout() = %v, want %v (default for invalid)", got, DefaultHailTimeout)
		}
	})

	t.Run("default timeout is 5 minutes", func(t *testing.T) {
		t.Parallel()
		if DefaultHailTimeout != 5*time.Minute {
			t.Errorf("DefaultHailTimeout = %v, want 5m", DefaultHailTimeout)
		}
	})
}
