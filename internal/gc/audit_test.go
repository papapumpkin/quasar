package gc

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// fixedClock returns a clock function that always reports t.
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestAuditLogAppend(t *testing.T) {
	t.Parallel()

	t.Run("writes one JSON line per entry with injected timestamp", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		ts := time.Date(2026, 6, 4, 22, 0, 0, 0, time.UTC)
		log := NewAuditLog(&buf, fixedClock(ts))

		if err := log.Append(AuditEntry{Category: CategoryCompletedNebulas, Action: ActionMark, NebulaID: "abc", Reason: "ttl_expired"}); err != nil {
			t.Fatalf("Append: %v", err)
		}
		if err := log.Append(AuditEntry{Category: CategoryBlobs, Action: ActionSweep, Hash: "deadbeef", SizeBytes: 4096}); err != nil {
			t.Fatalf("Append: %v", err)
		}

		lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
		if len(lines) != 2 {
			t.Fatalf("got %d lines, want 2: %q", len(lines), buf.String())
		}

		var first AuditEntry
		if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
			t.Fatalf("unmarshal line 0: %v", err)
		}
		if first.TS != "2026-06-04T22:00:00Z" {
			t.Errorf("TS = %q, want injected clock value", first.TS)
		}
		if first.NebulaID != "abc" || first.Action != ActionMark {
			t.Errorf("entry = %+v, want nebula abc / mark", first)
		}
	})

	t.Run("preserves a caller-supplied timestamp", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		log := NewAuditLog(&buf, fixedClock(time.Unix(0, 0)))
		if err := log.Append(AuditEntry{TS: "2020-01-01T00:00:00Z", Category: "x", Action: ActionReap}); err != nil {
			t.Fatalf("Append: %v", err)
		}
		if !strings.Contains(buf.String(), "2020-01-01T00:00:00Z") {
			t.Errorf("caller TS not preserved: %q", buf.String())
		}
	})

	t.Run("omits zero-value fields", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		log := NewAuditLog(&buf, fixedClock(time.Unix(0, 0)))
		if err := log.Append(AuditEntry{Category: "x", Action: ActionMark}); err != nil {
			t.Fatalf("Append: %v", err)
		}
		// omitempty keeps the line compact: no nebula_id / hash / dry_run keys.
		for _, absent := range []string{"nebula_id", "hash", "dry_run", "size_bytes"} {
			if strings.Contains(buf.String(), absent) {
				t.Errorf("expected %q to be omitted, line = %q", absent, buf.String())
			}
		}
	})

	t.Run("nil log is a no-op", func(t *testing.T) {
		t.Parallel()
		var log *AuditLog
		if err := log.Append(AuditEntry{Category: "x", Action: ActionMark}); err != nil {
			t.Errorf("nil Append: %v", err)
		}
	})

	t.Run("is safe for concurrent use", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		log := NewAuditLog(&buf, fixedClock(time.Unix(0, 0)))
		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = log.Append(AuditEntry{Category: "x", Action: ActionMark})
			}()
		}
		wg.Wait()
		lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
		if len(lines) != 50 {
			t.Errorf("got %d lines, want 50 (interleaved writes torn)", len(lines))
		}
	})
}

func TestReadAuditSince(t *testing.T) {
	t.Parallel()

	mk := func(ts string) string {
		b, _ := json.Marshal(AuditEntry{TS: ts, Category: "x", Action: ActionMark})
		return string(b)
	}

	t.Run("filters entries older than the cutoff", func(t *testing.T) {
		t.Parallel()
		log := strings.Join([]string{
			mk("2026-06-01T00:00:00Z"),
			mk("2026-06-05T00:00:00Z"),
			mk("2026-06-06T00:00:00Z"),
		}, "\n")
		since := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
		got, err := ReadAuditSince(strings.NewReader(log), since)
		if err != nil {
			t.Fatalf("ReadAuditSince: %v", err)
		}
		// The entry exactly at `since` is included (>= semantics).
		if len(got) != 2 {
			t.Fatalf("got %d entries, want 2", len(got))
		}
	})

	t.Run("tolerates a torn trailing line and blank lines", func(t *testing.T) {
		t.Parallel()
		log := mk("2026-06-06T00:00:00Z") + "\n\n" + `{"ts":"broken`
		got, err := ReadAuditSince(strings.NewReader(log), time.Unix(0, 0))
		if err != nil {
			t.Fatalf("ReadAuditSince: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("got %d entries, want 1 (torn line skipped)", len(got))
		}
	})

	t.Run("skips entries with an unparseable timestamp", func(t *testing.T) {
		t.Parallel()
		log := `{"ts":"not-a-time","category":"x","action":"mark"}`
		got, err := ReadAuditSince(strings.NewReader(log), time.Unix(0, 0))
		if err != nil {
			t.Fatalf("ReadAuditSince: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("got %d entries, want 0", len(got))
		}
	})
}
