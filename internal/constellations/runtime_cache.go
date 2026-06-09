package constellations

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/gitops"
	"github.com/papapumpkin/quasar/internal/repos"
)

// RuntimeCacheOpts configures the per-repo Runtime cache the trigger-queue
// supervisor uses to route a Fire call to the correct repo's runtime.
//
// DB / Blobs / Invoker are shared across every per-repo Runtime — they live
// once at the fabric layer. Loader resolution, gitops Client, working
// directory, and pre-commit policy are derived per-repo when a Runtime is
// first requested.
type RuntimeCacheOpts struct {
	// DB is the shared fabric database. Required.
	DB *sql.DB
	// Blobs is the shared content-addressed blob store, used by the nebula
	// store. Required.
	Blobs *blobstore.Store
	// Invoker is the shared claude CLI invoker. Required.
	Invoker agent.Invoker
	// DefaultBudgetUSD is the operator-level fallback budget cap. Used when
	// neither the nebula manifest nor a per-run override sets one. Zero
	// means uncapped.
	DefaultBudgetUSD float64
	// PreCommitFor, when non-nil, returns the pre-commit policy for a repo
	// path. Each per-repo Runtime threads the result through every
	// gitops.Commit call. A nil resolver means no pre-commit; a non-nil
	// resolver that errors is logged and the Runtime is still constructed
	// with an empty policy.
	PreCommitFor func(repoPath string) (gitops.PreCommitConfig, error)
}

// RuntimeCache lazily constructs and caches one *Runtime per repo. It is the
// missing piece between the supervisor (which holds a single Firer) and the
// per-repo binding *Runtime requires (workDir, gitops Client, pre-commit).
//
// Construction failures are NOT memoized: a transient config-load error must
// not kill the repo for the session. The next Get retries.
type RuntimeCache struct {
	opts     RuntimeCacheOpts
	runStore *fabric.ConstellationRunStore
	nebStore *fabric.NebulaStore
	entStore *fabric.EntanglementStore

	mu       sync.Mutex
	runtimes map[string]*Runtime
}

// NewRuntimeCache builds the shared stores once and returns a cache ready to
// hand out per-repo Runtimes on Get.
func NewRuntimeCache(opts RuntimeCacheOpts) (*RuntimeCache, error) {
	if opts.DB == nil {
		return nil, fmt.Errorf("constellations: RuntimeCache requires DB")
	}
	if opts.Blobs == nil {
		return nil, fmt.Errorf("constellations: RuntimeCache requires Blobs")
	}
	if opts.Invoker == nil {
		return nil, fmt.Errorf("constellations: RuntimeCache requires Invoker")
	}
	return &RuntimeCache{
		opts:     opts,
		runStore: fabric.NewConstellationRunStore(opts.DB),
		nebStore: fabric.NewNebulaStore(opts.DB, opts.Blobs),
		entStore: fabric.NewEntanglementStore(opts.DB),
		runtimes: make(map[string]*Runtime),
	}, nil
}

// Get returns the Runtime for repoPath, constructing it on first request. The
// returned Runtime is bound to repoPath's working directory, gitops Client,
// and pre-commit policy; subsequent Gets for the same path return the
// cached instance.
func (c *RuntimeCache) Get(_ context.Context, repoPath string) (*Runtime, error) {
	if repoPath == "" {
		return nil, fmt.Errorf("constellations: RuntimeCache.Get requires a non-empty repoPath")
	}
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("constellations: resolve %q: %w", repoPath, err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if rt, ok := c.runtimes[abs]; ok {
		return rt, nil
	}

	resolver, err := repos.NewResolver(&repos.Repo{Path: abs})
	if err != nil {
		return nil, fmt.Errorf("constellations: build resolver for %q: %w", abs, err)
	}
	loader := artifacts.New(resolver)

	var preCommit gitops.PreCommitConfig
	if c.opts.PreCommitFor != nil {
		if pc, err := c.opts.PreCommitFor(abs); err == nil {
			preCommit = pc
		}
		// A pre-commit lookup failure degrades to an empty policy. The Runtime
		// will still serve fires; commits in that repo just skip the gate.
		// Surfacing the error here would block the whole repo on a config
		// typo, which is worse than running without the gate.
	}

	rt := New(RuntimeOpts{
		RunStore:         c.runStore,
		NebStore:         c.nebStore,
		Loader:           loader,
		Invoker:          c.opts.Invoker,
		Committer:        gitops.New(abs),
		RepoPath:         abs,
		PreCommit:        preCommit,
		DefaultBudgetUSD: c.opts.DefaultBudgetUSD,
		Entanglements:    c.entStore,
	})
	c.runtimes[abs] = rt
	return rt, nil
}

// RuntimeCacheFirer satisfies the supervisor's Firer interface by routing
// each call through the cache. A repoPath without a Runtime triggers
// on-demand construction; failures propagate as fire errors so the
// supervisor's mark-consumed-with-log policy applies.
type RuntimeCacheFirer struct {
	Cache *RuntimeCache
}

// Fire resolves the per-repo Runtime and dispatches the trigger. The empty
// parent_run_id and zero budgetOverride are correct for a trigger row: it
// launches a top-level run, with budget falling back to the nebula manifest.
func (f *RuntimeCacheFirer) Fire(ctx context.Context, repoPath, constellationName, nebulaID string) (string, error) {
	rt, err := f.Cache.Get(ctx, repoPath)
	if err != nil {
		return "", fmt.Errorf("firer: get runtime for %q: %w", repoPath, err)
	}
	return rt.Fire(ctx, constellationName, nebulaID, "", 0)
}
