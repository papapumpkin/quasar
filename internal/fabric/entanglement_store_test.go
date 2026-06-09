package fabric

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// newEntStore spins up an on-disk SQLite fabric (all migrations applied) and
// returns a lifecycle store.
func newEntStore(t *testing.T) *EntanglementStore {
	t.Helper()
	dir := t.TempDir()
	fab, err := NewSQLiteFabric(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteFabric: %v", err)
	}
	t.Cleanup(func() { _ = fab.db.Close() })
	return NewEntanglementStore(fab.DB())
}

// statusOf returns the current status of the named symbol's first row.
func statusOf(t *testing.T, s *EntanglementStore, name string) string {
	t.Helper()
	var status string
	err := s.db.QueryRowContext(context.Background(),
		`SELECT status FROM entanglements WHERE name = ? ORDER BY id LIMIT 1`, name).Scan(&status)
	if err != nil {
		t.Fatalf("statusOf %q: %v", name, err)
	}
	return status
}

func TestEntanglementStoreLifecycle(t *testing.T) {
	ctx := context.Background()

	t.Run("declare through fulfill stamps each transition", func(t *testing.T) {
		s := newEntStore(t)
		if err := s.Declare(ctx, Entanglement{
			Producer: "phase-a", RunID: "run-1", PhaseID: "phase-a",
			Kind: KindFunction, Name: "Sensor.Poll", Signature: "func Poll() error",
		}); err != nil {
			t.Fatalf("Declare: %v", err)
		}
		if got := statusOf(t, s, "Sensor.Poll"); got != StatusDeclared {
			t.Fatalf("after Declare status = %q, want declared", got)
		}

		// Note: the lifecycle no longer transitions through 'claimed' in
		// production — Claim was removed in the 2026-06-08 audit because
		// the runtime never invoked it at coder pickup. MarkInFlight's
		// WHERE clause still accepts StatusClaimed for forward compatibility,
		// but the timestamps below assert the actual production path:
		// declared → in_flight, skipping claimed_at.

		// Two MarkInFlight calls: the second must refresh the signature.
		if err := s.MarkInFlight(ctx, "run-1", "Sensor.Poll", "func Poll(ctx context.Context) error"); err != nil {
			t.Fatalf("MarkInFlight 1: %v", err)
		}
		if err := s.MarkInFlight(ctx, "run-1", "Sensor.Poll", "func Poll(ctx context.Context) (Ticket, error)"); err != nil {
			t.Fatalf("MarkInFlight 2: %v", err)
		}

		active, err := s.Active(ctx, "Sensor.Poll")
		if err != nil {
			t.Fatalf("Active: %v", err)
		}
		if len(active) != 1 {
			t.Fatalf("Active returned %d rows, want 1", len(active))
		}
		e := active[0]
		if e.Status != StatusInFlight {
			t.Fatalf("status = %q, want in_flight", e.Status)
		}
		if e.CurrentSignature != "func Poll(ctx context.Context) (Ticket, error)" {
			t.Fatalf("current_signature = %q, want the latest draft", e.CurrentSignature)
		}
		if e.DeclaredAt == 0 || e.InFlightAt == 0 {
			t.Fatalf("expected declared/in_flight timestamps set, got %+v", e)
		}

		if err := s.Fulfill(ctx, "run-1"); err != nil {
			t.Fatalf("Fulfill: %v", err)
		}
		if got := statusOf(t, s, "Sensor.Poll"); got != StatusFulfilled {
			t.Fatalf("after Fulfill status = %q, want fulfilled", got)
		}
		// Fulfilled rows are terminal — Active must exclude them.
		if active, _ := s.Active(ctx, "Sensor.Poll"); len(active) != 0 {
			t.Fatalf("Active after Fulfill returned %d rows, want 0", len(active))
		}
	})

	t.Run("declare then withdraw on terminal failure", func(t *testing.T) {
		s := newEntStore(t)
		mustDeclare(t, s, "run-2", "phase-b", "Budget.CheckBefore")
		// Withdraw matches StatusDeclared directly — the old call to Claim
		// in between was unnecessary since the lifecycle skips 'claimed' in
		// production (see the audit note in entanglement_store.go).
		if err := s.Withdraw(ctx, "run-2"); err != nil {
			t.Fatalf("Withdraw: %v", err)
		}
		if got := statusOf(t, s, "Budget.CheckBefore"); got != StatusWithdrawn {
			t.Fatalf("status = %q, want withdrawn", got)
		}
	})

	t.Run("deprecate transitions in-flight to deprecated", func(t *testing.T) {
		s := newEntStore(t)
		mustDeclare(t, s, "run-3", "phase-c", "OldSymbol")
		if err := s.MarkInFlight(ctx, "run-3", "OldSymbol", "func OldSymbol()"); err != nil {
			t.Fatalf("MarkInFlight: %v", err)
		}
		if err := s.Deprecate(ctx, "run-3", "OldSymbol"); err != nil {
			t.Fatalf("Deprecate: %v", err)
		}
		if got := statusOf(t, s, "OldSymbol"); got != StatusDeprecated {
			t.Fatalf("status = %q, want deprecated", got)
		}
		// Deprecated is still an active intent — siblings must see it to avoid
		// reintroducing the symbol.
		if active, _ := s.Active(ctx, "OldSymbol"); len(active) != 1 {
			t.Fatalf("Active after Deprecate returned %d rows, want 1", len(active))
		}
	})

	t.Run("deprecate binds an architect-declared NULL-run row", func(t *testing.T) {
		s := newEntStore(t)
		// A removal phase: architect declares the symbol (NULL run_id) and the
		// coder deletes it without ever marking it in_flight.
		if err := s.Declare(ctx, Entanglement{
			Producer: "phase-r", PhaseID: "phase-r", Kind: KindFunction, Name: "Doomed",
		}); err != nil {
			t.Fatalf("Declare: %v", err)
		}
		if err := s.Deprecate(ctx, "run-r", "Doomed"); err != nil {
			t.Fatalf("Deprecate: %v", err)
		}
		active, err := s.Active(ctx, "Doomed")
		if err != nil {
			t.Fatalf("Active: %v", err)
		}
		if len(active) != 1 || active[0].Status != StatusDeprecated {
			t.Fatalf("Active = %+v, want one deprecated row", active)
		}
		if active[0].RunID != "run-r" {
			t.Errorf("run_id = %q, want run-r (NULL declaration should bind)", active[0].RunID)
		}
	})

	t.Run("declare is idempotent on producer/kind/name", func(t *testing.T) {
		s := newEntStore(t)
		e := Entanglement{Producer: "phase-d", RunID: "run-4", PhaseID: "phase-d", Kind: KindType, Name: "Widget"}
		if err := s.Declare(ctx, e); err != nil {
			t.Fatalf("Declare 1: %v", err)
		}
		// Advance the lifecycle, then re-declare: the second Declare must not
		// reset the row back to 'declared'.
		if err := s.MarkInFlight(ctx, "run-4", "Widget", "type Widget struct{}"); err != nil {
			t.Fatalf("MarkInFlight: %v", err)
		}
		if err := s.Declare(ctx, e); err != nil {
			t.Fatalf("Declare 2 (idempotent): %v", err)
		}
		if got := statusOf(t, s, "Widget"); got != StatusInFlight {
			t.Fatalf("status after re-declare = %q, want in_flight (no reset)", got)
		}
		var count int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM entanglements WHERE name = 'Widget'`).Scan(&count); err != nil {
			t.Fatalf("count: %v", err)
		}
		if count != 1 {
			t.Fatalf("re-declare created a duplicate: %d rows, want 1", count)
		}
	})

	t.Run("active excludes terminal and legacy rows, orders by recency", func(t *testing.T) {
		s := newEntStore(t)
		// One active (in_flight) and one terminal (fulfilled) row for the same name.
		mustDeclare(t, s, "run-5", "phase-e", "Shared")
		if err := s.MarkInFlight(ctx, "run-5", "Shared", "v1"); err != nil {
			t.Fatalf("MarkInFlight run-5: %v", err)
		}
		if err := s.Fulfill(ctx, "run-5"); err != nil {
			t.Fatalf("Fulfill run-5: %v", err)
		}
		// A second producer is still in flight.
		if err := s.Declare(ctx, Entanglement{
			Producer: "phase-f", RunID: "run-6", PhaseID: "phase-f", Kind: KindFunction, Name: "Shared",
		}); err != nil {
			t.Fatalf("Declare run-6: %v", err)
		}
		if err := s.MarkInFlight(ctx, "run-6", "Shared", "v2"); err != nil {
			t.Fatalf("MarkInFlight run-6: %v", err)
		}
		active, err := s.Active(ctx, "Shared")
		if err != nil {
			t.Fatalf("Active: %v", err)
		}
		if len(active) != 1 {
			t.Fatalf("Active returned %d rows, want 1 (fulfilled excluded)", len(active))
		}
		if active[0].RunID != "run-6" || active[0].CurrentSignature != "v2" {
			t.Fatalf("Active returned wrong row: %+v", active[0])
		}
	})
}

// mustDeclare declares a function symbol and fails the test on error.
func mustDeclare(t *testing.T, s *EntanglementStore, runID, phaseID, name string) {
	t.Helper()
	if err := s.Declare(context.Background(), Entanglement{
		Producer: phaseID, RunID: runID, PhaseID: phaseID, Kind: KindFunction, Name: name,
	}); err != nil {
		t.Fatalf("Declare %q: %v", name, err)
	}
}

// TestMigration009PendingRows verifies the data migration: 'pending' rows move
// to 'fulfilled' when their producing run is done and 'withdrawn' when it
// failed, and are left untouched when the run is still going. It builds a
// pre-009 database (migrations 001-008) on disk, seeds rows, then applies the
// real 009 SQL file and asserts.
func TestMigration009PendingRows(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "pre009.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, schema); err != nil {
		t.Fatalf("base schema: %v", err)
	}
	applyMigrationsUpTo(t, db, "008")

	// Seed three runs and a pending entanglement keyed to each (producer = run id).
	for _, r := range []struct{ id, state string }{
		{"run-done", "done"}, {"run-failed", "failed"}, {"run-running", "running"},
	} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO constellation_runs (id, nebula_id, state, current_node) VALUES (?, 'neb', ?, 'n')`, r.id, r.state); err != nil {
			t.Fatalf("seed run %s: %v", r.id, err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO entanglements (producer, kind, name, signature, status)
			 VALUES (?, 'function', ?, 'sig', 'pending')`, r.id, "Sym-"+r.id); err != nil {
			t.Fatalf("seed entanglement %s: %v", r.id, err)
		}
	}

	// Apply the real 009 migration file.
	body, err := os.ReadFile(filepath.Join("migrations", "009_entanglement_lifecycle.sql"))
	if err != nil {
		t.Fatalf("read 009: %v", err)
	}
	if _, err := db.ExecContext(ctx, string(body)); err != nil {
		t.Fatalf("apply 009: %v", err)
	}

	want := map[string]string{
		"Sym-run-done":    StatusFulfilled,
		"Sym-run-failed":  StatusWithdrawn,
		"Sym-run-running": StatusPending,
	}
	for name, status := range want {
		var got string
		if err := db.QueryRowContext(ctx,
			`SELECT status FROM entanglements WHERE name = ?`, name).Scan(&got); err != nil {
			t.Fatalf("query %s: %v", name, err)
		}
		if got != status {
			t.Errorf("%s: status = %q, want %q", name, got, status)
		}
	}
}

// applyMigrationsUpTo reads the migration files from disk and applies those
// whose numeric prefix is <= cutoff, in lexical order.
func applyMigrationsUpTo(t *testing.T, db *sql.DB, cutoff string) {
	t.Helper()
	entries, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") && e.Name()[:3] <= cutoff && e.Name()[:3] != "999" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		body, err := os.ReadFile(filepath.Join("migrations", n))
		if err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		if _, err := db.ExecContext(context.Background(), string(body)); err != nil {
			t.Fatalf("apply %s: %v", n, err)
		}
	}
}
