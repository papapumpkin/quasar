package fleet

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// newTestDB opens a fresh fabric database (running all migrations) in a temp
// dir and returns its *sql.DB plus a cleanup-registered close.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	fab, err := fabric.NewSQLiteFabric(context.Background(), filepath.Join(t.TempDir(), "fabric.db"))
	if err != nil {
		t.Fatalf("open fabric: %v", err)
	}
	t.Cleanup(func() { _ = fab.Close() })
	return fab.DB()
}

// seedRepo inserts an active repo row.
func seedRepo(t *testing.T, db *sql.DB, path, name string) {
	t.Helper()
	now := time.Now().Unix()
	_, err := db.Exec(`INSERT INTO repos (path, name, status, added_at, updated_at, last_seen_at)
		VALUES (?, ?, 'active', ?, ?, ?)`, path, name, now, now, now)
	if err != nil {
		t.Fatalf("seed repo: %v", err)
	}
}

// seedNebula inserts a nebula row with the given status.
func seedNebula(t *testing.T, db *sql.DB, id, repo, name, status string) {
	t.Helper()
	now := time.Now().Unix()
	_, err := db.Exec(`INSERT INTO nebulas (id, repo_path, name, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, id, repo, name, status, now, now)
	if err != nil {
		t.Fatalf("seed nebula: %v", err)
	}
}

func TestStoreLoad(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	const repo = "/src/papapumpkin/quasar"
	seedRepo(t, db, repo, "quasar")
	seedNebula(t, db, "neb-await", repo, "add retry", "awaiting_approval")
	seedNebula(t, db, "neb-done", repo, "shipped thing", "merged")
	seedNebula(t, db, "neb-draft", repo, "draft thing", "draft") // neither lane

	// One in-flight run.
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO constellation_runs
		(id, nebula_id, constellation_name, state, current_node, step_index, step_count, created_at, updated_at)
		VALUES ('run-1', 'neb-draft', 'coder-reviewer', 'running', 'reviewer', 3, 5, ?, ?)`, now, now); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	store := NewStore(db)
	f, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(f.Repos) != 1 {
		t.Fatalf("repos = %d, want 1", len(f.Repos))
	}
	lane := f.Repos[0]
	if lane.DisplayName != "papapumpkin/quasar" {
		t.Errorf("DisplayName = %q", lane.DisplayName)
	}
	if len(lane.AwaitingApproval) != 1 || lane.AwaitingApproval[0].ID != "neb-await" {
		t.Errorf("awaiting = %+v", lane.AwaitingApproval)
	}
	if len(lane.Recent) != 1 || lane.Recent[0].Status != "merged" {
		t.Errorf("recent = %+v", lane.Recent)
	}
	if len(lane.InFlight) != 1 || lane.InFlight[0].CurrentNode != "reviewer" || lane.InFlight[0].StepIndex != 3 {
		t.Errorf("inflight = %+v", lane.InFlight)
	}
}

func TestStoreApprove(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	const repo = "/src/quasar"
	seedRepo(t, db, repo, "quasar")
	seedNebula(t, db, "neb-1", repo, "thing", "awaiting_approval")

	store := NewStore(db)
	if err := store.Approve(ctx, "neb-1"); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	var status string
	if err := db.QueryRow("SELECT status FROM nebulas WHERE id='neb-1'").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "approved" {
		t.Errorf("status = %q, want approved", status)
	}
	var n, name string
	if err := db.QueryRow("SELECT nebula_id, constellation_name FROM trigger_queue WHERE state='pending'").Scan(&n, &name); err != nil {
		t.Fatalf("expected a pending trigger: %v", err)
	}
	if n != "neb-1" || name != "architect" {
		t.Errorf("trigger = (%q,%q), want (neb-1, architect)", n, name)
	}
}

func TestStoreRejectAndRunState(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	const repo = "/src/quasar"
	seedRepo(t, db, repo, "quasar")
	seedNebula(t, db, "neb-1", repo, "thing", "awaiting_approval")
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO constellation_runs (id, nebula_id, state, created_at, updated_at)
		VALUES ('run-1', 'neb-1', 'running', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO star_invocations (run_id, seq, node, star_name, state)
		VALUES ('run-1', 1, 'coder', 'implement', 'done')`); err != nil {
		t.Fatal(err)
	}

	store := NewStore(db)
	if err := store.Reject(ctx, "neb-1", "not now"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	var status string
	if err := db.QueryRow("SELECT status FROM nebulas WHERE id='neb-1'").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "rejected" {
		t.Errorf("status = %q, want rejected", status)
	}

	if err := store.SetRunState(ctx, "run-1", "killed"); err != nil {
		t.Fatalf("SetRunState: %v", err)
	}
	var runState string
	if err := db.QueryRow("SELECT state FROM constellation_runs WHERE id='run-1'").Scan(&runState); err != nil {
		t.Fatal(err)
	}
	if runState != "killed" {
		t.Errorf("run state = %q, want killed", runState)
	}

	trace, err := store.Trace(ctx, "run-1")
	if err != nil {
		t.Fatalf("Trace: %v", err)
	}
	if len(trace) != 1 || trace[0].StarName != "implement" {
		t.Errorf("trace = %+v", trace)
	}
}

func TestStoreLoadMissingInFlightTable(t *testing.T) {
	// Sanity: in-flight query joins constellation_runs; an empty table yields an
	// empty lane rather than an error.
	t.Parallel()
	db := newTestDB(t)
	seedRepo(t, db, "/src/quasar", "quasar")
	f, err := NewStore(db).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(f.Repos[0].InFlight) != 0 {
		t.Errorf("expected empty in-flight lane")
	}
}

func TestParseFilter(t *testing.T) {
	t.Parallel()
	f := ParseFilter("repo:quasar state:running since:2h some text")
	if f.Repo != "quasar" || f.State != "running" || f.Since != 2*time.Hour || f.Text != "some text" {
		t.Errorf("parsed = %+v", f)
	}
	if !ParseFilter("").Empty() {
		t.Error("empty string should parse to empty filter")
	}
}

func TestFilterApply(t *testing.T) {
	t.Parallel()
	in := Fleet{Repos: []RepoLane{
		{DisplayName: "org/a", AwaitingApproval: []NebulaCard{{Title: "fix flaky", Status: "awaiting_approval"}}},
		{DisplayName: "org/b", Recent: []NebulaCard{{Title: "other", Status: "merged"}}},
	}}
	out := ParseFilter("repo:a").Apply(in)
	if len(out.Repos) != 1 || out.Repos[0].DisplayName != "org/a" {
		t.Errorf("repo filter = %+v", out.Repos)
	}
	out = ParseFilter("flaky").Apply(in)
	if len(out.Repos) != 1 || len(out.Repos[0].AwaitingApproval) != 1 {
		t.Errorf("text filter = %+v", out.Repos)
	}
}

func TestStateRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sub", "tui-state.json")
	want := UIState{FoldedRepos: []string{"org/old"}, ActiveLane: "recent", Filter: "repo:x"}
	if err := SaveState(path, want); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got.ActiveLane != want.ActiveLane || got.Filter != want.Filter || !got.IsFolded("org/old") {
		t.Errorf("round-trip = %+v", got)
	}

	// Missing file is not an error.
	missing, err := LoadState(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || !missing.IsFolded("") == false {
		t.Errorf("missing file should yield zero state, got %+v err=%v", missing, err)
	}
}

func TestToggleFold(t *testing.T) {
	t.Parallel()
	st := UIState{}
	st = st.ToggleFold("org/a")
	if !st.IsFolded("org/a") {
		t.Fatal("expected folded")
	}
	st = st.ToggleFold("org/a")
	if st.IsFolded("org/a") {
		t.Fatal("expected unfolded")
	}
}
