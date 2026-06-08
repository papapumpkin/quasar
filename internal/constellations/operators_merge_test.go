package constellations

import (
	"context"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/gitops"
)

// fakeMerger is a test seam for opMergeAttempt: it returns a canned outcome and
// records the TryOpts it received and whether Cleanup ran.
type fakeMerger struct {
	outcome   gitops.MergeOutcome
	err       error
	gotOpts   gitops.TryOpts
	cleanedUp string
}

func (f *fakeMerger) Try(_ context.Context, opts gitops.TryOpts) (gitops.MergeOutcome, error) {
	f.gotOpts = opts
	return f.outcome, f.err
}

func (f *fakeMerger) Cleanup(_ context.Context, worktree string) { f.cleanedUp = worktree }

func TestOpMergeAttempt(t *testing.T) {
	t.Run("maps each outcome into the output schema", func(t *testing.T) {
		cases := []struct {
			name        string
			outcome     gitops.MergeOutcome
			wantCleanup bool
		}{
			{
				name: "clean",
				outcome: gitops.MergeOutcome{
					Result: gitops.MergeClean, MergedSHA: "abc123", Worktree: "/wt/clean",
				},
				wantCleanup: true,
			},
			{
				name: "markers",
				outcome: gitops.MergeOutcome{
					Result: gitops.MergeMarkers, ConflictedFiles: []string{"a.go", "b.go"}, Worktree: "/wt/markers",
				},
				wantCleanup: false,
			},
			{
				name: "build_failure",
				outcome: gitops.MergeOutcome{
					Result: gitops.MergeBuildFailure, BuildOutput: "boom", MergedSHA: "def456", Worktree: "/wt/bf",
				},
				wantCleanup: false,
			},
			{
				name: "merge_error",
				outcome: gitops.MergeOutcome{
					Result: gitops.MergeError, Worktree: "/wt/err",
				},
				wantCleanup: true,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				fm := &fakeMerger{outcome: tc.outcome}
				rt := &Runtime{merger: fm}
				out, err := opMergeAttempt(context.Background(), rt, nil, map[string]any{
					"src_branch": "quasar/feature",
					"dst_branch": "main",
				})
				if err != nil {
					t.Fatalf("opMergeAttempt: %v", err)
				}
				if got := out["result"]; got != string(tc.outcome.Result) {
					t.Errorf("result = %v, want %v", got, tc.outcome.Result)
				}
				if got := out["merged_sha"]; got != tc.outcome.MergedSHA {
					t.Errorf("merged_sha = %v, want %v", got, tc.outcome.MergedSHA)
				}
				if got := out["worktree_path"]; got != tc.outcome.Worktree {
					t.Errorf("worktree_path = %v, want %v", got, tc.outcome.Worktree)
				}
				if got := out["build_output"]; got != tc.outcome.BuildOutput {
					t.Errorf("build_output = %v, want %v", got, tc.outcome.BuildOutput)
				}
				files, ok := out["conflicted_files"].([]string)
				if !ok {
					t.Fatalf("conflicted_files is %T, want []string", out["conflicted_files"])
				}
				if len(files) != len(tc.outcome.ConflictedFiles) {
					t.Errorf("conflicted_files = %v, want %v", files, tc.outcome.ConflictedFiles)
				}
				cleaned := fm.cleanedUp == tc.outcome.Worktree
				if cleaned != tc.wantCleanup {
					t.Errorf("cleanup ran = %v (path %q), want %v", cleaned, fm.cleanedUp, tc.wantCleanup)
				}
			})
		}
	})

	t.Run("missing branches error", func(t *testing.T) {
		rt := &Runtime{merger: &fakeMerger{}}
		if _, err := opMergeAttempt(context.Background(), rt, nil, map[string]any{"src_branch": "x"}); err == nil {
			t.Fatal("expected error when dst_branch is missing")
		}
	})

	t.Run("verify_command, verify_timeout, and run_id are threaded into TryOpts", func(t *testing.T) {
		fm := &fakeMerger{outcome: gitops.MergeOutcome{Result: gitops.MergeClean}}
		rt := &Runtime{merger: fm}
		_, err := opMergeAttempt(context.Background(), rt, nil, map[string]any{
			"src_branch":     "quasar/feature",
			"dst_branch":     "main",
			"verify_command": "make ci",
			"verify_timeout": "5m",
			"run_id":         "run-xyz",
		})
		if err != nil {
			t.Fatalf("opMergeAttempt: %v", err)
		}
		if fm.gotOpts.VerifyCommand != "make ci" {
			t.Errorf("VerifyCommand = %q, want %q", fm.gotOpts.VerifyCommand, "make ci")
		}
		if fm.gotOpts.Timeout != 5*time.Minute {
			t.Errorf("Timeout = %v, want 5m", fm.gotOpts.Timeout)
		}
		if fm.gotOpts.RunID != "run-xyz" {
			t.Errorf("RunID = %q, want %q", fm.gotOpts.RunID, "run-xyz")
		}
		if !fm.gotOpts.KeepWorktree {
			t.Error("KeepWorktree should be set so a resolver can inherit the worktree")
		}
	})

	t.Run("malformed verify_timeout errors", func(t *testing.T) {
		rt := &Runtime{merger: &fakeMerger{}}
		if _, err := opMergeAttempt(context.Background(), rt, nil, map[string]any{
			"src_branch":     "quasar/feature",
			"dst_branch":     "main",
			"verify_timeout": "soon",
		}); err == nil {
			t.Fatal("expected error for an unparseable verify_timeout")
		}
	})
}

func TestOpFulfillEntanglements(t *testing.T) {
	t.Run("no entanglement store is a no-op passthrough", func(t *testing.T) {
		rt := &Runtime{}
		out, err := opFulfillEntanglements(context.Background(), rt, nil, map[string]any{
			"merged_sha": "abc", "run_id": "run-1",
		})
		if err != nil {
			t.Fatalf("opFulfillEntanglements: %v", err)
		}
		if out["fulfilled"] != false {
			t.Errorf("fulfilled = %v, want false", out["fulfilled"])
		}
		if out["merged_sha"] != "abc" {
			t.Errorf("merged_sha = %v, want abc", out["merged_sha"])
		}
	})

	t.Run("fulfills the producing run's in_flight entanglements", func(t *testing.T) {
		ctx := context.Background()
		rt, entStore, _ := newEntanglementRuntime(t)
		const runID = "run-fulfill"
		seedInFlight(t, entStore, runID, "Sensor")

		out, err := opFulfillEntanglements(ctx, rt, nil, map[string]any{
			"merged_sha": "sha1", "run_id": runID,
		})
		if err != nil {
			t.Fatalf("opFulfillEntanglements: %v", err)
		}
		if out["fulfilled"] != true {
			t.Fatalf("fulfilled = %v, want true", out["fulfilled"])
		}
		assertStatus(t, entStore, "Sensor", "fulfilled")
	})

	t.Run("empty run_id with a store is a no-op", func(t *testing.T) {
		rt, _, _ := newEntanglementRuntime(t)
		out, err := opFulfillEntanglements(context.Background(), rt, nil, map[string]any{"merged_sha": "x"})
		if err != nil {
			t.Fatalf("opFulfillEntanglements: %v", err)
		}
		if out["fulfilled"] != false {
			t.Errorf("fulfilled = %v, want false", out["fulfilled"])
		}
	})
}
