package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestCloseIfCloserDiscardNoPanic pins the regression: the supervisor-log
// close path must tolerate the io.Discard fallback. io.Discard is non-nil but
// has no Close method, so a bare type assertion panicked here — exactly on the
// log-open failure the fallback exists to tolerate.
func TestCloseIfCloserDiscardNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("closeIfCloser(io.Discard) panicked: %v", r)
		}
	}()
	closeIfCloser(io.Discard)
}

// TestCloseIfCloserClosesFile confirms the close still happens for a real
// closable writer (an *os.File), so the comma-ok guard didn't disable cleanup.
func TestCloseIfCloserClosesFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "log")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	closeIfCloser(f)
	// A second Close on an already-closed file returns an error; that proves
	// closeIfCloser closed it the first time.
	if err := f.Close(); err == nil {
		t.Error("file was not closed by closeIfCloser")
	}
}

// TestOpenSupervisorLogFallsBackToDiscard verifies an unopenable log path
// degrades to io.Discard rather than nil, so the close path is exercised with
// the value that used to panic.
func TestOpenSupervisorLogFallsBackToDiscard(t *testing.T) {
	// A dbPath whose directory does not exist makes OpenFile fail.
	bad := filepath.Join(t.TempDir(), "missing-subdir", "fabric.db")
	w := openSupervisorLog(bad)
	if w != io.Discard {
		t.Fatalf("openSupervisorLog(%q) = %v, want io.Discard fallback", bad, w)
	}
}
