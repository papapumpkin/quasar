package agent

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/papapumpkin/quasar/internal/telemetry"
)

// RouterModel is the cheap model every routed sub-question runs against. It is
// the same claude binary as the coder uses, invoked with a different --model
// flag, so a bounded factual lookup costs Haiku rates rather than Opus/Sonnet.
const RouterModel = "claude-haiku-4-5-20251001"

// defaultRouterLatency bounds a single routed call. A routed question is a
// bounded lookup; if Haiku has not answered within this window the call is
// cancelled so a stuck sub-question never stalls the coder.
const defaultRouterLatency = 15 * time.Second

// routerCacheCapacity is the number of distinct sub-questions the in-process LRU
// retains per Router. Routed questions repeat heavily within a phase (the same
// "where is X declared?" recurs across cycles), so a small cache captures most
// of the reuse while bounding memory.
const routerCacheCapacity = 128

// SubKind classifies a routed sub-question so the Router can hand Haiku the
// right rubric (system prompt) and so telemetry can attribute savings by kind.
type SubKind string

// The bounded question kinds the router answers. Each maps to a tiny sub-star
// prompt tuned for Haiku (internal/artifacts/defaults/stars/*.md).
const (
	// SubKindFileFinder locates where a symbol is declared or implemented.
	SubKindFileFinder SubKind = "file_finder"
	// SubKindTestMapper lists the tests covering a file or function.
	SubKindTestMapper SubKind = "test_mapper"
	// SubKindLintTriage picks the highest-priority issue out of lint output.
	SubKindLintTriage SubKind = "lint_triage"
	// SubKindSymbolFinder names the package that owns a symbol.
	SubKindSymbolFinder SubKind = "symbol_finder"
)

// SubQuestion is a bounded factual question the coder delegates to the cheap
// tier instead of paying for premium-model inference on its own.
type SubQuestion struct {
	Kind       SubKind       // selects the rubric and tags telemetry
	Query      string        // free-text question, e.g. "Where is the Sensor interface declared?"
	Workdir    string        // repo root the sub-agent runs in
	Scope      []string      // optional file/dir scope to narrow the search
	MaxLatency time.Duration // per-call deadline; defaultRouterLatency when zero
}

// Answer is the structured result of a routed sub-question. Result is formatted
// per Kind (a path:line list, a JSON object, etc.). The token counts feed
// telemetry that confirms Haiku stayed cheap.
type Answer struct {
	Result       string // structured answer per Kind
	InputTokens  int    // Haiku input tokens billed for this answer
	OutputTokens int    // Haiku output tokens billed for this answer
	ModelUsed    string // the model that produced the answer (always RouterModel)
}

// RouterMetricRecorder records router telemetry. *telemetry.RouterMetricStore
// satisfies it; a nil recorder disables recording without the Router branching.
// The interface is defined here, where it is consumed, per project convention.
type RouterMetricRecorder interface {
	RecordRouter(ctx context.Context, m telemetry.RouterMetric) error
}

// Router executes a sub-prompt against a cheaper model and returns the
// structured result. Coder/reviewer stars use it to delegate bounded questions
// (file lookup, test mapping, symbol resolution) without paying for
// premium-model inference. Results repeat within a phase, so identical
// questions are served from an in-process LRU on every call after the first.
type Router struct {
	invoker  Invoker
	model    string
	cache    *resultCache
	recorder RouterMetricRecorder
	now      func() time.Time // injectable clock for latency measurement (tests)

	// NebulaID and PhaseID tag recorded RouterMetrics so `quasar cache report
	// --router` can attribute savings to a phase. The wiring layer sets them
	// before handing the Router to a star; both may be empty (untagged).
	NebulaID string
	PhaseID  string
}

// NewRouter returns a Router that delegates to inv (the same Claude invoker the
// coder uses, run with the Haiku --model flag) and records every call to rec. A
// nil rec disables recording.
func NewRouter(inv Invoker, rec RouterMetricRecorder) *Router {
	return &Router{
		invoker:  inv,
		model:    RouterModel,
		cache:    newResultCache(routerCacheCapacity),
		recorder: rec,
		now:      time.Now,
	}
}

// Ask answers a bounded sub-question. Identical questions (same kind, query,
// workdir, and scope) are served from the in-process LRU after the first call,
// so the second Ask never fires Haiku. Every call — hit or miss — is recorded
// as a RouterMetric.
func (r *Router) Ask(ctx context.Context, q SubQuestion) (Answer, error) {
	key := q.cacheKey()
	start := r.now()

	if ans, ok := r.cache.get(key); ok {
		r.record(ctx, q, ans, r.elapsedMs(start), true)
		return ans, nil
	}

	ans, err := r.invoke(ctx, q)
	if err != nil {
		return Answer{}, err
	}

	r.cache.put(key, ans)
	r.record(ctx, q, ans, r.elapsedMs(start), false)
	return ans, nil
}

// invoke runs Haiku for a cache miss, bounded by the question's MaxLatency.
func (r *Router) invoke(ctx context.Context, q SubQuestion) (Answer, error) {
	latency := q.MaxLatency
	if latency <= 0 {
		latency = defaultRouterLatency
	}
	callCtx, cancel := context.WithTimeout(ctx, latency)
	defer cancel()

	a := Agent{
		Role:              RoleCoder,
		SystemPrompt:      systemPromptFor(q.Kind),
		Model:             r.model,
		CacheOptimization: true,
	}

	res, err := r.invoker.Invoke(callCtx, a, q.userPrompt(), q.Workdir)
	if err != nil {
		return Answer{}, fmt.Errorf("router: %s sub-question: %w", q.Kind, err)
	}

	return Answer{
		Result:       res.ResultText,
		InputTokens:  res.InputTokens,
		OutputTokens: res.OutputTokens,
		ModelUsed:    r.model,
	}, nil
}

// record writes a RouterMetric for one Ask. A recording failure is telemetry
// only — it is swallowed so a logging glitch never fails the routed question
// (correctness over throughput).
func (r *Router) record(ctx context.Context, q SubQuestion, ans Answer, latencyMs int64, hit bool) {
	if r.recorder == nil {
		return
	}
	_ = r.recorder.RecordRouter(ctx, telemetry.RouterMetric{
		NebulaID:       r.NebulaID,
		PhaseID:        r.PhaseID,
		SubKind:        string(q.Kind),
		HaikuInTokens:  ans.InputTokens,
		HaikuOutTokens: ans.OutputTokens,
		LatencyMs:      latencyMs,
		CacheHit:       hit,
	})
}

// elapsedMs returns milliseconds elapsed since start using the Router's clock.
func (r *Router) elapsedMs(start time.Time) int64 {
	return r.now().Sub(start).Milliseconds()
}

// cacheKey derives a stable LRU key from the fields that determine the answer.
// Scope is joined as-is (callers pass it in a consistent order); a different
// scope orientation is a different question and correctly misses the cache.
func (q SubQuestion) cacheKey() string {
	h := sha256.New()
	h.Write([]byte(q.Kind))
	h.Write([]byte{0})
	h.Write([]byte(q.Query))
	h.Write([]byte{0})
	h.Write([]byte(q.Workdir))
	h.Write([]byte{0})
	h.Write([]byte(strings.Join(q.Scope, "\x00")))
	return hex.EncodeToString(h.Sum(nil))
}

// userPrompt renders the sub-question into the prompt Haiku receives, appending
// the scope as an explicit constraint when present.
func (q SubQuestion) userPrompt() string {
	var b strings.Builder
	b.WriteString(q.Query)
	if len(q.Scope) > 0 {
		b.WriteString("\n\nLimit your search to these paths:\n")
		for _, s := range q.Scope {
			b.WriteString("- ")
			b.WriteString(s)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// systemPromptFor returns the per-kind rubric handed to Haiku so it answers in
// the structured shape the coder expects. An unknown kind gets a generic
// instruction rather than failing the call.
func systemPromptFor(kind SubKind) string {
	switch kind {
	case SubKindFileFinder:
		return "You locate where a Go symbol is declared or implemented. " +
			"Answer with a single line in the form <path>:<line> and nothing else."
	case SubKindTestMapper:
		return "You map a Go file or function to the tests that cover it. " +
			"Answer with one <path>:<TestFuncName> per line and nothing else."
	case SubKindLintTriage:
		return "You triage linter or compiler output. " +
			"Answer with a single JSON object: " +
			`{"file","line","severity","category","summary"} and nothing else.`
	case SubKindSymbolFinder:
		return "You name the Go package that owns a symbol. " +
			"Answer with the package name on one line and nothing else."
	default:
		return "Answer the question concisely with only the requested result."
	}
}

// resultCache is a fixed-capacity, mutex-guarded LRU keyed by sub-question hash.
// It bounds memory while capturing the heavy within-phase reuse of routed
// questions. The zero value is unusable; construct via newResultCache.
type resultCache struct {
	mu       sync.Mutex
	capacity int
	ll       *list.List               // front = most recently used
	items    map[string]*list.Element // key -> element holding *cacheEntry
}

type cacheEntry struct {
	key    string
	answer Answer
}

// newResultCache returns an LRU holding at most capacity entries. A capacity of
// zero or less disables caching (every get misses).
func newResultCache(capacity int) *resultCache {
	return &resultCache{
		capacity: capacity,
		ll:       list.New(),
		items:    make(map[string]*list.Element),
	}
}

// get returns the cached answer for key and marks it most-recently-used.
func (c *resultCache) get(key string) (Answer, bool) {
	if c.capacity <= 0 {
		return Answer{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*cacheEntry).answer, true
	}
	return Answer{}, false
}

// put inserts or refreshes key, evicting the least-recently-used entry when the
// cache is over capacity.
func (c *resultCache) put(key string, ans Answer) {
	if c.capacity <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value.(*cacheEntry).answer = ans
		c.ll.MoveToFront(el)
		return
	}
	el := c.ll.PushFront(&cacheEntry{key: key, answer: ans})
	c.items[key] = el
	if c.ll.Len() > c.capacity {
		c.evictOldest()
	}
}

// evictOldest removes the least-recently-used entry. The caller holds c.mu.
func (c *resultCache) evictOldest() {
	el := c.ll.Back()
	if el == nil {
		return
	}
	c.ll.Remove(el)
	delete(c.items, el.Value.(*cacheEntry).key)
}
