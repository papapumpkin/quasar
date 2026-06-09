package constellations

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// fakeFirer records the Fire calls it received so tests can assert which
// triggers were consumed. It can be configured to fail a specific call.
type fakeFirer struct {
	calls   []fakeFireCall
	failOn  string // constellation name to fail; empty = always succeed
	counter atomic.Int64
}

type fakeFireCall struct {
	RepoPath          string
	ConstellationName string
	NebulaID          string
}

func (f *fakeFirer) Fire(_ context.Context, repoPath, constellation, nebulaID string) (string, error) {
	f.counter.Add(1)
	f.calls = append(f.calls, fakeFireCall{
		RepoPath:          repoPath,
		ConstellationName: constellation,
		NebulaID:          nebulaID,
	})
	if f.failOn != "" && constellation == f.failOn {
		return "", errors.New("synthetic fire failure")
	}
	return "run-" + nebulaID, nil
}

// newSupervisorTestDB opens a real fabric database (running all migrations) so
// the trigger_queue schema matches production exactly. Each test gets its
// own temp database to avoid cross-test contention.
func newSupervisorTestDB(t *testing.T) *sql.DB {
	t.Helper()
	fab, err := fabric.NewSQLiteFabric(context.Background(), filepath.Join(t.TempDir(), "fabric.db"))
	if err != nil {
		t.Fatalf("fabric: %v", err)
	}
	t.Cleanup(func() { _ = fab.Close() })
	return fab.DB()
}

// seedTrigger inserts a pending trigger_queue row.
func seedTrigger(t *testing.T, db *sql.DB, nebulaID, constellation, repoPath string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO trigger_queue (nebula_id, constellation_name, state, created_at, repo_path)
		VALUES (?, ?, 'pending', strftime('%s','now'), ?)`,
		nebulaID, constellation, repoPath)
	if err != nil {
		t.Fatalf("seed trigger: %v", err)
	}
}

// countByState returns the number of trigger_queue rows in the given state.
func countByState(t *testing.T, db *sql.DB, state string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM trigger_queue WHERE state = ?`, state).Scan(&n); err != nil {
		t.Fatalf("count by state %q: %v", state, err)
	}
	return n
}

func TestSupervisorTickFiresEachPendingTrigger(t *testing.T) {
	t.Parallel()
	db := newSupervisorTestDB(t)
	seedTrigger(t, db, "neb-a", "architect", "/repos/a")
	seedTrigger(t, db, "neb-b", "architect", "/repos/b")
	seedTrigger(t, db, "neb-c", "master-review", "/repos/a")

	firer := &fakeFirer{}
	sup := &Supervisor{DB: db, Firer: firer}

	fired, err := sup.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if fired != 3 {
		t.Errorf("fired = %d, want 3", fired)
	}
	if len(firer.calls) != 3 {
		t.Errorf("Fire calls = %d, want 3", len(firer.calls))
	}
	if got := countByState(t, db, "consumed"); got != 3 {
		t.Errorf("consumed rows = %d, want 3", got)
	}
	if got := countByState(t, db, "pending"); got != 0 {
		t.Errorf("pending rows = %d, want 0", got)
	}
}

func TestSupervisorTickRespectsBatchLimit(t *testing.T) {
	t.Parallel()
	db := newSupervisorTestDB(t)
	for i := 0; i < 5; i++ {
		seedTrigger(t, db, "neb", "architect", "/r")
	}

	firer := &fakeFirer{}
	sup := &Supervisor{DB: db, Firer: firer, BatchLimit: 2}

	fired, err := sup.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if fired != 2 {
		t.Errorf("fired = %d, want 2 (BatchLimit)", fired)
	}
	if got := countByState(t, db, "pending"); got != 3 {
		t.Errorf("pending after Tick = %d, want 3 (BatchLimit=2)", got)
	}
}

func TestSupervisorTickMarksConsumedEvenOnFireFailure(t *testing.T) {
	t.Parallel()
	// Marking-consumed-on-failure is deliberate: an unhealthy Fire that loops
	// forever on the same row would block the rest of the queue. Operators
	// see the log line and can re-approve from the fleet view once the cause
	// is fixed.
	db := newSupervisorTestDB(t)
	seedTrigger(t, db, "neb-bad", "broken-constellation", "/r")
	seedTrigger(t, db, "neb-good", "architect", "/r")

	firer := &fakeFirer{failOn: "broken-constellation"}
	var logbuf bytes.Buffer
	sup := &Supervisor{DB: db, Firer: firer, Logger: &logbuf}

	fired, err := sup.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if fired != 1 {
		t.Errorf("fired = %d, want 1 (only the good one)", fired)
	}
	// Both rows must be consumed — the bad one too, to prevent retry storms.
	if got := countByState(t, db, "consumed"); got != 2 {
		t.Errorf("consumed = %d, want 2 (both rows)", got)
	}
	// The failure must be logged so operators see it.
	if !strings.Contains(logbuf.String(), "broken-constellation") {
		t.Errorf("expected log mentioning the failed constellation, got: %s", logbuf.String())
	}
}

func TestSupervisorTickIsIdempotentOnEmptyQueue(t *testing.T) {
	t.Parallel()
	db := newSupervisorTestDB(t)
	firer := &fakeFirer{}
	sup := &Supervisor{DB: db, Firer: firer}

	fired, err := sup.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick on empty queue: %v", err)
	}
	if fired != 0 {
		t.Errorf("fired = %d on empty queue, want 0", fired)
	}
	if len(firer.calls) != 0 {
		t.Errorf("Fire called %d times on empty queue, want 0", len(firer.calls))
	}
}

func TestSupervisorTickDoubleClaimSafe(t *testing.T) {
	t.Parallel()
	// Two ticks in quick succession on the same DB must not double-fire a
	// trigger. The claim UPDATE is the atomic gate; the second Tick's claim
	// returns 0 rows affected and the row is skipped.
	db := newSupervisorTestDB(t)
	seedTrigger(t, db, "neb-once", "architect", "/r")

	firer := &fakeFirer{}
	sup := &Supervisor{DB: db, Firer: firer}

	if _, err := sup.Tick(context.Background()); err != nil {
		t.Fatalf("Tick 1: %v", err)
	}
	if _, err := sup.Tick(context.Background()); err != nil {
		t.Fatalf("Tick 2: %v", err)
	}
	if firer.counter.Load() != 1 {
		t.Errorf("Fire called %d times across two ticks, want 1", firer.counter.Load())
	}
}

func TestSupervisorTickForwardsRepoPathToFirer(t *testing.T) {
	t.Parallel()
	// The Firer's repoPath argument is how multi-repo wiring routes a
	// trigger to the correct per-repo Runtime; the supervisor must read
	// repo_path off the row and pass it through.
	db := newSupervisorTestDB(t)
	seedTrigger(t, db, "neb-a", "architect", "/srv/repos/papapumpkin/quasar")
	seedTrigger(t, db, "neb-b", "architect", "/srv/repos/papapumpkin/relativity")

	firer := &fakeFirer{}
	sup := &Supervisor{DB: db, Firer: firer}
	if _, err := sup.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	gotPaths := map[string]string{}
	for _, c := range firer.calls {
		gotPaths[c.NebulaID] = c.RepoPath
	}
	if gotPaths["neb-a"] != "/srv/repos/papapumpkin/quasar" {
		t.Errorf("neb-a repoPath = %q, want %q", gotPaths["neb-a"], "/srv/repos/papapumpkin/quasar")
	}
	if gotPaths["neb-b"] != "/srv/repos/papapumpkin/relativity" {
		t.Errorf("neb-b repoPath = %q, want %q", gotPaths["neb-b"], "/srv/repos/papapumpkin/relativity")
	}
}

func TestSupervisorTickProcessesOldestFirst(t *testing.T) {
	t.Parallel()
	db := newSupervisorTestDB(t)
	// Seed with explicit created_at so ordering is deterministic regardless
	// of insert speed.
	for _, row := range []struct {
		nebID   string
		created int64
	}{
		{"neb-newest", 300},
		{"neb-oldest", 100},
		{"neb-middle", 200},
	} {
		_, err := db.Exec(`INSERT INTO trigger_queue
			(nebula_id, constellation_name, state, created_at, repo_path)
			VALUES (?, 'architect', 'pending', ?, '/r')`, row.nebID, row.created)
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	firer := &fakeFirer{}
	sup := &Supervisor{DB: db, Firer: firer, BatchLimit: 2}
	if _, err := sup.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(firer.calls) != 2 {
		t.Fatalf("fired %d, want 2", len(firer.calls))
	}
	if firer.calls[0].NebulaID != "neb-oldest" {
		t.Errorf("first fired = %q, want oldest (neb-oldest)", firer.calls[0].NebulaID)
	}
	if firer.calls[1].NebulaID != "neb-middle" {
		t.Errorf("second fired = %q, want second-oldest (neb-middle)", firer.calls[1].NebulaID)
	}
}
