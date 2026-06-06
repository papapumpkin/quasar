package fleet

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// updateGolden regenerates the golden files when set: go test ./... -update-golden.
// The flag is named -update-golden rather than -update because the teatest
// package (used by the keymap tests) registers a global -update flag of its own.
var updateGolden = flag.Bool("update-golden", false, "update golden files")

// sampleFleets returns deterministic fixture fleets keyed by golden-file name.
func sampleFleets() map[string]Fleet {
	multi := Fleet{Repos: []RepoLane{
		{
			DisplayName: "papapumpkin/quasar",
			AwaitingApproval: []NebulaCard{
				{ID: "n1", Title: "add gh-rate-limit retry", SourceLabel: "#142", Status: "awaiting_approval", Age: time.Hour},
				{ID: "n2", Title: "fix flaky scheduler test", SourceLabel: "#143", Status: "awaiting_approval", Age: 2 * time.Hour},
			},
			InFlight: []RunCard{
				{RunID: "run-7c2abcd1", ConstellationName: "coder-reviewer", CurrentNode: "reviewer", StepIndex: 3, StepCount: 5, State: "running"},
			},
			Recent: []NebulaCard{
				{ID: "n3", Title: "earlier work", Status: "merged", PRNumber: 441, PRStatus: "merged"},
				{ID: "n4", Title: "budget overflow", Status: "failed"},
			},
		},
		{
			DisplayName: "papapumpkin/relativity",
			AwaitingApproval: []NebulaCard{
				{ID: "n5", Title: "split coordinator state mgmt", SourceLabel: "#88", Status: "awaiting_approval", Age: 30 * time.Minute},
			},
			Recent: []NebulaCard{
				{ID: "n6", Title: "done earlier", Status: "merged", PRNumber: 12, PRStatus: "merged"},
			},
		},
	}}
	single := Fleet{Repos: multi.Repos[:1]}
	return map[string]Fleet{
		"empty.golden":  {},
		"single.golden": single,
		"multi.golden":  multi,
	}
}

func TestRenderFleetGolden(t *testing.T) {
	for name, f := range sampleFleets() {
		name, f := name, f
		t.Run(name, func(t *testing.T) {
			got := RenderFleet(f, 110, Selection{})
			path := filepath.Join("testdata", name)
			if *updateGolden {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run with -update): %v", err)
			}
			if got != string(want) {
				t.Errorf("render mismatch for %s:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
			}
		})
	}
}

func TestRenderFoldedRepoHidesCards(t *testing.T) {
	t.Parallel()
	f := Fleet{Repos: []RepoLane{{
		DisplayName:      "org/a",
		Folded:           true,
		AwaitingApproval: []NebulaCard{{Title: "hidden", SourceLabel: "#1"}},
	}}}
	out := RenderFleet(f, 90, Selection{})
	if strings.Contains(out, "hidden") {
		t.Error("folded repo should not render its cards")
	}
}

// TestRenderFleetCursorGolden covers a non-zero cursor: the second awaiting
// card of the first repo is selected and must carry the selection marker, while
// every other card stays unmarked.
func TestRenderFleetCursorGolden(t *testing.T) {
	f := sampleFleets()["multi.golden"]
	sel := Selection{Lane: 0, RepoIdx: 0, CardIdx: 1, Active: true}
	got := RenderFleet(f, 110, sel)

	if !strings.Contains(got, selectGutter+"#143 fix flaky scheduler test") {
		t.Errorf("selected card should carry the %q marker:\n%s", selectGutter, got)
	}
	if strings.Contains(got, selectGutter+"#142 add gh-rate-limit retry") {
		t.Errorf("unselected card must not carry the marker:\n%s", got)
	}

	path := filepath.Join("testdata", "multi_cursor.golden")
	if *updateGolden {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update-golden): %v", err)
	}
	if got != string(want) {
		t.Errorf("cursor render mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
