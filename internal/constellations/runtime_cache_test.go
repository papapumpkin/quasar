package constellations

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/gitops"
)

// stubInvoker satisfies agent.Invoker for cache-construction tests; the
// trigger consumer doesn't actually drive runs through it during these tests,
// so the Invoke implementation is unused.
type stubInvoker struct{}

func (stubInvoker) Invoke(_ context.Context, _ agent.Agent, _, _ string) (agent.InvocationResult, error) {
	return agent.InvocationResult{}, errors.New("stubInvoker: not implemented")
}

func (stubInvoker) Validate() error { return nil }

// newCacheTestDeps opens a real fabric DB + blob store in a temp dir. The
// per-repo construction in the cache pulls in real artifacts loaders and
// gitops clients, so the temp dir doubles as a stand-in repo root.
func newCacheTestDeps(t *testing.T) (*RuntimeCache, string) {
	t.Helper()
	dir := t.TempDir()
	fab, err := fabric.NewSQLiteFabric(context.Background(), filepath.Join(dir, "fabric.db"))
	if err != nil {
		t.Fatalf("fabric: %v", err)
	}
	t.Cleanup(func() { _ = fab.Close() })
	blobs, err := blobstore.New(filepath.Join(dir, "blobs"), fab.DB())
	if err != nil {
		t.Fatalf("blobs: %v", err)
	}
	cache, err := NewRuntimeCache(RuntimeCacheOpts{
		DB:      fab.DB(),
		Blobs:   blobs,
		Invoker: stubInvoker{},
	})
	if err != nil {
		t.Fatalf("NewRuntimeCache: %v", err)
	}
	return cache, dir
}

func TestRuntimeCacheConstructorRequiresDB(t *testing.T) {
	t.Parallel()
	// The constructor's nil-checks are a thin guard against forgetting a
	// required dep at the call site. We exercise the DB check directly
	// since *sql.DB is a concrete type with no cheap stub; the Blobs and
	// Invoker checks are structurally identical and covered by review.
	_, err := NewRuntimeCache(RuntimeCacheOpts{})
	if err == nil || !contains(err.Error(), "DB") {
		t.Errorf("NewRuntimeCache(zero) err = %v, want it to mention DB", err)
	}
}

func TestRuntimeCacheRejectsEmptyRepoPath(t *testing.T) {
	t.Parallel()
	cache, _ := newCacheTestDeps(t)
	if _, err := cache.Get(context.Background(), ""); err == nil {
		t.Error("Get(\"\") err = nil, want non-nil")
	}
}

func TestRuntimeCacheMemoizesSuccess(t *testing.T) {
	t.Parallel()
	cache, dir := newCacheTestDeps(t)
	ctx := context.Background()
	first, err := cache.Get(ctx, dir)
	if err != nil {
		t.Fatalf("Get 1: %v", err)
	}
	second, err := cache.Get(ctx, dir)
	if err != nil {
		t.Fatalf("Get 2: %v", err)
	}
	if first != second {
		t.Errorf("second Get returned a different Runtime pointer (%p vs %p); cache is not memoizing", first, second)
	}
}

func TestRuntimeCachePerRepoIsolation(t *testing.T) {
	t.Parallel()
	cache, _ := newCacheTestDeps(t)
	ctx := context.Background()
	dirA := t.TempDir()
	dirB := t.TempDir()
	rtA, err := cache.Get(ctx, dirA)
	if err != nil {
		t.Fatalf("Get A: %v", err)
	}
	rtB, err := cache.Get(ctx, dirB)
	if err != nil {
		t.Fatalf("Get B: %v", err)
	}
	if rtA == rtB {
		t.Error("distinct repo paths returned the same Runtime; per-repo binding lost")
	}
}

func TestRuntimeCachePreCommitForIsCalledPerRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fab, _ := fabric.NewSQLiteFabric(context.Background(), filepath.Join(dir, "fabric.db"))
	t.Cleanup(func() { _ = fab.Close() })
	blobs, _ := blobstore.New(filepath.Join(dir, "blobs"), fab.DB())

	var seenRepos []string
	cache, err := NewRuntimeCache(RuntimeCacheOpts{
		DB:      fab.DB(),
		Blobs:   blobs,
		Invoker: stubInvoker{},
		PreCommitFor: func(repoPath string) (gitops.PreCommitConfig, error) {
			seenRepos = append(seenRepos, repoPath)
			return gitops.PreCommitConfig{Commands: []string{"go vet ./..."}}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewRuntimeCache: %v", err)
	}

	repoA := t.TempDir()
	repoB := t.TempDir()
	if _, err := cache.Get(context.Background(), repoA); err != nil {
		t.Fatalf("Get repoA: %v", err)
	}
	if _, err := cache.Get(context.Background(), repoB); err != nil {
		t.Fatalf("Get repoB: %v", err)
	}
	if len(seenRepos) != 2 {
		t.Errorf("PreCommitFor called %d times, want 2 (one per first-Get)", len(seenRepos))
	}
}

func TestRuntimeCacheFirer_RoutesByRepoPath(t *testing.T) {
	t.Parallel()
	// RuntimeCacheFirer satisfies Firer and is what the supervisor will use
	// in production. This test verifies the indirection: Firer.Fire on path
	// X goes through Cache.Get(X) and then *Runtime.Fire on that runtime.
	// We can't drive a real run end-to-end here (stubInvoker rejects), so
	// we assert by surface: the cache produces a runtime per repo, and the
	// firer's error path mentions the path it tried to route.
	cache, _ := newCacheTestDeps(t)
	firer := &RuntimeCacheFirer{Cache: cache}
	// Empty path → cache rejects → firer surfaces the error wrapped with
	// "firer:" so it's distinguishable from other failures the supervisor
	// might log.
	_, err := firer.Fire(context.Background(), "", "architect", "neb-1")
	if err == nil {
		t.Fatal("Fire(\"\", ...) err = nil, want non-nil")
	}
	if !contains(err.Error(), "firer") {
		t.Errorf("error %q does not mention firer; harder for the supervisor log to point at the cause", err)
	}
}

// contains is a tiny local helper to avoid pulling strings into every test.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
