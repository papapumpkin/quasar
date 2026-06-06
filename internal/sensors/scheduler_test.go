package sensors

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/artifacts"
)

// scriptedSensor is a scriptable Sensor. Poll returns the queued batch and
// cursor; SeedNebula renders content from the event's Raw map.
type scriptedSensor struct {
	events    []Event
	newCursor json.RawMessage
	pollErr   error
	pollCalls int
}

func (f *scriptedSensor) Name() string { return "fake" }

func (f *scriptedSensor) Configure(map[string]any, SecretResolver) error { return nil }

func (f *scriptedSensor) Poll(context.Context, json.RawMessage) ([]Event, json.RawMessage, error) {
	f.pollCalls++
	if f.pollErr != nil {
		return nil, nil, f.pollErr
	}
	return f.events, f.newCursor, nil
}

func (f *scriptedSensor) SeedNebula(ev Event) (*SeedNebulaContent, error) {
	return &SeedNebulaContent{
		Name:        rawStr(ev.Raw, "title"),
		Description: rawStr(ev.Raw, "body"),
		SourceName:  "github",
		SourceID:    ev.ExternalID,
		SourceURL:   rawStr(ev.Raw, "url"),
		Goals:       []string{"goal-" + ev.ExternalID},
		Constraints: []string{"constraint-" + ev.ExternalID},
	}, nil
}

func rawStr(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

// fakeCursorStore is an in-memory CursorStore.
type fakeCursorStore struct {
	cursor json.RawMessage
	sets   int
}

func (s *fakeCursorStore) Get(context.Context, string, string) (json.RawMessage, error) {
	return s.cursor, nil
}

func (s *fakeCursorStore) Set(_ context.Context, _, _ string, c json.RawMessage) error {
	s.cursor = c
	s.sets++
	return nil
}

// fakeEventRow tracks one observed event in the fake store.
type fakeEventRow struct {
	id         int64
	externalID string
	processed  bool
}

// fakeEventStore is an in-memory EventStore with (externalID) dedup and
// unprocessed tracking, mirroring the fabric semantics the scheduler relies on.
type fakeEventStore struct {
	mu     sync.Mutex
	rows   []*fakeEventRow
	byID   map[int64]*fakeEventRow
	byExt  map[string]*fakeEventRow
	nextID int64
}

func newFakeEventStore() *fakeEventStore {
	return &fakeEventStore{byID: map[int64]*fakeEventRow{}, byExt: map[string]*fakeEventRow{}}
}

func (s *fakeEventStore) Insert(_ context.Context, _, _, externalID string, _ time.Time) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.byExt[externalID]; ok {
		return r.id, false, nil
	}
	s.nextID++
	r := &fakeEventRow{id: s.nextID, externalID: externalID}
	s.rows = append(s.rows, r)
	s.byID[r.id] = r
	s.byExt[externalID] = r
	return r.id, true, nil
}

func (s *fakeEventStore) MarkProcessed(_ context.Context, id int64, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.byID[id]
	if !ok {
		return fmt.Errorf("fake: no event %d", id)
	}
	r.processed = true
	return nil
}

func (s *fakeEventStore) UnprocessedExternalIDs(context.Context, string, string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for _, r := range s.rows {
		if !r.processed {
			ids = append(ids, r.externalID)
		}
	}
	return ids, nil
}

// seedInsertWithoutProcessing records an observed-but-unseeded event, simulating
// a tick that crashed between Insert and MarkProcessed.
func (s *fakeEventStore) seedInsertWithoutProcessing(externalID string) {
	_, _, _ = s.Insert(context.Background(), "", "", externalID, time.Time{})
}

// fakeInserter records the seed nebulas it is asked to insert.
type fakeInserter struct {
	inserted []SeedNebula
}

func (f *fakeInserter) Insert(_ context.Context, n SeedNebula) (string, error) {
	f.inserted = append(f.inserted, n)
	return fmt.Sprintf("nebula-%d", len(f.inserted)), nil
}

func event(extID, title string) Event {
	return Event{ExternalID: extID, Raw: map[string]any{"title": title, "url": "https://x/" + extID}}
}

// newTestScheduler wires a scheduler over the fakes with sane defaults.
func newTestScheduler(t *testing.T, opts SchedulerOpts) *Scheduler {
	t.Helper()
	if opts.RepoPath == "" {
		opts.RepoPath = "/repo"
	}
	if opts.Instance == nil {
		opts.Instance = &artifacts.SensorInstance{Name: "fake", PollInterval: time.Minute}
	}
	s, err := NewScheduler(opts)
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	return s
}

func TestSchedulerPollOnce(t *testing.T) {
	t.Run("seeds new events and persists cursor", func(t *testing.T) {
		sensor := &scriptedSensor{
			events:    []Event{event("r#1", "first"), event("r#2", "second")},
			newCursor: json.RawMessage(`{"last_issue_number":2}`),
		}
		cur := &fakeCursorStore{}
		inserter := &fakeInserter{}
		sched := newTestScheduler(t, SchedulerOpts{
			Sensor: sensor, Cursors: cur, Events: newFakeEventStore(), Nebulas: inserter,
		})

		res, err := sched.PollOnce(context.Background())
		if err != nil {
			t.Fatalf("PollOnce: %v", err)
		}
		if res.Observed != 2 || res.Seeded != 2 {
			t.Fatalf("observed=%d seeded=%d, want 2/2", res.Observed, res.Seeded)
		}
		if len(inserter.inserted) != 2 {
			t.Fatalf("inserted %d nebulas, want 2", len(inserter.inserted))
		}
		for _, n := range inserter.inserted {
			if n.Status != SeedStatus {
				t.Errorf("status = %q, want %q", n.Status, SeedStatus)
			}
			if n.SourceName != "github" {
				t.Errorf("source_name = %q, want github", n.SourceName)
			}
		}
		// Cursor advanced to the sensor's newCursor.
		if string(cur.cursor) != `{"last_issue_number":2}` {
			t.Errorf("cursor = %s, want last_issue_number 2", cur.cursor)
		}
		if cur.sets != 1 {
			t.Errorf("cursor set %d times, want 1", cur.sets)
		}
	})

	t.Run("carries derived goals and constraints to the inserter", func(t *testing.T) {
		sensor := &scriptedSensor{events: []Event{event("r#7", "fix")}, newCursor: json.RawMessage(`{}`)}
		inserter := &fakeInserter{}
		sched := newTestScheduler(t, SchedulerOpts{
			Sensor: sensor, Cursors: &fakeCursorStore{}, Events: newFakeEventStore(), Nebulas: inserter,
		})

		if _, err := sched.PollOnce(context.Background()); err != nil {
			t.Fatalf("PollOnce: %v", err)
		}
		got := inserter.inserted[0]
		if len(got.Goals) != 1 || got.Goals[0] != "goal-r#7" {
			t.Errorf("goals = %v, want [goal-r#7]", got.Goals)
		}
		if len(got.Constraints) != 1 || got.Constraints[0] != "constraint-r#7" {
			t.Errorf("constraints = %v, want [constraint-r#7]", got.Constraints)
		}
	})

	t.Run("deduplicates already-processed events across polls", func(t *testing.T) {
		sensor := &scriptedSensor{events: []Event{event("r#1", "first")}, newCursor: json.RawMessage(`{}`)}
		store := newFakeEventStore()
		inserter := &fakeInserter{}
		sched := newTestScheduler(t, SchedulerOpts{
			Sensor: sensor, Cursors: &fakeCursorStore{}, Events: store, Nebulas: inserter,
		})

		if _, err := sched.PollOnce(context.Background()); err != nil {
			t.Fatalf("first PollOnce: %v", err)
		}
		// Second poll returns the same event; it is already processed, so it must
		// not be re-seeded.
		res, err := sched.PollOnce(context.Background())
		if err != nil {
			t.Fatalf("second PollOnce: %v", err)
		}
		if res.Seeded != 0 {
			t.Errorf("second poll seeded=%d, want 0 (dedup)", res.Seeded)
		}
		if len(inserter.inserted) != 1 {
			t.Errorf("total inserted = %d, want 1", len(inserter.inserted))
		}
	})

	t.Run("recovers an orphaned event from a prior crashed tick", func(t *testing.T) {
		store := newFakeEventStore()
		// Simulate a crash: r#1 was observed (row exists) but never seeded.
		store.seedInsertWithoutProcessing("r#1")

		sensor := &scriptedSensor{events: []Event{event("r#1", "orphan")}, newCursor: json.RawMessage(`{}`)}
		inserter := &fakeInserter{}
		sched := newTestScheduler(t, SchedulerOpts{
			Sensor: sensor, Cursors: &fakeCursorStore{}, Events: store, Nebulas: inserter,
		})

		res, err := sched.PollOnce(context.Background())
		if err != nil {
			t.Fatalf("PollOnce: %v", err)
		}
		if res.Seeded != 1 {
			t.Fatalf("seeded=%d, want 1 (orphan recovered)", res.Seeded)
		}
		ids, _ := store.UnprocessedExternalIDs(context.Background(), "", "")
		if len(ids) != 0 {
			t.Errorf("unprocessed after recovery = %v, want none", ids)
		}
	})
}

func TestSchedulerMaxInflight(t *testing.T) {
	var (
		mu    sync.Mutex
		fired []string
	)
	trigger := func(_ context.Context, _, nebulaID, _ string) error {
		mu.Lock()
		fired = append(fired, nebulaID)
		mu.Unlock()
		return nil
	}

	sensor := &scriptedSensor{
		events:    []Event{event("r#1", "a"), event("r#2", "b"), event("r#3", "c")},
		newCursor: json.RawMessage(`{}`),
	}
	instance := &artifacts.SensorInstance{
		Name:         "fake",
		PollInterval: time.Minute,
		MaxInflight:  1,
		Triggers:     []artifacts.SensorTrigger{{Constellation: "architect", When: "new_item"}},
	}
	sched := newTestScheduler(t, SchedulerOpts{
		Instance: instance, Sensor: sensor, Cursors: &fakeCursorStore{},
		Events: newFakeEventStore(), Nebulas: &fakeInserter{}, Trigger: trigger,
	})

	res, err := sched.PollOnce(context.Background())
	if err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	if res.Fired != 1 || res.Queued != 2 {
		t.Fatalf("fired=%d queued=%d, want 1/2 (max_inflight=1)", res.Fired, res.Queued)
	}

	// Drain the queue: each release fires the next queued trigger in FIFO order.
	sched.ReleaseInflight()
	sched.ReleaseInflight()

	mu.Lock()
	defer mu.Unlock()
	want := []string{"nebula-1", "nebula-2", "nebula-3"}
	if len(fired) != 3 {
		t.Fatalf("fired %d triggers, want 3", len(fired))
	}
	for i, id := range want {
		if fired[i] != id {
			t.Errorf("fire order[%d] = %q, want %q (FIFO)", i, fired[i], id)
		}
	}
}

func TestInflightGate(t *testing.T) {
	t.Run("fires up to the cap then queues", func(t *testing.T) {
		g := newInflightGate(2)
		var fired []int
		mk := func(n int) func() { return func() { fired = append(fired, n) } }

		if !g.submit(mk(1)) {
			t.Error("submit 1 should fire (slot free)")
		}
		if !g.submit(mk(2)) {
			t.Error("submit 2 should fire (slot free)")
		}
		if g.submit(mk(3)) {
			t.Error("submit 3 should queue (at cap)")
		}
		if len(fired) != 2 {
			t.Fatalf("fired %v, want 2 immediate", fired)
		}
	})

	t.Run("release drains the queue in FIFO order", func(t *testing.T) {
		g := newInflightGate(1)
		var fired []int
		mk := func(n int) func() { return func() { fired = append(fired, n) } }

		g.submit(mk(1)) // fires
		g.submit(mk(2)) // queued
		g.submit(mk(3)) // queued

		g.release() // drains 2
		g.release() // drains 3
		want := []int{1, 2, 3}
		for i, n := range want {
			if fired[i] != n {
				t.Errorf("fired[%d] = %d, want %d", i, fired[i], n)
			}
		}
	})

	t.Run("release with empty queue decrements the count", func(t *testing.T) {
		g := newInflightGate(1)
		g.submit(func() {}) // count=1
		g.release()         // count back to 0, queue empty
		// A slot is free again.
		if !g.submit(func() {}) {
			t.Error("submit after release should fire (slot freed)")
		}
	})
}
