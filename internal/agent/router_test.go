package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/telemetry"
)

// fakeInvoker is a test Invoker that records each call and returns a canned
// result, so router tests assert on the Agent the Router built without spawning
// a real subprocess.
type fakeInvoker struct {
	mu     sync.Mutex
	calls  int
	agents []Agent
	prompt []string
	result InvocationResult
	err    error
}

func (f *fakeInvoker) Invoke(_ context.Context, a Agent, prompt, _ string) (InvocationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.agents = append(f.agents, a)
	f.prompt = append(f.prompt, prompt)
	if f.err != nil {
		return InvocationResult{}, f.err
	}
	return f.result, nil
}

func (f *fakeInvoker) Validate() error { return nil }

// recordingRecorder captures every RouterMetric for assertions.
type recordingRecorder struct {
	mu      sync.Mutex
	metrics []telemetry.RouterMetric
}

func (r *recordingRecorder) RecordRouter(_ context.Context, m telemetry.RouterMetric) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics = append(r.metrics, m)
	return nil
}

func TestRouterAsk(t *testing.T) {
	ctx := context.Background()

	t.Run("invokes claude with the haiku model", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{result: InvocationResult{ResultText: "internal/sensors/sensor.go:12", InputTokens: 100, OutputTokens: 8}}
		r := NewRouter(inv, nil)

		ans, err := r.Ask(ctx, SubQuestion{Kind: SubKindFileFinder, Query: "Where is the Sensor interface declared?"})
		if err != nil {
			t.Fatalf("Ask: %v", err)
		}
		if inv.calls != 1 {
			t.Fatalf("invoker calls = %d, want 1", inv.calls)
		}
		if got := inv.agents[0].Model; got != RouterModel {
			t.Errorf("model = %q, want %q", got, RouterModel)
		}
		if ans.ModelUsed != RouterModel {
			t.Errorf("ModelUsed = %q, want %q", ans.ModelUsed, RouterModel)
		}
		if ans.Result != "internal/sensors/sensor.go:12" {
			t.Errorf("Result = %q", ans.Result)
		}
		if ans.InputTokens != 100 || ans.OutputTokens != 8 {
			t.Errorf("tokens = %d/%d, want 100/8", ans.InputTokens, ans.OutputTokens)
		}
	})

	t.Run("identical question hits the LRU on the second call", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{result: InvocationResult{ResultText: "x.go:1", InputTokens: 50, OutputTokens: 4}}
		rec := &recordingRecorder{}
		r := NewRouter(inv, rec)
		q := SubQuestion{Kind: SubKindSymbolFinder, Query: "which package owns Loader?"}

		first, err := r.Ask(ctx, q)
		if err != nil {
			t.Fatalf("first Ask: %v", err)
		}
		second, err := r.Ask(ctx, q)
		if err != nil {
			t.Fatalf("second Ask: %v", err)
		}

		if inv.calls != 1 {
			t.Errorf("invoker calls = %d, want 1 (second served from cache)", inv.calls)
		}
		if first.Result != second.Result {
			t.Errorf("cached result mismatch: %q vs %q", first.Result, second.Result)
		}
		if len(rec.metrics) != 2 {
			t.Fatalf("recorded metrics = %d, want 2", len(rec.metrics))
		}
		if rec.metrics[0].CacheHit {
			t.Error("first metric CacheHit = true, want false")
		}
		if !rec.metrics[1].CacheHit {
			t.Error("second metric CacheHit = false, want true")
		}
		// A cache hit still carries the answer's token volume so per-phase
		// savings keep accruing without firing Haiku again.
		if rec.metrics[1].RoutedTokens() != 54 {
			t.Errorf("cached metric RoutedTokens = %d, want 54", rec.metrics[1].RoutedTokens())
		}
	})

	t.Run("distinct questions do not collide", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{result: InvocationResult{ResultText: "y.go:2"}}
		r := NewRouter(inv, nil)

		if _, err := r.Ask(ctx, SubQuestion{Kind: SubKindFileFinder, Query: "a"}); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Ask(ctx, SubQuestion{Kind: SubKindFileFinder, Query: "b"}); err != nil {
			t.Fatal(err)
		}
		if inv.calls != 2 {
			t.Errorf("invoker calls = %d, want 2 (different queries)", inv.calls)
		}
	})

	t.Run("scope is part of the cache key", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{result: InvocationResult{ResultText: "z.go:3"}}
		r := NewRouter(inv, nil)
		base := SubQuestion{Kind: SubKindTestMapper, Query: "tests for X"}

		scoped := base
		scoped.Scope = []string{"internal/sensors"}
		if _, err := r.Ask(ctx, base); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Ask(ctx, scoped); err != nil {
			t.Fatal(err)
		}
		if inv.calls != 2 {
			t.Errorf("invoker calls = %d, want 2 (scope differs)", inv.calls)
		}
	})

	t.Run("invocation error propagates and is not cached", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{err: fmt.Errorf("boom")}
		r := NewRouter(inv, nil)
		q := SubQuestion{Kind: SubKindFileFinder, Query: "fails"}

		if _, err := r.Ask(ctx, q); err == nil {
			t.Fatal("expected error, got nil")
		}
		// A failed answer must not poison the cache: a retry re-invokes.
		inv.err = nil
		inv.result = InvocationResult{ResultText: "ok.go:9"}
		if _, err := r.Ask(ctx, q); err != nil {
			t.Fatalf("retry Ask: %v", err)
		}
		if inv.calls != 2 {
			t.Errorf("invoker calls = %d, want 2 (failure not cached)", inv.calls)
		}
	})
}

func TestRouterMetricTagging(t *testing.T) {
	inv := &fakeInvoker{result: InvocationResult{InputTokens: 30, OutputTokens: 6}}
	rec := &recordingRecorder{}
	r := NewRouter(inv, rec)
	r.NebulaID = "neb-1"
	r.PhaseID = "phase-2"

	if _, err := r.Ask(context.Background(), SubQuestion{Kind: SubKindLintTriage, Query: "worst issue?"}); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if len(rec.metrics) != 1 {
		t.Fatalf("metrics = %d, want 1", len(rec.metrics))
	}
	m := rec.metrics[0]
	if m.NebulaID != "neb-1" || m.PhaseID != "phase-2" {
		t.Errorf("tags = %q/%q, want neb-1/phase-2", m.NebulaID, m.PhaseID)
	}
	if m.SubKind != string(SubKindLintTriage) {
		t.Errorf("SubKind = %q, want %q", m.SubKind, SubKindLintTriage)
	}
	if m.HaikuInTokens != 30 || m.HaikuOutTokens != 6 {
		t.Errorf("tokens = %d/%d, want 30/6", m.HaikuInTokens, m.HaikuOutTokens)
	}
}

func TestResultCacheEviction(t *testing.T) {
	c := newResultCache(2)
	c.put("a", Answer{Result: "A"})
	c.put("b", Answer{Result: "B"})
	// Touch "a" so "b" becomes the least-recently-used entry.
	if _, ok := c.get("a"); !ok {
		t.Fatal("a should be present")
	}
	c.put("c", Answer{Result: "C"}) // evicts "b"

	if _, ok := c.get("b"); ok {
		t.Error("b should have been evicted")
	}
	if _, ok := c.get("a"); !ok {
		t.Error("a should still be present")
	}
	if _, ok := c.get("c"); !ok {
		t.Error("c should be present")
	}
}

func TestResultCacheDisabled(t *testing.T) {
	c := newResultCache(0)
	c.put("a", Answer{Result: "A"})
	if _, ok := c.get("a"); ok {
		t.Error("zero-capacity cache should never hit")
	}
}

// TestRouterElapsedClock confirms the injectable clock drives latency so a hit
// can be timed without wall-clock flakiness.
func TestRouterElapsedClock(t *testing.T) {
	inv := &fakeInvoker{result: InvocationResult{ResultText: "t.go:1"}}
	rec := &recordingRecorder{}
	r := NewRouter(inv, rec)

	now := time.Unix(0, 0)
	r.now = func() time.Time { return now }
	q := SubQuestion{Kind: SubKindFileFinder, Query: "timed"}

	// First call: advance the clock 200ms across the miss.
	r.now = func() time.Time {
		t := now
		now = now.Add(200 * time.Millisecond)
		return t
	}
	if _, err := r.Ask(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if rec.metrics[0].LatencyMs != 200 {
		t.Errorf("miss latency = %dms, want 200", rec.metrics[0].LatencyMs)
	}
}
