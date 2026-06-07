package fleet

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// TestParseErrorReason covers the inline TOML extractor used by the Recent
// lane. The extractor is intentionally permissive: malformed input, a missing
// _error node, or a non-string reason all collapse to "" so the display
// degrades to the bare status glyph rather than surfacing parse errors.
func TestParseErrorReason(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "valid budget exhausted",
			in: `
[nodes._error]
reason = "budget exhausted"
node = "coder"
`,
			want: "budget exhausted",
		},
		{
			name: "valid generic failure",
			in: `
[nodes._error]
reason = "max master-review cycles exhausted"
`,
			want: "max master-review cycles exhausted",
		},
		{
			name: "no _error node",
			in: `
[nodes.coder]
result = "ok"
`,
			want: "",
		},
		{
			name: "empty input",
			in:   "",
			want: "",
		},
		{
			name: "malformed toml",
			in:   "not [valid] {{toml",
			want: "",
		},
		{
			name: "_error present but no reason key",
			in: `
[nodes._error]
detail = "..."
`,
			want: "",
		},
		{
			name: "_error reason is non-string (number)",
			in: `
[nodes._error]
reason = 42
`,
			want: "",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := parseErrorReason(tc.in); got != tc.want {
				t.Errorf("parseErrorReason() = %q, want %q", got, tc.want)
			}
		})
	}
}

// seedFailedRun inserts a constellation_run row in state='failed' with the
// given dag_state_toml. Helper for the failureReasonFor tests below.
func seedFailedRun(t *testing.T, db *sql.DB, runID, nebulaID, dagState string, completedOffset time.Duration) {
	t.Helper()
	now := time.Now()
	completedAt := now.Add(completedOffset).Unix()
	_, err := db.Exec(`INSERT INTO constellation_runs
		(id, nebula_id, constellation_name, state, current_node, step_index, step_count,
		 created_at, updated_at, completed_at, dag_state_toml)
		VALUES (?, ?, 'coder-reviewer', 'failed', '', 0, 0, ?, ?, ?, ?)`,
		runID, nebulaID, now.Unix(), now.Unix(), completedAt, dagState)
	if err != nil {
		t.Fatalf("seed failed run: %v", err)
	}
}

// TestFailureReasonFor_NoFailedRun confirms the lookup degrades gracefully
// when a nebula has no failed run (e.g. it terminated cleanly or is still
// in-flight).
func TestFailureReasonFor_NoFailedRun(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	store := NewStore(db)
	const repo = "/src/papapumpkin/quasar"
	seedRepo(t, db, repo, "quasar")
	seedNebula(t, db, "neb-1", repo, "fine", "merged")

	got, err := store.failureReasonFor(context.Background(), "neb-1")
	if err != nil {
		t.Fatalf("failureReasonFor: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// TestFailureReasonFor_FailedRunWithReason confirms the lookup extracts the
// _error.reason from the run's dag_state_toml.
func TestFailureReasonFor_FailedRunWithReason(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	store := NewStore(db)
	const repo = "/src/papapumpkin/quasar"
	seedRepo(t, db, repo, "quasar")
	seedNebula(t, db, "neb-1", repo, "broke", "failed")
	seedFailedRun(t, db, "run-1", "neb-1", `
[nodes._error]
reason = "budget exhausted"
`, -1*time.Minute)

	got, err := store.failureReasonFor(context.Background(), "neb-1")
	if err != nil {
		t.Fatalf("failureReasonFor: %v", err)
	}
	if got != "budget exhausted" {
		t.Errorf("got %q, want %q", got, "budget exhausted")
	}
}

// TestFailureReasonFor_PicksLatestFailedRun confirms that when multiple failed
// runs exist for the same nebula, the most-recently-completed one wins. This
// matters when a coder-reviewer constellation retries an inner cycle that
// itself was failed: the latest result is the user-meaningful one.
func TestFailureReasonFor_PicksLatestFailedRun(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	store := NewStore(db)
	const repo = "/src/papapumpkin/quasar"
	seedRepo(t, db, repo, "quasar")
	seedNebula(t, db, "neb-1", repo, "tries", "failed")

	// Older failed run with a different reason.
	seedFailedRun(t, db, "run-old", "neb-1", `
[nodes._error]
reason = "max master-review cycles exhausted"
`, -10*time.Minute)
	// Newer failed run — should win.
	seedFailedRun(t, db, "run-new", "neb-1", `
[nodes._error]
reason = "budget exhausted"
`, -1*time.Minute)

	got, err := store.failureReasonFor(context.Background(), "neb-1")
	if err != nil {
		t.Fatalf("failureReasonFor: %v", err)
	}
	if got != "budget exhausted" {
		t.Errorf("got %q, want latest %q", got, "budget exhausted")
	}
}

// TestFailureReasonFor_NullDagState confirms a failed run with no recorded
// state (e.g. the engine crashed before SaveProgress) yields "" rather than
// an error.
func TestFailureReasonFor_NullDagState(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	store := NewStore(db)
	const repo = "/src/papapumpkin/quasar"
	seedRepo(t, db, repo, "quasar")
	seedNebula(t, db, "neb-1", repo, "broke", "failed")
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO constellation_runs
		(id, nebula_id, constellation_name, state, current_node, step_index, step_count,
		 created_at, updated_at, completed_at)
		VALUES ('run-1', 'neb-1', 'coder-reviewer', 'failed', '', 0, 0, ?, ?, ?)`,
		now, now, now); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := store.failureReasonFor(context.Background(), "neb-1")
	if err != nil {
		t.Fatalf("failureReasonFor: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// TestRecentLanePopulatesFailureReason end-to-end checks that the Recent lane
// carries the failure reason for failed nebulas (and only failed ones).
func TestRecentLanePopulatesFailureReason(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	store := NewStore(db)
	const repo = "/src/papapumpkin/quasar"
	seedRepo(t, db, repo, "quasar")
	seedNebula(t, db, "neb-ok", repo, "shipped", "merged")
	seedNebula(t, db, "neb-fail", repo, "broke", "failed")
	seedFailedRun(t, db, "run-1", "neb-fail", `
[nodes._error]
reason = "budget exhausted"
`, -1*time.Minute)

	f, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(f.Repos) != 1 {
		t.Fatalf("repos = %d", len(f.Repos))
	}
	recent := f.Repos[0].Recent
	if len(recent) != 2 {
		t.Fatalf("recent = %d cards, want 2", len(recent))
	}
	// Cards come back newest first. Find each by ID rather than position so
	// the assertion does not depend on tie-breaking in updated_at.
	for _, c := range recent {
		switch c.ID {
		case "neb-fail":
			if c.FailureReason != "budget exhausted" {
				t.Errorf("neb-fail.FailureReason = %q, want %q", c.FailureReason, "budget exhausted")
			}
		case "neb-ok":
			if c.FailureReason != "" {
				t.Errorf("neb-ok.FailureReason = %q, want empty", c.FailureReason)
			}
		default:
			t.Errorf("unexpected card ID %q", c.ID)
		}
	}
}

// TestFailureSuffix covers the render-side helper used by the Recent lane to
// turn FailureReason into a parenthetical the card line can concatenate.
func TestFailureSuffix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   NebulaCard
		want string
	}{
		{name: "empty reason", in: NebulaCard{}, want: ""},
		{name: "budget exhausted", in: NebulaCard{FailureReason: "budget exhausted"}, want: " (budget exhausted)"},
		{name: "cycles exhausted", in: NebulaCard{FailureReason: "max master-review cycles exhausted"}, want: " (max master-review cycles exhausted)"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := failureSuffix(tc.in); got != tc.want {
				t.Errorf("failureSuffix(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestRenderFleetIncludesFailureSuffix exercises the rendered Recent lane to
// verify the failure suffix actually reaches the screen. Asserts substrings
// rather than a golden block so this test does not need to track every
// formatting tweak in unrelated lanes.
func TestRenderFleetIncludesFailureSuffix(t *testing.T) {
	t.Parallel()
	f := Fleet{Repos: []RepoLane{{
		Path:        "/src/papapumpkin/quasar",
		DisplayName: "papapumpkin/quasar",
		Recent: []NebulaCard{
			{ID: "neb-fail", Title: "broke", Status: "failed", FailureReason: "budget exhausted"},
			{ID: "neb-ok", Title: "shipped", Status: "merged"},
		},
	}}}
	out := RenderFleet(f, 110, Selection{})
	if !strings.Contains(out, "broke (budget exhausted)") {
		t.Errorf("rendered output missing failure suffix; got:\n%s", out)
	}
	if strings.Contains(out, "shipped (budget exhausted)") {
		t.Errorf("rendered output applied failure suffix to non-failed card:\n%s", out)
	}
}
