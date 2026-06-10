package constellations

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/fabric"
)

// fakeStepper records every Step call so tests can assert which runs got
// advanced and in what order. The behavior per run can be configured (return
// terminal, error, or stay running) so a single test can exercise multiple
// dispositions in one Tick.
type fakeStepper struct {
	mu      sync.Mutex
	calls   []fakeStepCall
	disp    map[string]stepDisposition // keyed by runID
	counter atomic.Int64
}

type fakeStepCall struct {
	RepoPath string
	RunID    string
}

type stepDisposition struct {
	state string
	err   error
}

func (f *fakeStepper) Step(_ context.Context, repoPath, runID string) (string, error) {
	f.counter.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeStepCall{RepoPath: repoPath, RunID: runID})
	if d, ok := f.disp[runID]; ok {
		return d.state, d.err
	}
	return StateRunning, nil
}

// newStepDriverTestDB opens a real fabric DB (running all migrations) so the
// constellation_runs schema matches production exactly.
func newStepDriverTestDB(t *testing.T) (*sql.DB, *fabric.SQLiteFabric) {
	t.Helper()
	fab, err := fabric.NewSQLiteFabric(context.Background(), filepath.Join(t.TempDir(), "fabric.db"))
	if err != nil {
		t.Fatalf("fabric: %v", err)
	}
	t.Cleanup(func() { _ = fab.Close() })
	return fab.DB(), fab
}

// seedRunningRun inserts a constellation_runs row in state='running' for
// the given nebula. A nebula row is inserted first so the FK constraint
// holds.
func seedRunningRun(t *testing.T, db *sql.DB, runID, nebulaID, repoPath string, heartbeat int64) {
	t.Helper()
	// Nebula row (FK target). Use a unique name per call to avoid UNIQUE
	// collisions on (name) if the schema enforces one.
	_, err := db.Exec(`INSERT OR IGNORE INTO nebulas (id, name, status, created_at, updated_at)
		VALUES (?, ?, 'running', strftime('%s','now'), strftime('%s','now'))`,
		nebulaID, nebulaID+"-name")
	if err != nil {
		t.Fatalf("seed nebula: %v", err)
	}
	now := time.Now().Unix()
	_, err = db.Exec(`INSERT INTO constellation_runs
		(id, nebula_id, constellation_name, state, current_node, step_index, cycle,
		 created_at, updated_at, heartbeat_at, repo_path)
		VALUES (?, ?, 'test-con', 'running', 'entry', 0, 0, ?, ?, ?, ?)`,
		runID, nebulaID, now, now, heartbeat, repoPath)
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}
}

func TestStepDriverTickAdvancesEachRunningRun(t *testing.T) {
	t.Parallel()
	db, _ := newStepDriverTestDB(t)
	seedRunningRun(t, db, "run-a", "neb-a", "/r1", 100)
	seedRunningRun(t, db, "run-b", "neb-b", "/r2", 200)

	st := &fakeStepper{disp: map[string]stepDisposition{}}
	d := &StepDriver{DB: db, Stepper: st}

	advanced, err := d.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if advanced != 2 {
		t.Errorf("advanced = %d, want 2", advanced)
	}
	if got := st.counter.Load(); got != 2 {
		t.Errorf("Step called %d times, want 2", got)
	}
}

func TestStepDriverTickRoutesByRepoPath(t *testing.T) {
	t.Parallel()
	// The Stepper's repoPath argument is how multi-repo wiring picks the
	// right Runtime; the driver must read repo_path off the row and pass
	// it through.
	db, _ := newStepDriverTestDB(t)
	seedRunningRun(t, db, "run-a", "neb-a", "/srv/repos/papapumpkin/quasar", 100)
	seedRunningRun(t, db, "run-b", "neb-b", "/srv/repos/papapumpkin/relativity", 100)

	st := &fakeStepper{disp: map[string]stepDisposition{}}
	d := &StepDriver{DB: db, Stepper: st}
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	gotPaths := map[string]string{}
	for _, c := range st.calls {
		gotPaths[c.RunID] = c.RepoPath
	}
	if gotPaths["run-a"] != "/srv/repos/papapumpkin/quasar" {
		t.Errorf("run-a repoPath = %q", gotPaths["run-a"])
	}
	if gotPaths["run-b"] != "/srv/repos/papapumpkin/relativity" {
		t.Errorf("run-b repoPath = %q", gotPaths["run-b"])
	}
}

func TestStepDriverTickRespectsBatchLimit(t *testing.T) {
	t.Parallel()
	db, _ := newStepDriverTestDB(t)
	for i := 0; i < 5; i++ {
		seedRunningRun(t, db, fmt.Sprintf("run-%d", i), fmt.Sprintf("neb-%d", i), "/r", int64(100+i))
	}
	st := &fakeStepper{}
	d := &StepDriver{DB: db, Stepper: st, BatchLimit: 2}
	advanced, err := d.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if advanced != 2 {
		t.Errorf("advanced = %d, want 2 (BatchLimit)", advanced)
	}
}

func TestStepDriverTickOldestHeartbeatFirst(t *testing.T) {
	t.Parallel()
	// A stalled run (older heartbeat) must be advanced before fresh runs.
	db, _ := newStepDriverTestDB(t)
	seedRunningRun(t, db, "run-fresh", "neb-1", "/r", 500)
	seedRunningRun(t, db, "run-stalled", "neb-2", "/r", 100)
	seedRunningRun(t, db, "run-middle", "neb-3", "/r", 300)

	st := &fakeStepper{}
	d := &StepDriver{DB: db, Stepper: st, BatchLimit: 2}
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if len(st.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(st.calls))
	}
	if st.calls[0].RunID != "run-stalled" {
		t.Errorf("first stepped = %q, want oldest heartbeat (run-stalled)", st.calls[0].RunID)
	}
	if st.calls[1].RunID != "run-middle" {
		t.Errorf("second stepped = %q, want second-oldest (run-middle)", st.calls[1].RunID)
	}
}

func TestStepDriverTickContinuesPastErrors(t *testing.T) {
	t.Parallel()
	// One run's Step failing must not block other runs from advancing.
	db, _ := newStepDriverTestDB(t)
	seedRunningRun(t, db, "run-bad", "neb-1", "/r", 100)
	seedRunningRun(t, db, "run-good", "neb-2", "/r", 200)

	var logbuf bytes.Buffer
	st := &fakeStepper{disp: map[string]stepDisposition{
		"run-bad": {err: errors.New("synthetic step failure")},
	}}
	d := &StepDriver{DB: db, Stepper: st, Logger: &logbuf}
	advanced, err := d.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if advanced != 1 {
		t.Errorf("advanced = %d, want 1 (only the good one)", advanced)
	}
	if !strings.Contains(logbuf.String(), "run-bad") {
		t.Errorf("expected log mentioning the failed run, got: %s", logbuf.String())
	}
}

func TestStepDriverTickSkipsErrTerminalSilently(t *testing.T) {
	t.Parallel()
	// A run that's already terminal returns ErrTerminal — that's a benign
	// race with the run's own transition or an external action and should
	// neither log nor count as advanced.
	db, _ := newStepDriverTestDB(t)
	seedRunningRun(t, db, "run-done", "neb-1", "/r", 100)

	var logbuf bytes.Buffer
	st := &fakeStepper{disp: map[string]stepDisposition{
		"run-done": {err: ErrTerminal},
	}}
	d := &StepDriver{DB: db, Stepper: st, Logger: &logbuf}
	advanced, err := d.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if advanced != 0 {
		t.Errorf("advanced = %d, want 0 (terminal race shouldn't count)", advanced)
	}
	if strings.Contains(logbuf.String(), "ErrTerminal") || strings.Contains(logbuf.String(), "terminal") {
		t.Errorf("unexpected log output for ErrTerminal: %s", logbuf.String())
	}
}

func TestStepDriverTickIgnoresNonRunningRows(t *testing.T) {
	t.Parallel()
	db, _ := newStepDriverTestDB(t)
	// Seed a 'done' row directly; selectRunning should skip it.
	_, err := db.Exec(`INSERT INTO nebulas (id, name, status, created_at, updated_at)
		VALUES ('neb-done','neb-done','done', strftime('%s','now'), strftime('%s','now'))`)
	if err != nil {
		t.Fatalf("seed nebula: %v", err)
	}
	_, err = db.Exec(`INSERT INTO constellation_runs
		(id, nebula_id, constellation_name, state, current_node, step_index, cycle,
		 created_at, updated_at, heartbeat_at, repo_path)
		VALUES ('run-done','neb-done','t','done','x',0,0,1,1,1,'/r')`)
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}

	st := &fakeStepper{}
	d := &StepDriver{DB: db, Stepper: st}
	advanced, err := d.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if advanced != 0 || st.counter.Load() != 0 {
		t.Errorf("driver advanced terminal run; advanced=%d, calls=%d", advanced, st.counter.Load())
	}
}

func TestStepDriverTickIgnoresChildRuns(t *testing.T) {
	t.Parallel()
	// A child run (parent_run_id set) is driven to terminal synchronously by
	// its parent's Step (dispatchConstellation). The driver must NOT claim it,
	// or a node whose side effect already ran could be re-stepped.
	db, _ := newStepDriverTestDB(t)
	seedRunningRun(t, db, "run-parent", "neb-parent", "/r", 100)
	// A running child of run-parent, with a fresher (smaller) heartbeat so it
	// would sort first and be claimed if the filter were absent.
	_, err := db.Exec(`INSERT OR IGNORE INTO nebulas (id, name, status, created_at, updated_at)
		VALUES ('neb-child','neb-child-name','running', strftime('%s','now'), strftime('%s','now'))`)
	if err != nil {
		t.Fatalf("seed child nebula: %v", err)
	}
	_, err = db.Exec(`INSERT INTO constellation_runs
		(id, nebula_id, constellation_name, state, current_node, step_index, cycle,
		 created_at, updated_at, heartbeat_at, repo_path, parent_run_id)
		VALUES ('run-child','neb-child','t','running','x',0,0,1,1,1,'/r','run-parent')`)
	if err != nil {
		t.Fatalf("seed child run: %v", err)
	}

	st := &fakeStepper{disp: map[string]stepDisposition{}}
	d := &StepDriver{DB: db, Stepper: st}
	advanced, err := d.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if advanced != 1 {
		t.Errorf("advanced = %d, want 1 (parent only)", advanced)
	}
	for _, c := range st.calls {
		if c.RunID == "run-child" {
			t.Errorf("driver claimed child run %q; children are parent-driven", c.RunID)
		}
	}
}
