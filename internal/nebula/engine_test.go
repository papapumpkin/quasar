package nebula

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/bus"
)

// collectBusEvents subscribes to the bus and collects all events until the
// bus is closed or the context is canceled.
func collectBusEvents(b bus.Bus) <-chan []bus.Event {
	ch := make(chan []bus.Event, 1)
	sub := b.Subscribe("test", 64)
	go func() {
		var events []bus.Event
		for ev := range sub.Events() {
			events = append(events, ev)
		}
		ch <- events
	}()
	return ch
}

func TestEngine_Phase_InitialState(t *testing.T) {
	t.Parallel()
	e := NewEngine(EngineConfig{}, nil, nil, nil)
	if got := e.Phase(); got != EngineIdle {
		t.Errorf("initial phase = %v, want %v", got, EngineIdle)
	}
}

func TestEngine_Transition_ThreadSafe(t *testing.T) {
	t.Parallel()
	e := NewEngine(EngineConfig{}, nil, nil, nil)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			e.transition(EngineLoading)
			e.transition(EngineIdle)
		}
		close(done)
	}()

	for i := 0; i < 100; i++ {
		_ = e.Phase() // concurrent read
	}
	<-done
}

func TestEngine_Run_LoadError_ShortCircuits(t *testing.T) {
	t.Parallel()

	memBus := bus.NewMemoryBus()
	eventsCh := collectBusEvents(memBus)

	e := NewEngine(EngineConfig{
		NebulaDir: "/nonexistent/path/to/nebula",
	}, memBus, nil, nil)

	result := e.Run(context.Background())

	if result.Err == nil {
		t.Fatal("expected error from invalid nebula dir, got nil")
	}
	if !strings.Contains(result.Err.Error(), "load nebula") {
		t.Errorf("error should mention 'load nebula', got: %v", result.Err)
	}
	if result.Plan != nil {
		t.Error("plan should be nil on load error")
	}
	if result.WorkerResults != nil {
		t.Error("worker results should be nil on load error")
	}

	// Verify final phase is Done.
	if got := e.Phase(); got != EngineDone {
		t.Errorf("phase after error = %v, want %v", got, EngineDone)
	}

	// Verify bus events.
	memBus.Close()
	events := <-eventsCh

	if len(events) < 2 {
		t.Fatalf("expected at least 2 bus events, got %d", len(events))
	}
	if events[0].Kind != bus.KindEngineLoading {
		t.Errorf("first event = %v, want %v", events[0].Kind, bus.KindEngineLoading)
	}
	if events[len(events)-1].Kind != bus.KindEngineDone {
		t.Errorf("last event = %v, want %v", events[len(events)-1].Kind, bus.KindEngineDone)
	}
}

func TestEngine_Run_ValidationError_ShortCircuits(t *testing.T) {
	t.Parallel()

	// Use testdata/invalid-missing which has a phase missing required fields.
	e := NewEngine(EngineConfig{
		NebulaDir: "testdata/invalid-missing",
	}, nil, nil, nil)

	result := e.Run(context.Background())

	if result.Err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(result.Err.Error(), "validation failed") {
		t.Errorf("error should mention 'validation failed', got: %v", result.Err)
	}
	// Verify the error is a ValidationFailedError with structured fields.
	var valErr *ValidationFailedError
	if !errors.As(result.Err, &valErr) {
		t.Errorf("expected *ValidationFailedError, got %T", result.Err)
	}
	if got := e.Phase(); got != EngineDone {
		t.Errorf("phase = %v, want %v", got, EngineDone)
	}
}

func TestEngine_Run_BranchError_ShortCircuits(t *testing.T) {
	t.Parallel()

	// Valid nebula but WorkDir is not a git repo → branch creation fails.
	e := NewEngine(EngineConfig{
		NebulaDir: "testdata/valid",
		WorkDir:   t.TempDir(), // temp dir is not a git repo
	}, nil, nil, nil)

	result := e.Run(context.Background())

	if result.Err == nil {
		t.Fatal("expected branch error, got nil")
	}
	if !strings.Contains(result.Err.Error(), "branch") {
		t.Errorf("error should mention 'branch', got: %v", result.Err)
	}
	if got := e.Phase(); got != EngineDone {
		t.Errorf("phase = %v, want %v", got, EngineDone)
	}
}

func TestEngine_Run_NilBus_NoPanic(t *testing.T) {
	t.Parallel()

	// A nil bus should not cause a panic at any lifecycle point.
	e := NewEngine(EngineConfig{
		NebulaDir: "/nonexistent",
	}, nil, nil, nil)

	// This should not panic even with nil bus.
	result := e.Run(context.Background())
	if result.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := e.Phase(); got != EngineDone {
		t.Errorf("phase = %v, want %v", got, EngineDone)
	}
}

func TestEngine_Run_ContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	e := NewEngine(EngineConfig{
		NebulaDir: "/nonexistent",
	}, nil, nil, nil)

	result := e.Run(ctx)
	// Should still complete (load will fail with file error, not context error).
	if result.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := e.Phase(); got != EngineDone {
		t.Errorf("phase = %v, want %v", got, EngineDone)
	}
}

func TestEngine_Run_LifecycleEvents_Ordered(t *testing.T) {
	t.Parallel()

	memBus := bus.NewMemoryBus()
	eventsCh := collectBusEvents(memBus)

	// This will fail at load, but we can verify the events that were published.
	e := NewEngine(EngineConfig{
		NebulaDir: "/nonexistent",
	}, memBus, nil, nil)

	_ = e.Run(context.Background())

	memBus.Close()
	events := <-eventsCh

	// We expect: loading, done (load fails immediately).
	wantKinds := []bus.Kind{bus.KindEngineLoading, bus.KindEngineDone}
	if len(events) != len(wantKinds) {
		t.Fatalf("got %d events, want %d", len(events), len(wantKinds))
	}
	for i, want := range wantKinds {
		if events[i].Kind != want {
			t.Errorf("event[%d].Kind = %v, want %v", i, events[i].Kind, want)
		}
	}

	// Verify timestamps are non-zero and ordered.
	for i, ev := range events {
		if ev.Timestamp.IsZero() {
			t.Errorf("event[%d] has zero timestamp", i)
		}
		if i > 0 && ev.Timestamp.Before(events[i-1].Timestamp) {
			t.Errorf("event[%d] timestamp before event[%d]", i, i-1)
		}
	}
}

func TestEngine_PublishLifecycle_NilBus(t *testing.T) {
	t.Parallel()

	e := NewEngine(EngineConfig{}, nil, nil, nil)

	// Should not panic.
	e.publishLifecycle(context.Background(), bus.KindEngineLoading)
	e.publishEvent(context.Background(), bus.New(bus.KindEngineDone))
}

func TestEngine_BuildWorkerOptions(t *testing.T) {
	t.Parallel()

	memBus := bus.NewMemoryBus()
	defer memBus.Close()

	e := NewEngine(EngineConfig{
		MaxWorkers:      4,
		MaxReviewCycles: 10,
		MaxBudgetUSD:    50.0,
		Model:           "claude-sonnet",
		Resume:          true,
		NebulaDir:       "/tmp/test-nebula",
	}, memBus, nil, nil)

	opts := e.buildWorkerOptions()

	// We should have the base options plus resume options.
	// Base: MaxWorkers, BeadsClient, GlobalCycles, GlobalBudget, GlobalModel, Bus, Invoker, Committer, CheckpointDir = 9
	// Resume: ResumeEnabled = 1
	// Total = 10
	if len(opts) != 10 {
		t.Errorf("got %d options, want 10", len(opts))
	}
}

func TestEngine_BuildWorkerOptions_NoResume(t *testing.T) {
	t.Parallel()

	e := NewEngine(EngineConfig{
		MaxWorkers:      2,
		MaxReviewCycles: 5,
		MaxBudgetUSD:    10.0,
	}, nil, nil, nil)

	opts := e.buildWorkerOptions()

	// Base options only: MaxWorkers, BeadsClient, GlobalCycles, GlobalBudget, GlobalModel, Bus, Invoker, Committer, CheckpointDir = 9
	if len(opts) != 9 {
		t.Errorf("got %d options, want 9", len(opts))
	}
}

func TestEngine_BuildWorkerOptions_WithFabric(t *testing.T) {
	t.Parallel()

	e := NewEngine(EngineConfig{
		MaxWorkers:      2,
		MaxReviewCycles: 5,
		MaxBudgetUSD:    10.0,
	}, nil, nil, nil)

	e.SetFabric(&mockFabricCloser{
		options: []Option{
			WithGlobalModel("test"), // dummy option to count
		},
	})

	opts := e.buildWorkerOptions()

	// 9 base + 1 from fabric
	if len(opts) != 10 {
		t.Errorf("got %d options, want 10", len(opts))
	}
}

func TestEngine_SetFabric(t *testing.T) {
	t.Parallel()

	e := NewEngine(EngineConfig{}, nil, nil, nil)
	if e.fabric != nil {
		t.Error("fabric should be nil initially")
	}

	fc := &mockFabricCloser{}
	e.SetFabric(fc)
	if e.fabric == nil {
		t.Error("fabric should be set after SetFabric")
	}
}

func TestEngine_Phase_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		phase EnginePhase
		want  string
	}{
		{EngineIdle, "idle"},
		{EngineLoading, "loading"},
		{EnginePlanning, "planning"},
		{EngineExecuting, "executing"},
		{EngineCompleting, "completing"},
		{EngineDone, "done"},
		{EnginePhase(99), "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.phase.String(); got != tc.want {
				t.Errorf("EnginePhase(%d).String() = %q, want %q", tc.phase, got, tc.want)
			}
		})
	}
}

func TestToErrors(t *testing.T) {
	t.Parallel()

	valErrs := []ValidationError{
		{PhaseID: "a", Err: ErrMissingField, SourceFile: "01.md"},
		{PhaseID: "b", Err: ErrDuplicateID, SourceFile: "02.md"},
	}

	errs := toErrors(valErrs)
	if len(errs) != 2 {
		t.Fatalf("got %d errors, want 2", len(errs))
	}
	for i, err := range errs {
		if err == nil {
			t.Errorf("errs[%d] is nil", i)
		}
		if !strings.Contains(err.Error(), valErrs[i].PhaseID) {
			t.Errorf("errs[%d] should contain phase ID %q, got %q", i, valErrs[i].PhaseID, err.Error())
		}
	}
}

func TestEngine_PlanOnlyMode(t *testing.T) {
	t.Parallel()

	memBus := bus.NewMemoryBus()
	eventsCh := collectBusEvents(memBus)

	// Plan-only mode: Auto=false. Load will fail but we verify the event order.
	e := NewEngine(EngineConfig{
		NebulaDir: "/nonexistent",
		Auto:      false,
	}, memBus, nil, nil)

	result := e.Run(context.Background())
	if result.Err == nil {
		t.Fatal("expected error, got nil")
	}

	memBus.Close()
	events := <-eventsCh

	// Should have loading + done (load error short circuits).
	// Even in plan-only mode, we never reach executing/completing.
	hasExecuting := false
	for _, ev := range events {
		if ev.Kind == bus.KindEngineExecuting {
			hasExecuting = true
		}
	}
	if hasExecuting {
		t.Error("plan-only mode should not publish executing event")
	}
}

// --- Test helpers ---

// mockFabricCloser implements fabricCloser for testing.
type mockFabricCloser struct {
	closed  bool
	options []Option
}

func (m *mockFabricCloser) Close() error {
	m.closed = true
	return nil
}

func (m *mockFabricCloser) WorkerGroupOptions() []Option {
	return m.options
}
