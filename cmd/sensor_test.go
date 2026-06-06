package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/sensors"
)

// TestSeedNebulaInserter verifies the cmd-layer adapter maps a sensors.SeedNebula
// onto a fabric.NebulaRow and persists it with the sensor-generated status and
// source provenance intact.
func TestSeedNebulaInserter(t *testing.T) {
	ctx := context.Background()
	store, _, fab := newImportFixture(t)

	repoDir := t.TempDir()
	registerRepoRow(t, fab, repoDir)

	inserter := &seedNebulaInserter{store: store}
	id, err := inserter.Insert(ctx, sensors.SeedNebula{
		RepoPath:    repoDir,
		Name:        "Fix the widget",
		Description: "the widget is broken",
		SourceName:  "github",
		SourceID:    "papapumpkin/quasar#42",
		SourceURL:   "https://github.com/papapumpkin/quasar/issues/42",
		Status:      sensors.SeedStatus,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != sensors.SeedStatus {
		t.Errorf("status = %q, want %q", got.Status, sensors.SeedStatus)
	}
	if got.SourceName != "github" || got.SourceID != "papapumpkin/quasar#42" {
		t.Errorf("source = %q/%q, want github/papapumpkin/quasar#42", got.SourceName, got.SourceID)
	}
	if got.SourceURL != "https://github.com/papapumpkin/quasar/issues/42" {
		t.Errorf("source url = %q, want issue url", got.SourceURL)
	}
	if got.Name != "Fix the widget" || got.RepoPath != repoDir {
		t.Errorf("nebula = %q/%q, want %q/%q", got.Name, got.RepoPath, "Fix the widget", repoDir)
	}
	if len(got.Phases) != 0 {
		t.Errorf("phases = %d, want 0 (seed nebulas carry no phases)", len(got.Phases))
	}
}

// TestSeedNebulaInserterRendersContext verifies the adapter renders the
// sensor-derived goals and constraints into the row's context TOML so the
// architect inherits them, and omits the block entirely when both are empty.
func TestSeedNebulaInserterRendersContext(t *testing.T) {
	ctx := context.Background()
	store, _, fab := newImportFixture(t)
	repoDir := t.TempDir()
	registerRepoRow(t, fab, repoDir)

	inserter := &seedNebulaInserter{store: store}
	id, err := inserter.Insert(ctx, sensors.SeedNebula{
		RepoPath:    repoDir,
		Name:        "n",
		Status:      sensors.SeedStatus,
		Goals:       []string{"ship it"},
		Constraints: []string{"no breaking changes"},
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	for _, want := range []string{"ship it", "no breaking changes", "goals", "constraints"} {
		if !strings.Contains(got.ContextTOML, want) {
			t.Errorf("context toml %q missing %q", got.ContextTOML, want)
		}
	}

	// No goals/constraints → no context block.
	emptyTOML, err := renderSeedContextTOML(nil, nil)
	if err != nil {
		t.Fatalf("renderSeedContextTOML: %v", err)
	}
	if emptyTOML != "" {
		t.Errorf("empty context = %q, want \"\"", emptyTOML)
	}
}

// TestPrintSensorPollResult verifies the summary renders to stderr (never
// stdout) and lists each seeded nebula id.
func TestPrintSensorPollResult(t *testing.T) {
	var stdout, stderr strings.Builder
	cmd := &cobra.Command{}
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	printSensorPollResult(cmd, "/repos/quasar", "github_issues", sensors.PollResult{
		Observed:  3,
		Seeded:    2,
		Fired:     1,
		Queued:    1,
		NebulaIDs: []string{"nebula-1-a", "nebula-2-b"},
	})

	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty (human output goes to stderr)", stdout.String())
	}
	out := stderr.String()
	for _, want := range []string{
		"sensor /repos/quasar/github_issues",
		"observed=3 seeded=2 fired=1 queued=1",
		"nebula-1-a",
		"nebula-2-b",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr %q missing %q", out, want)
		}
	}
}

// TestSensorPollCommandRegistered verifies the hidden `sensor poll` command is
// wired with the expected two-argument contract.
func TestSensorPollCommandRegistered(t *testing.T) {
	poll, _, err := sensorCmd.Find([]string{"poll"})
	if err != nil {
		t.Fatalf("find poll: %v", err)
	}
	if poll.Name() != "poll" {
		t.Fatalf("found %q, want poll", poll.Name())
	}
	if !sensorCmd.Hidden {
		t.Error("sensor command should be hidden (admin/debugging only)")
	}
	if err := poll.Args(poll, []string{"one"}); err == nil {
		t.Error("poll should reject a single argument; needs <repo-path> <sensor-name>")
	}
	if err := poll.Args(poll, []string{"repo", "sensor"}); err != nil {
		t.Errorf("poll should accept exactly two arguments: %v", err)
	}
}

// compile-time assertion that the adapter satisfies the scheduler's consumer
// interface, mirroring the production wiring.
var _ sensors.NebulaInserter = (*seedNebulaInserter)(nil)
