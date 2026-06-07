package cmd

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/telemetry"
)

// newCacheReportCmd builds an isolated cobra command wired to runCacheReport so
// tests can capture output without touching the global command tree.
func newCacheReportCmd(out *bytes.Buffer) *cobra.Command {
	c := &cobra.Command{RunE: runCacheReport}
	c.Flags().String("nebula", "", "")
	c.SetOut(out)
	c.SetContext(context.Background())
	return c
}

func TestRunCacheReport_PerPhaseAndGlobal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache_metrics.jsonl")

	store := telemetry.NewCacheMetricStore(path)
	ctx := context.Background()
	records := []telemetry.CacheMetric{
		{NebulaID: "n1", PhaseID: "p1", CycleN: 0, InputTokens: 1000, CacheRead: 0},
		{NebulaID: "n1", PhaseID: "p1", CycleN: 1, InputTokens: 0, CacheRead: 1000},
		{NebulaID: "n1", PhaseID: "p2", CycleN: 0, InputTokens: 500, CacheRead: 500},
	}
	for _, r := range records {
		if err := store.Record(ctx, r); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	// Point the command at our temp log.
	orig := cacheMetricsPath
	cacheMetricsPath = path
	defer func() { cacheMetricsPath = orig }()

	var out bytes.Buffer
	c := newCacheReportCmd(&out)
	if err := c.Flags().Set("nebula", "n1"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if err := runCacheReport(c, nil); err != nil {
		t.Fatalf("runCacheReport: %v", err)
	}

	got := out.String()
	// Global pooled: read 1500 / (1500 + 1500) = 50%.
	for _, want := range []string{"global hit rate: 50%", "phase p1: 50%", "phase p2: 50%", "cycle 0:", "cycle 1:"} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q\n--- output ---\n%s", want, got)
		}
	}
}

func TestRunCacheReport_NoMetrics(t *testing.T) {
	orig := cacheMetricsPath
	cacheMetricsPath = filepath.Join(t.TempDir(), "absent.jsonl")
	defer func() { cacheMetricsPath = orig }()

	var out bytes.Buffer
	c := newCacheReportCmd(&out)
	if err := c.Flags().Set("nebula", "ghost"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if err := runCacheReport(c, nil); err != nil {
		t.Fatalf("runCacheReport: %v", err)
	}
	if !strings.Contains(out.String(), "no cache metrics recorded") {
		t.Errorf("expected empty-log message, got %q", out.String())
	}
}

func TestCacheMetricsPathDefault(t *testing.T) {
	want := filepath.Join(".quasar", "telemetry", "cache_metrics.jsonl")
	if cacheMetricsPath != want {
		t.Errorf("cacheMetricsPath = %q, want %q", cacheMetricsPath, want)
	}
}
