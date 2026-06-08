package cmd

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/telemetry"
)

// newConflictsReportCmd builds an isolated cobra command wired to
// runConflictsReport so tests can set --since and capture output without
// touching the global command tree.
func newConflictsReportCmd(out *bytes.Buffer) *cobra.Command {
	c := &cobra.Command{RunE: runConflictsReport}
	c.Flags().Duration("since", 7*24*time.Hour, "")
	c.SetOut(out)
	c.SetContext(context.Background())
	return c
}

func TestRunConflictsReport_Summary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conflict_resolutions.jsonl")

	log := telemetry.NewConflictResolutionLog(path)
	ctx := context.Background()
	events := []telemetry.ConflictResolutionEvent{
		{Mode: "markers", Status: "resolved", Cycles: 1, FilesChanged: 2, Files: []string{"internal/sensors/sensors.go"}, CostUSD: 0.40, LatencyMs: 18000},
		{Mode: "no_markers", Status: "needs_human", Cycles: 2, Files: []string{"internal/sensors/sensors.go", "go.mod"}, CostUSD: 0.60, LatencyMs: 22000},
	}
	for _, ev := range events {
		if err := log.Record(ctx, ev); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	orig := conflictResolutionLogPath
	conflictResolutionLogPath = path
	defer func() { conflictResolutionLogPath = orig }()

	var out bytes.Buffer
	c := newConflictsReportCmd(&out)
	if err := runConflictsReport(c, nil); err != nil {
		t.Fatalf("runConflictsReport: %v", err)
	}

	got := out.String()
	// 1 of 2 resolved = 50%; mean cost (0.40+0.60)/2 = $0.50; mean latency
	// (18000+22000)/2 = 20000ms; sensors.go appears in both rows.
	for _, want := range []string{
		"2 total, 1 resolved (50% resolution rate)",
		"Average cost: $0.50 per resolution",
		"Average latency: 20000 ms per resolution",
		"internal/sensors/sensors.go: 2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q\n--- output ---\n%s", want, got)
		}
	}
}

func TestRunConflictsReport_NoResolutions(t *testing.T) {
	orig := conflictResolutionLogPath
	conflictResolutionLogPath = filepath.Join(t.TempDir(), "absent.jsonl")
	defer func() { conflictResolutionLogPath = orig }()

	var out bytes.Buffer
	c := newConflictsReportCmd(&out)
	if err := runConflictsReport(c, nil); err != nil {
		t.Fatalf("runConflictsReport: %v", err)
	}
	if !strings.Contains(out.String(), "no conflict resolutions") {
		t.Errorf("expected empty-log message, got %q", out.String())
	}
}

func TestConflictResolutionLogPathDefault(t *testing.T) {
	want := filepath.Join(".quasar", "telemetry", "conflict_resolutions.jsonl")
	if conflictResolutionLogPath != want {
		t.Errorf("conflictResolutionLogPath = %q, want %q", conflictResolutionLogPath, want)
	}
}
