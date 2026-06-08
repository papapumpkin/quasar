package claude

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/telemetry"
)

// TestCacheReuseAcrossInvocations is the end-to-end check tying the invoker to
// the metric store: two consecutive invocations of the same phase should
// populate then reuse the cache, so the second invocation reports
// cache_read > 0 and the pooled global hit ratio is positive.
func TestCacheReuseAcrossInvocations(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "count")

	// Fake claude: cold cache (all fresh tokens) on the first call, warm cache
	// (tokens served from cache) on every call thereafter, tracked via a file.
	cold := `{"result":"ok","usage":{"input_tokens":1000,"cache_creation_input_tokens":900,"cache_read_input_tokens":0}}`
	warm := `{"result":"ok","usage":{"input_tokens":100,"cache_creation_input_tokens":0,"cache_read_input_tokens":900}}`
	body := "n=$(cat '" + counter + "' 2>/dev/null || echo 0)\n" +
		"n=$((n+1)); echo $n > '" + counter + "'\n" +
		"if [ \"$n\" -eq 1 ]; then printf '%s' '" + cold + "'; else printf '%s' '" + warm + "'; fi\n"
	script := writeScript(t, dir, "claude", body)

	inv := newTestInvoker("claude", false, fakeExecContextWith(script), nil)
	store := telemetry.NewCacheMetricStore(filepath.Join(dir, "cache_metrics.jsonl"))
	ctx := context.Background()
	a := agent.Agent{SystemPrompt: "stable prefix", CacheOptimization: true}

	var second agent.InvocationResult
	for cycle := 0; cycle < 2; cycle++ {
		res, err := inv.Invoke(ctx, a, "user prompt", dir)
		if err != nil {
			t.Fatalf("invoke cycle %d: %v", cycle, err)
		}
		second = res
		if err := store.Record(ctx, telemetry.CacheMetric{
			NebulaID:    "neb-e2e",
			PhaseID:     "phase-1",
			CycleN:      cycle,
			InputTokens: res.InputTokens,
			CacheCreate: res.CacheCreationTokens,
			CacheRead:   res.CacheReadTokens,
		}); err != nil {
			t.Fatalf("record cycle %d: %v", cycle, err)
		}
	}

	if second.CacheReadTokens <= 0 {
		t.Errorf("second invocation CacheReadTokens = %d, want > 0", second.CacheReadTokens)
	}

	metrics, err := store.MetricsByNebula(ctx, "neb-e2e")
	if err != nil {
		t.Fatalf("MetricsByNebula: %v", err)
	}
	if global := telemetry.HitRatioFor(metrics); global <= 0 {
		t.Errorf("global hit ratio = %v, want > 0", global)
	}
}
