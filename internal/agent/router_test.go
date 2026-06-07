package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/artifacts"
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

// fakeLoader returns a canned sub-star for any name, standing in for the real
// artifacts loader so router tests can assert the Agent inherits the star's
// rubric, tool allow-list, effort, and budget.
type fakeLoader struct {
	mu    sync.Mutex
	names []string
	star  *artifacts.Star
	err   error
}

func (l *fakeLoader) LoadStar(name string) (*artifacts.Star, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.names = append(l.names, name)
	if l.err != nil {
		return nil, l.err
	}
	return l.star, nil
}

// readOnlyStar is the canned sub-star used across tests: read-only tools, a
// low-effort/low-budget profile, and a Haiku model the Router is expected to
// honor (it forces RouterModel regardless, which equals this).
func readOnlyStar() *artifacts.Star {
	return &artifacts.Star{
		Name:          "file-finder",
		Model:         "some-other-model", // Router must override this with RouterModel.
		FallbackModel: "fallback-x",
		Prompt:        "You locate where a Go symbol is declared.",
		Tools:         artifacts.StarTools{Allowed: []string{"Read", "Glob", "Grep"}, Denied: []string{"Edit", "Write"}},
		Defaults:      artifacts.StarDefaults{MaxBudgetUSD: 0.05, Effort: "low"},
	}
}

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

	t.Run("loads the sub-star and forces the haiku model", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{result: InvocationResult{ResultText: "internal/sensors/sensor.go:12", InputTokens: 100, OutputTokens: 8}}
		ld := &fakeLoader{star: readOnlyStar()}
		r := NewRouter(inv, ld, nil)

		ans, err := r.Ask(ctx, SubQuestion{Kind: SubKindFileFinder, Query: "Where is the Sensor interface declared?"})
		if err != nil {
			t.Fatalf("Ask: %v", err)
		}
		if inv.calls != 1 {
			t.Fatalf("invoker calls = %d, want 1", inv.calls)
		}
		// The right sub-star was loaded for this kind.
		if len(ld.names) != 1 || ld.names[0] != "file-finder" {
			t.Errorf("loaded stars = %v, want [file-finder]", ld.names)
		}
		got := inv.agents[0]
		// Router owns the model: forces the cheap tier regardless of star authoring.
		if got.Model != RouterModel {
			t.Errorf("model = %q, want %q", got.Model, RouterModel)
		}
		// Star owns the rubric, tools, effort, and budget — all must flow through.
		if got.SystemPrompt != "You locate where a Go symbol is declared." {
			t.Errorf("system prompt = %q", got.SystemPrompt)
		}
		if len(got.AllowedTools) != 3 || got.AllowedTools[0] != "Read" {
			t.Errorf("allowed tools = %v, want read-only set", got.AllowedTools)
		}
		if got.MaxBudgetUSD != 0.05 {
			t.Errorf("budget = %v, want 0.05", got.MaxBudgetUSD)
		}
		if got.Effort != "low" {
			t.Errorf("effort = %q, want low", got.Effort)
		}
		if got.Role != RoleRouter {
			t.Errorf("role = %q, want %q", got.Role, RoleRouter)
		}
		// The pinned tight budget, not coder/reviewer defaults.
		if got.ContextBudget == nil || got.ContextBudget.MaxTotalReads != routerMaxTotalReads {
			t.Errorf("context budget = %+v, want tight router budget", got.ContextBudget)
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

	t.Run("each kind loads its own sub-star", func(t *testing.T) {
		t.Parallel()
		cases := map[SubKind]string{
			SubKindFileFinder:   "file-finder",
			SubKindTestMapper:   "test-mapper",
			SubKindLintTriage:   "lint-triage",
			SubKindSymbolFinder: "symbol-finder",
		}
		for kind, want := range cases {
			inv := &fakeInvoker{result: InvocationResult{ResultText: "x"}}
			ld := &fakeLoader{star: readOnlyStar()}
			r := NewRouter(inv, ld, nil)
			if _, err := r.Ask(ctx, SubQuestion{Kind: kind, Query: "q"}); err != nil {
				t.Fatalf("Ask(%s): %v", kind, err)
			}
			if len(ld.names) != 1 || ld.names[0] != want {
				t.Errorf("kind %s loaded %v, want [%s]", kind, ld.names, want)
			}
		}
	})

	t.Run("unknown kind fails closed without invoking", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{result: InvocationResult{ResultText: "x"}}
		ld := &fakeLoader{star: readOnlyStar()}
		r := NewRouter(inv, ld, nil)
		if _, err := r.Ask(ctx, SubQuestion{Kind: SubKind("bogus"), Query: "q"}); err == nil {
			t.Fatal("expected error for unknown kind")
		}
		if inv.calls != 0 {
			t.Errorf("invoker calls = %d, want 0 (failed before invoke)", inv.calls)
		}
	})

	t.Run("sub-star load failure fails closed", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{result: InvocationResult{ResultText: "x"}}
		ld := &fakeLoader{err: fmt.Errorf("missing star")}
		r := NewRouter(inv, ld, nil)
		if _, err := r.Ask(ctx, SubQuestion{Kind: SubKindFileFinder, Query: "q"}); err == nil {
			t.Fatal("expected error when sub-star cannot load")
		}
		if inv.calls != 0 {
			t.Errorf("invoker calls = %d, want 0 (no unrestricted fallback)", inv.calls)
		}
	})

	t.Run("identical question hits the LRU on the second call", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{result: InvocationResult{ResultText: "x.go:1", InputTokens: 50, OutputTokens: 4}}
		ld := &fakeLoader{star: readOnlyStar()}
		rec := &recordingRecorder{}
		r := NewRouter(inv, ld, rec)
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
		// Every metric carries a non-empty invocation id.
		for i, m := range rec.metrics {
			if m.InvocationID == "" {
				t.Errorf("metric[%d] InvocationID empty", i)
			}
		}
	})

	t.Run("distinct questions do not collide", func(t *testing.T) {
		t.Parallel()
		inv := &fakeInvoker{result: InvocationResult{ResultText: "y.go:2"}}
		ld := &fakeLoader{star: readOnlyStar()}
		r := NewRouter(inv, ld, nil)

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
		ld := &fakeLoader{star: readOnlyStar()}
		r := NewRouter(inv, ld, nil)
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
		ld := &fakeLoader{star: readOnlyStar()}
		r := NewRouter(inv, ld, nil)
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
	ld := &fakeLoader{star: readOnlyStar()}
	rec := &recordingRecorder{}
	r := NewRouter(inv, ld, rec)
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
	if m.InvocationID == "" {
		t.Error("InvocationID should be populated")
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
	ld := &fakeLoader{star: readOnlyStar()}
	rec := &recordingRecorder{}
	r := NewRouter(inv, ld, rec)

	now := time.Unix(0, 0)
	// Each now() call advances the clock 200ms; Ask calls it twice (start +
	// elapsed), so the recorded latency is exactly one 200ms step.
	r.now = func() time.Time {
		t := now
		now = now.Add(200 * time.Millisecond)
		return t
	}
	if _, err := r.Ask(context.Background(), SubQuestion{Kind: SubKindFileFinder, Query: "timed"}); err != nil {
		t.Fatal(err)
	}
	if rec.metrics[0].LatencyMs != 200 {
		t.Errorf("miss latency = %dms, want 200", rec.metrics[0].LatencyMs)
	}
}
