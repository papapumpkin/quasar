package constellations

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/fabric"
	"github.com/papapumpkin/quasar/internal/telemetry"
)

// TestRuntimeRecordsCacheMetrics verifies that a star dispatch persists the
// invocation's prompt-cache token counts to the configured CacheMetricStore.
func TestRuntimeRecordsCacheMetrics(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	fab, err := fabric.NewSQLiteFabric(ctx, filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("fabric: %v", err)
	}
	t.Cleanup(func() { _ = fab.DB().Close() })
	blobs, err := blobstore.New(filepath.Join(dir, "blobs"), fab.DB())
	if err != nil {
		t.Fatalf("blobs: %v", err)
	}
	nebStore := fabric.NewNebulaStore(fab.DB(), blobs)
	nebID, err := nebStore.Insert(ctx, fabric.NebulaRow{
		Name: "demo", Status: "running", ContextTOML: "do the thing",
	})
	if err != nil {
		t.Fatalf("seed nebula: %v", err)
	}

	con := &artifacts.Constellation{
		Name:  "code",
		Nodes: []artifacts.ConstellationNode{{ID: "coder", Type: artifacts.NodeStar, Star: "coder"}},
		Edges: []artifacts.ConstellationEdge{{From: "coder", To: artifacts.TermDone}},
	}
	loader := &fakeLoader{
		cons:  map[string]*artifacts.Constellation{"code": con},
		stars: map[string]*artifacts.Star{"coder": {Name: "coder", Prompt: "be a coder"}},
	}
	inv := &fakeInvoker{result: agent.InvocationResult{
		ResultText:          "did it",
		CostUSD:             0.5,
		InputTokens:         200,
		CacheCreationTokens: 1000,
		CacheReadTokens:     1800,
	}}

	store := telemetry.NewCacheMetricStore(filepath.Join(dir, "cache_metrics.jsonl"))
	rt := New(RuntimeOpts{
		RunStore:     fabric.NewConstellationRunStore(fab.DB()),
		NebStore:     nebStore,
		Loader:       loader,
		Invoker:      inv,
		RepoPath:     dir,
		CacheMetrics: store,
	})

	runID, err := rt.Fire(ctx, "code", nebID, "", 0)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if _, err := rt.Step(ctx, runID); err != nil {
		t.Fatalf("Step: %v", err)
	}

	metrics, err := store.MetricsByNebula(ctx, nebID)
	if err != nil {
		t.Fatalf("MetricsByNebula: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 recorded metric, got %d", len(metrics))
	}
	m := metrics[0]
	if m.PhaseID != "coder" {
		t.Errorf("PhaseID = %q, want %q", m.PhaseID, "coder")
	}
	if m.InputTokens != 200 || m.CacheCreate != 1000 || m.CacheRead != 1800 {
		t.Errorf("token counts wrong: %+v", m)
	}
	if m.CacheRead <= 0 {
		t.Errorf("expected positive cache read, got %d", m.CacheRead)
	}
}
