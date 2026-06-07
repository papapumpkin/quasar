package claude

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseTokenCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		line string
		want int
		ok   bool
	}{
		{"assistant event nested usage", `{"type":"assistant","message":{"usage":{"output_tokens":42}}}`, 42, true},
		{"top-level usage", `{"type":"result","usage":{"output_tokens":7}}`, 7, true},
		{"no usage", `{"type":"system","subtype":"init"}`, 0, false},
		{"zero tokens ignored", `{"message":{"usage":{"output_tokens":0}}}`, 0, false},
		{"not json", `not a json line`, 0, false},
		{"empty", ``, 0, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseTokenCount([]byte(tt.line))
			if got != tt.want || ok != tt.ok {
				t.Fatalf("parseTokenCount(%q) = (%d, %v), want (%d, %v)", tt.line, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestTokenRateMeter(t *testing.T) {
	t.Parallel()
	now := time.Now()
	clock := func() time.Time { return now }
	m := newTokenRateMeter(60*time.Second, clock)

	if _, ok := m.Rate(now); ok {
		t.Fatal("rate should be invalid before any observation")
	}

	// 600 tokens over a 60s window → 10 tokens/sec.
	m.Observe(300)
	m.Observe(300)
	rate, ok := m.Rate(now)
	if !ok {
		t.Fatal("rate should be valid after observations")
	}
	if rate != 10 {
		t.Fatalf("rate = %v, want 10 tokens/sec", rate)
	}

	// Advance past the window: old samples prune, rate falls to 0 (still valid).
	now = now.Add(2 * time.Minute)
	rate, ok = m.Rate(now)
	if !ok || rate != 0 {
		t.Fatalf("rate after window = (%v, %v), want (0, true)", rate, ok)
	}
}

func TestCPUPoller(t *testing.T) {
	t.Parallel()
	now := time.Now()
	clock := func() time.Time { return now }
	p := newCPUPoller(1234, clock)

	if _, ok := p.IdleSince(now); ok {
		t.Fatal("idle should be invalid before first sample")
	}

	// Active sample resets the idle timer.
	p.readPCPU = func(int) (float64, error) { return 50.0, nil }
	p.Poll()
	now = now.Add(time.Minute)
	idle, ok := p.IdleSince(now)
	if !ok {
		t.Fatal("idle should be valid after a sample")
	}
	if idle != time.Minute {
		t.Fatalf("idle = %v, want 1m since last active", idle)
	}

	// Idle samples do not reset the timer; idle keeps growing.
	p.readPCPU = func(int) (float64, error) { return 0.0, nil }
	p.Poll()
	now = now.Add(time.Minute)
	idle, _ = p.IdleSince(now)
	if idle != 2*time.Minute {
		t.Fatalf("idle = %v, want 2m", idle)
	}
}

func TestCPUPollerIgnoresErrors(t *testing.T) {
	t.Parallel()
	now := time.Now()
	p := newCPUPoller(1234, func() time.Time { return now })
	p.readPCPU = func(int) (float64, error) { return 0, errors.New("no such process") }
	p.Poll()
	if _, ok := p.IdleSince(now); ok {
		t.Fatal("a failed sample should not mark the poller as seen")
	}
}

func TestFileWriteWatcher(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fw, err := newFileWriteWatcher(dir, nil)
	if err != nil {
		t.Fatalf("newFileWriteWatcher: %v", err)
	}
	defer fw.Close()

	// A write resets the idle timer to ~0.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Poll for the event to be processed (fsnotify is async).
	deadline := time.Now().Add(2 * time.Second)
	for {
		idle, ok := fw.IdleSince(time.Now())
		if ok && idle < 200*time.Millisecond {
			break // write registered
		}
		if time.Now().After(deadline) {
			t.Fatalf("write event not observed; idle=%v", idle)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFileWriteWatcherExcludesGitDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fw, err := newFileWriteWatcher(dir, nil)
	if err != nil {
		t.Fatalf("newFileWriteWatcher: %v", err)
	}
	defer fw.Close()

	base, _ := fw.IdleSince(time.Now())
	// A write inside .git must NOT reset the idle timer.
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	after, _ := fw.IdleSince(time.Now())
	if after < base {
		t.Fatal("write under .git reset the idle timer but should be excluded")
	}
}
