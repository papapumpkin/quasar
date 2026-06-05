package repos

import (
	"errors"
	"testing"
)

func TestStatusConstants(t *testing.T) {
	got := []string{StatusActive, StatusPaused, StatusRemoved}
	want := []string{"active", "paused", "removed"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("status[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestErrorSentinelsAreDistinct(t *testing.T) {
	sentinels := []error{
		ErrRepoNotRegistered,
		ErrRepoAlreadyRegistered,
		ErrRepoActiveNebulas,
		ErrRepoPathInvalid,
		ErrSensorNotConfigured,
	}
	for i := range sentinels {
		for j := range sentinels {
			if i == j {
				continue
			}
			if errors.Is(sentinels[i], sentinels[j]) {
				t.Errorf("sentinel %d matches %d, want distinct", i, j)
			}
		}
	}
}

func TestEmbeddedPathSentinel(t *testing.T) {
	if EmbeddedPath != ":embedded:" {
		t.Errorf("EmbeddedPath = %q, want :embedded:", EmbeddedPath)
	}
}
