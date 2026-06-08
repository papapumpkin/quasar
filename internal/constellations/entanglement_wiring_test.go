package constellations

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/papapumpkin/quasar/internal/blobstore"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// newEntanglementRuntime builds a runtime with an EntanglementStore wired, plus
// the store and a seeded nebula id, so wiring tests can assert lifecycle rows.
func newEntanglementRuntime(t *testing.T) (*Runtime, *fabric.EntanglementStore, string) {
	t.Helper()
	dir := t.TempDir()
	fab, err := fabric.NewSQLiteFabric(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("fabric: %v", err)
	}
	t.Cleanup(func() { _ = fab.DB().Close() })
	blobs, err := blobstore.New(filepath.Join(dir, "blobs"), fab.DB())
	if err != nil {
		t.Fatalf("blobs: %v", err)
	}
	nebStore := fabric.NewNebulaStore(fab.DB(), blobs)
	nebID, err := nebStore.Insert(context.Background(), fabric.NebulaRow{Name: "demo", Status: "running"})
	if err != nil {
		t.Fatalf("seed nebula: %v", err)
	}
	entStore := fabric.NewEntanglementStore(fab.DB())
	rt := New(RuntimeOpts{
		RunStore:      fabric.NewConstellationRunStore(fab.DB()),
		NebStore:      nebStore,
		Loader:        &fakeLoader{},
		RepoPath:      dir,
		Entanglements: entStore,
	})
	return rt, entStore, nebID
}

func TestArchitectDeclaresProducerSymbols(t *testing.T) {
	ctx := context.Background()
	rt, entStore, nebID := newEntanglementRuntime(t)
	st := NewState(NebulaSnapshot{ID: nebID}, 1)

	body := "## Solution\n\nIntroduce `type Sensor interface` and `func NewSensor`.\n\n## Tests\n\nfunc Unscanned()\n"
	args := map[string]any{"phases_toml": "[[phases]]\nid = \"p1\"\ntitle = \"t\"\nbody = \"\"\"" + body + "\"\"\"\n"}

	if _, err := opPersistPhases(ctx, rt, st, args); err != nil {
		t.Fatalf("opPersistPhases: %v", err)
	}

	for _, name := range []string{"Sensor", "NewSensor"} {
		active, err := entStore.Active(ctx, name)
		if err != nil {
			t.Fatalf("Active(%q): %v", name, err)
		}
		if len(active) != 1 {
			t.Fatalf("Active(%q) returned %d rows, want 1", name, len(active))
		}
		if active[0].Status != fabric.StatusDeclared || active[0].PhaseID != "p1" {
			t.Errorf("Active(%q)[0] = %+v, want declared row for phase p1", name, active[0])
		}
	}
	// A symbol outside the scanned sections must not be declared.
	if active, _ := entStore.Active(ctx, "Unscanned"); len(active) != 0 {
		t.Errorf("Unscanned symbol declared: %+v", active)
	}
}

func TestApplyTerminalEntanglements(t *testing.T) {
	ctx := context.Background()

	t.Run("withdraws non-terminal rows on failure", func(t *testing.T) {
		rt, entStore, _ := newEntanglementRuntime(t)
		seedInFlight(t, entStore, "run-x", "Symbol")
		rt.applyTerminalEntanglements(ctx, "run-x", StateFailed)
		assertStatus(t, entStore, "Symbol", fabric.StatusWithdrawn)
	})

	t.Run("fulfills in-flight rows on done", func(t *testing.T) {
		rt, entStore, _ := newEntanglementRuntime(t)
		seedInFlight(t, entStore, "run-y", "Symbol")
		rt.applyTerminalEntanglements(ctx, "run-y", StateDone)
		assertStatus(t, entStore, "Symbol", fabric.StatusFulfilled)
	})

	t.Run("nil store is a no-op", func(t *testing.T) {
		rt := New(runtimeOptsWithoutEntanglements(t))
		// Must not panic.
		rt.applyTerminalEntanglements(ctx, "run-z", StateFailed)
	})
}

func seedInFlight(t *testing.T, s *fabric.EntanglementStore, runID, name string) {
	t.Helper()
	ctx := context.Background()
	if err := s.Declare(ctx, fabric.Entanglement{
		Producer: name, RunID: runID, PhaseID: "p", Kind: fabric.KindFunction, Name: name,
	}); err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if err := s.MarkInFlight(ctx, runID, name, "sig"); err != nil {
		t.Fatalf("MarkInFlight: %v", err)
	}
}

func assertStatus(t *testing.T, s *fabric.EntanglementStore, name, want string) {
	t.Helper()
	all, err := s.Active(context.Background(), name)
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	// Withdraw and Fulfill are terminal, so they move the row out of Active;
	// assert absence for those and a single matching active row otherwise.
	switch want {
	case fabric.StatusWithdrawn, fabric.StatusFulfilled:
		if len(all) != 0 {
			t.Fatalf("expected %q to be terminal (absent from Active), got %+v", name, all)
		}
	default:
		if len(all) != 1 || all[0].Status != want {
			t.Fatalf("status of %q = %+v, want %q", name, all, want)
		}
	}
}

func runtimeOptsWithoutEntanglements(t *testing.T) RuntimeOpts {
	t.Helper()
	dir := t.TempDir()
	fab, err := fabric.NewSQLiteFabric(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("fabric: %v", err)
	}
	t.Cleanup(func() { _ = fab.DB().Close() })
	blobs, err := blobstore.New(filepath.Join(dir, "blobs"), fab.DB())
	if err != nil {
		t.Fatalf("blobs: %v", err)
	}
	return RuntimeOpts{
		RunStore: fabric.NewConstellationRunStore(fab.DB()),
		NebStore: fabric.NewNebulaStore(fab.DB(), blobs),
		Loader:   &fakeLoader{},
		RepoPath: dir,
	}
}
