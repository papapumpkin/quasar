package sensors

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/papapumpkin/quasar/internal/artifacts"
)

// SeedStatus is the lifecycle status a sensor-seeded nebula is written with.
// Sensor-generated drafts need human approval before execution, so they land in
// awaiting_approval; the architect constellation runs once the user approves.
const SeedStatus = "awaiting_approval"

// defaultMaxInflight caps concurrent in-flight constellation triggers per
// (repo, sensor) when the instance does not configure max_inflight.
const defaultMaxInflight = 4

// defaultPollTimeout bounds a single sensor.Poll so a hung external call cannot
// stall the scheduler's tick loop indefinitely.
const defaultPollTimeout = 60 * time.Second

// defaultPollInterval is the fallback poll cadence for sensor instances whose
// TOML file omits poll_interval. Five minutes is conservative enough to stay
// within typical GitHub API rate limits across a large fleet.
const defaultPollInterval = 5 * time.Minute

// defaultTriggerWhen is the trigger condition matched against newly-seeded
// events when a trigger omits its `when`.
const defaultTriggerWhen = "new_item"

// TriggerFunc launches the constellation named for a seeded nebula. In this
// phase it is injected so the scheduler can be exercised without the
// constellation runtime; Phase 5 supplies the production implementation that
// enqueues a trigger_queue row for the runtime to consume.
type TriggerFunc func(ctx context.Context, repoPath, nebulaID, constellationName string) error

// CursorStore persists a sensor's opaque cursor across polls.
type CursorStore interface {
	Get(ctx context.Context, repoPath, sensorName string) (json.RawMessage, error)
	Set(ctx context.Context, repoPath, sensorName string, cursor json.RawMessage) error
}

// EventStore records observed events with (repo, sensor, externalID) dedup and
// tracks which have been seeded into a nebula.
type EventStore interface {
	Insert(ctx context.Context, repoPath, sensorName, externalID string, ts time.Time) (id int64, isNew bool, err error)
	MarkProcessed(ctx context.Context, id int64, nebulaID string) error
	// UnprocessedExternalIDs returns the external ids of events that were observed
	// (a row exists) but never seeded into a nebula — orphans left by a tick that
	// crashed between recording an event and marking it processed. The scheduler
	// re-seeds them on the next poll (see PollOnce).
	UnprocessedExternalIDs(ctx context.Context, repoPath, sensorName string) ([]string, error)
}

// SeedNebula is the payload the scheduler hands a NebulaInserter for a newly
// observed event. It is package-local — mirroring only the persistable fields a
// sensor produces — so the sensors package owns its consumer interface's
// parameter type and stays off the fabric dependency (layering). The
// fabric-backed implementation maps it onto its own row type.
type SeedNebula struct {
	RepoPath    string
	Name        string
	Description string
	SourceName  string
	SourceID    string
	SourceURL   string
	Status      string
	// Goals and Constraints are derived by the sensor from the source item. The
	// inserter renders them into the nebula's context block so the architect that
	// later refines the seed inherits them as repo context.
	Goals       []string
	Constraints []string
	// Labels and Assignee carry the source item's triage metadata through to the
	// seed row.
	Labels   []string
	Assignee string
}

// NebulaInserter writes a seed nebula row and returns its generated id.
type NebulaInserter interface {
	Insert(ctx context.Context, n SeedNebula) (string, error)
}

// SchedulerOpts configures a Scheduler. RepoPath, Instance, Sensor, Cursors,
// Events, and Nebulas are required; the rest take documented defaults.
type SchedulerOpts struct {
	RepoPath    string
	Instance    *artifacts.SensorInstance
	Sensor      Sensor
	Cursors     CursorStore
	Events      EventStore
	Nebulas     NebulaInserter
	Trigger     TriggerFunc   // nil disables trigger firing (events are still seeded)
	Logger      io.Writer     // nil discards
	PollTimeout time.Duration // <=0 uses defaultPollTimeout
	Now         func() time.Time
	// OnSeed, when non-nil, is called after each new nebula row is durably
	// written. The supervisor uses it to notify connected UIs without coupling
	// the sensors package to any UI or event-bus type.
	OnSeed func(nebulaID string)
}

// Scheduler drives one sensor instance: on each tick it polls for new events,
// persists the cursor, deduplicates and seeds new events into nebulas, and fires
// the instance's triggers under a per-(repo, sensor) in-flight cap. There is one
// Scheduler per (repo_path, sensor_name) tuple.
type Scheduler struct {
	repoPath    string
	sensorName  string
	instance    *artifacts.SensorInstance
	sensor      Sensor
	cursors     CursorStore
	events      EventStore
	nebulas     NebulaInserter
	trigger     TriggerFunc
	logger      io.Writer
	pollTimeout time.Duration
	now         func() time.Time
	onSeed      func(string)

	gate *inflightGate
}

// PollResult summarizes a single poll cycle for logging and the admin CLI.
type PollResult struct {
	Observed  int      // events the sensor returned
	Seeded    int      // new nebulas written this cycle
	NebulaIDs []string // ids of the nebulas seeded this cycle
	Fired     int      // triggers fired immediately
	Queued    int      // triggers queued behind the in-flight cap
}

// NewScheduler validates opts and constructs a ready Scheduler.
func NewScheduler(opts SchedulerOpts) (*Scheduler, error) {
	switch {
	case opts.RepoPath == "":
		return nil, fmt.Errorf("sensors: scheduler requires RepoPath")
	case opts.Instance == nil:
		return nil, fmt.Errorf("sensors: scheduler requires Instance")
	case opts.Sensor == nil:
		return nil, fmt.Errorf("sensors: scheduler requires Sensor")
	case opts.Cursors == nil:
		return nil, fmt.Errorf("sensors: scheduler requires Cursors")
	case opts.Events == nil:
		return nil, fmt.Errorf("sensors: scheduler requires Events")
	case opts.Nebulas == nil:
		return nil, fmt.Errorf("sensors: scheduler requires Nebulas")
	}

	maxInflight := opts.Instance.MaxInflight
	if maxInflight <= 0 {
		maxInflight = defaultMaxInflight
	}
	logger := opts.Logger
	if logger == nil {
		logger = io.Discard
	}
	pollTimeout := opts.PollTimeout
	if pollTimeout <= 0 {
		pollTimeout = defaultPollTimeout
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	onSeed := opts.OnSeed
	if onSeed == nil {
		onSeed = func(string) {}
	}

	return &Scheduler{
		repoPath:    opts.RepoPath,
		sensorName:  opts.Instance.Name,
		instance:    opts.Instance,
		sensor:      opts.Sensor,
		cursors:     opts.Cursors,
		events:      opts.Events,
		nebulas:     opts.Nebulas,
		trigger:     opts.Trigger,
		logger:      logger,
		pollTimeout: pollTimeout,
		now:         now,
		onSeed:      onSeed,
		gate:        newInflightGate(maxInflight),
	}, nil
}

// Run drives the scheduler loop until ctx is canceled. It polls immediately,
// then on every PollInterval tick. A failed poll is logged and the loop
// continues — a transient gh outage must not kill the scheduler.
func (s *Scheduler) Run(ctx context.Context) error {
	interval := s.instance.PollInterval
	if interval <= 0 {
		interval = defaultPollInterval
		fmt.Fprintf(s.logger, "sensor %s/%s: no poll_interval configured, using default %s\n",
			s.repoPath, s.sensorName, defaultPollInterval)
	}

	s.tick(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick runs one poll cycle and logs the outcome, swallowing errors so the loop
// survives transient failures.
func (s *Scheduler) tick(ctx context.Context) {
	res, err := s.PollOnce(ctx)
	if err != nil {
		fmt.Fprintf(s.logger, "sensor %s/%s: poll failed: %v\n", s.repoPath, s.sensorName, err)
		return
	}
	fmt.Fprintf(s.logger, "sensor %s/%s: observed=%d seeded=%d fired=%d queued=%d\n",
		s.repoPath, s.sensorName, res.Observed, res.Seeded, res.Fired, res.Queued)
}

// PollOnce executes a single poll cycle: recover orphans from a prior crashed
// tick, poll, dedup-and-seed events, persist the cursor, and fire triggers under
// the in-flight cap. It is exported so the admin `quasar sensor poll` command can
// force one cycle.
//
// Crash safety rests on two invariants: an event is recorded (Insert) before it
// is seeded, and the cursor is advanced (cursors.Set) only after the whole batch
// is seeded. So a crash anywhere mid-batch leaves the cursor unadvanced, the next
// Poll re-fetches the same issues (with their Raw payload, which the event row
// does not store), and the orphan set tells the scheduler which of those to
// re-seed versus skip as already-done.
func (s *Scheduler) PollOnce(ctx context.Context) (PollResult, error) {
	cursor, err := s.cursors.Get(ctx, s.repoPath, s.sensorName)
	if err != nil {
		return PollResult{}, fmt.Errorf("sensors: load cursor: %w", err)
	}

	orphans, err := s.events.UnprocessedExternalIDs(ctx, s.repoPath, s.sensorName)
	if err != nil {
		return PollResult{}, fmt.Errorf("sensors: load unprocessed events: %w", err)
	}
	orphaned := make(map[string]bool, len(orphans))
	for _, id := range orphans {
		orphaned[id] = true
	}

	pollCtx, cancel := context.WithTimeout(ctx, s.pollTimeout)
	defer cancel()
	events, newCursor, err := s.sensor.Poll(pollCtx, cursor)
	if err != nil {
		return PollResult{}, fmt.Errorf("sensors: poll %q: %w", s.sensorName, err)
	}

	res := PollResult{Observed: len(events)}

	// Record every observed event (the row is the dedup key), seeding the ones not
	// yet processed: brand-new events (isNew) and orphans re-fetched after a crash.
	// A duplicate from a completed earlier tick is neither, so it is skipped.
	type seeded struct{ nebulaID string }
	var done []seeded
	for _, ev := range events {
		ts := ev.Timestamp
		if ts.IsZero() {
			ts = s.now()
		}
		id, isNew, insErr := s.events.Insert(ctx, s.repoPath, s.sensorName, ev.ExternalID, ts)
		if insErr != nil {
			return res, fmt.Errorf("sensors: record event %q: %w", ev.ExternalID, insErr)
		}
		if !isNew && !orphaned[ev.ExternalID] {
			continue
		}

		nebulaID, seedErr := s.seed(ctx, ev)
		if seedErr != nil {
			return res, seedErr
		}
		if err := s.events.MarkProcessed(ctx, id, nebulaID); err != nil {
			return res, fmt.Errorf("sensors: mark event %q processed: %w", ev.ExternalID, err)
		}
		res.Seeded++
		res.NebulaIDs = append(res.NebulaIDs, nebulaID)
		done = append(done, seeded{nebulaID: nebulaID})
	}

	// Advance the cursor only after the batch is durably seeded, so a crash before
	// this point is recovered by re-polling rather than orphaning events.
	if err := s.cursors.Set(ctx, s.repoPath, s.sensorName, newCursor); err != nil {
		return res, fmt.Errorf("sensors: persist cursor: %w", err)
	}

	for _, d := range done {
		fired, queued := s.dispatch(ctx, d.nebulaID)
		res.Fired += fired
		res.Queued += queued
	}

	return res, nil
}

// seed renders an event into seed nebula content and inserts an
// awaiting_approval nebula row, returning its id.
func (s *Scheduler) seed(ctx context.Context, ev Event) (string, error) {
	content, err := s.sensor.SeedNebula(ev)
	if err != nil {
		return "", fmt.Errorf("sensors: seed nebula for %q: %w", ev.ExternalID, err)
	}
	id, err := s.nebulas.Insert(ctx, SeedNebula{
		RepoPath:    s.repoPath,
		Name:        content.Name,
		Description: content.Description,
		SourceName:  content.SourceName,
		SourceID:    content.SourceID,
		SourceURL:   content.SourceURL,
		Status:      SeedStatus,
		Goals:       content.Goals,
		Constraints: content.Constraints,
		Labels:      content.Labels,
		Assignee:    content.Assignee,
	})
	if err != nil {
		return "", fmt.Errorf("sensors: insert seed nebula for %q: %w", ev.ExternalID, err)
	}
	s.onSeed(id)
	return id, nil
}

// dispatch fires every matching trigger for a seeded nebula through the
// in-flight gate, returning how many fired immediately versus were queued.
//
// A queued trigger captures ctx and runs only when ReleaseInflight later frees a
// slot — so callers MUST keep ctx alive until every queued trigger has drained.
// In the long-running Run loop ctx is the scheduler's lifetime context, so this
// holds. The one-shot CLI path injects a nil Trigger, so nothing is queued there.
// When Phase 5 wires the real trigger, prefer giving the gate a
// scheduler-lifetime context to fire queued work under, rather than the
// per-poll context captured here.
func (s *Scheduler) dispatch(ctx context.Context, nebulaID string) (fired, queued int) {
	for _, t := range s.instance.Triggers {
		if !triggerMatches(t.When) {
			continue
		}
		constellation := t.Constellation
		f := func() {
			if s.trigger == nil {
				return
			}
			if err := s.trigger(ctx, s.repoPath, nebulaID, constellation); err != nil {
				fmt.Fprintf(s.logger, "sensor %s/%s: trigger %q for %s failed: %v\n",
					s.repoPath, s.sensorName, constellation, nebulaID, err)
			}
		}
		if s.gate.submit(f) {
			fired++
		} else {
			queued++
		}
	}
	return fired, queued
}

// ReleaseInflight frees one in-flight slot and fires the oldest queued trigger,
// if any. Phase 5's runtime calls it when a constellation completes; tests call
// it to observe FIFO draining. A drained trigger runs under the context captured
// when it was queued (see dispatch), so callers must only release while that
// originating context is still alive.
func (s *Scheduler) ReleaseInflight() {
	s.gate.release()
}

// triggerMatches reports whether a trigger's `when` fires on a freshly-seeded
// event. An empty `when` defaults to new_item.
func triggerMatches(when string) bool {
	return when == "" || when == defaultTriggerWhen
}
