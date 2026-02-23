package telemetry

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewEmitter_CreatesFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "events.jsonl")

	em, err := NewEmitter(path)
	if err != nil {
		t.Fatalf("NewEmitter(%q): %v", path, err)
	}
	defer em.Close()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist at %q: %v", path, err)
	}
}

func TestNewEmitter_ErrorOnBadPath(t *testing.T) {
	t.Parallel()
	_, err := NewEmitter("/nonexistent/dir/events.jsonl")
	if err == nil {
		t.Fatal("expected error for bad path, got nil")
	}
	if !strings.Contains(err.Error(), "telemetry: open") {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestEmit_WritesValidJSONL(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "events.jsonl")

	em, err := NewEmitter(path)
	if err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}

	events := []Event{
		{Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Kind: KindEpochStart, EpochID: "e1"},
		{Timestamp: time.Date(2025, 1, 1, 0, 1, 0, 0, time.UTC), Kind: KindTaskState, EpochID: "e1", TaskID: "t1", Data: map[string]string{"from": "queued", "to": "running"}},
		{Timestamp: time.Date(2025, 1, 1, 0, 2, 0, 0, time.UTC), Kind: KindEpochDone, EpochID: "e1"},
	}

	for _, evt := range events {
		if err := em.Emit(evt); err != nil {
			t.Fatalf("Emit: %v", err)
		}
	}
	if err := em.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read back and verify each line is valid JSON.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var decoded []Event
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var evt Event
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatalf("invalid JSON line: %v\nline: %s", err, line)
		}
		decoded = append(decoded, evt)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}

	if len(decoded) != len(events) {
		t.Fatalf("expected %d events, got %d", len(events), len(decoded))
	}
	for i, got := range decoded {
		if got.Kind != events[i].Kind {
			t.Errorf("event %d: kind=%q, want %q", i, got.Kind, events[i].Kind)
		}
		if got.EpochID != events[i].EpochID {
			t.Errorf("event %d: epoch=%q, want %q", i, got.EpochID, events[i].EpochID)
		}
	}
}

func TestEmit_ConcurrentSafety(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "concurrent.jsonl")

	em, err := NewEmitter(path)
	if err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			evt := Event{
				Timestamp: time.Now(),
				Kind:      KindAgentStart,
				TaskID:    "concurrent",
				Data:      map[string]int{"idx": idx},
			}
			if err := em.Emit(evt); err != nil {
				t.Errorf("Emit from goroutine %d: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	if err := em.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Verify all lines are valid JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != n {
		t.Fatalf("expected %d lines, got %d", n, len(lines))
	}
	for i, line := range lines {
		var evt Event
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Errorf("line %d: invalid JSON: %v", i, err)
		}
	}
}

func TestNilEmitter_NoOp(t *testing.T) {
	t.Parallel()
	var em *Emitter

	// Emit on nil should return nil.
	if err := em.Emit(Event{Kind: KindEpochStart}); err != nil {
		t.Errorf("nil Emit: %v", err)
	}
	// Close on nil should return nil.
	if err := em.Close(); err != nil {
		t.Errorf("nil Close: %v", err)
	}
}

func TestEmit_AppendsToExistingFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "append.jsonl")

	// Write first batch.
	em1, err := NewEmitter(path)
	if err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}
	if err := em1.Emit(Event{Kind: KindEpochStart, EpochID: "e1"}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	em1.Close()

	// Write second batch.
	em2, err := NewEmitter(path)
	if err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}
	if err := em2.Emit(Event{Kind: KindEpochDone, EpochID: "e1"}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	em2.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
}

func TestEventKinds_AreDistinct(t *testing.T) {
	t.Parallel()
	kinds := []string{
		KindEpochStart,
		KindEpochDone,
		KindTaskState,
		KindAgentStart,
		KindAgentDone,
		KindEntanglementPosted,
		KindClaimAcquired,
		KindClaimReleased,
		KindDiscoveryPosted,
		KindDiscoveryResolved,
		KindFilterResult,
		KindCycleStart,
		KindCycleDone,
	}
	seen := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		if k == "" {
			t.Errorf("empty kind constant found")
		}
		if seen[k] {
			t.Errorf("duplicate kind: %q", k)
		}
		seen[k] = true
	}
}

func TestEvent_OmitsEmptyFields(t *testing.T) {
	t.Parallel()
	evt := Event{
		Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Kind:      KindEpochStart,
	}
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(data)
	if strings.Contains(s, `"epoch"`) {
		t.Errorf("expected epoch to be omitted, got: %s", s)
	}
	if strings.Contains(s, `"task"`) {
		t.Errorf("expected task to be omitted, got: %s", s)
	}
	if strings.Contains(s, `"data"`) {
		t.Errorf("expected data to be omitted, got: %s", s)
	}
}
