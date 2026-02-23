package fabric

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

// testFabric creates a temporary SQLite fabric for testing and registers cleanup.
func testFabric(t *testing.T) *SQLiteFabric {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.fabric.db")
	b, err := NewSQLiteFabric(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteFabric(%q): %v", dbPath, err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

func TestNewSQLiteFabric(t *testing.T) {
	t.Parallel()

	t.Run("creates database and tables", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		// Verify WAL mode is active.
		var mode string
		if err := b.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
			t.Fatalf("query journal_mode: %v", err)
		}
		if mode != "wal" {
			t.Errorf("journal_mode = %q, want %q", mode, "wal")
		}

		// Verify all five tables exist by querying sqlite_master.
		tables := map[string]bool{"fabric": false, "entanglements": false, "file_claims": false, "discoveries": false, "pulses": false}
		rows, err := b.db.Query("SELECT name FROM sqlite_master WHERE type='table'")
		if err != nil {
			t.Fatalf("query sqlite_master: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatalf("scan table name: %v", err)
			}
			tables[name] = true
		}
		for name, found := range tables {
			if !found {
				t.Errorf("table %q not created", name)
			}
		}
	})

	t.Run("idempotent schema creation", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "idempotent.fabric.db")

		// Open twice — second open should succeed without error.
		b1, err := NewSQLiteFabric(context.Background(), dbPath)
		if err != nil {
			t.Fatalf("first open: %v", err)
		}
		b1.Close()

		b2, err := NewSQLiteFabric(context.Background(), dbPath)
		if err != nil {
			t.Fatalf("second open: %v", err)
		}
		b2.Close()
	})

	t.Run("invalid path returns error", func(t *testing.T) {
		t.Parallel()
		_, err := NewSQLiteFabric(context.Background(), filepath.Join(os.DevNull, "nonexistent", "path.db"))
		if err == nil {
			t.Fatal("expected error for invalid path")
		}
	})
}

func TestPhaseState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("set and get", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		if err := b.SetPhaseState(ctx, "phase-1", StateQueued); err != nil {
			t.Fatalf("SetPhaseState: %v", err)
		}
		got, err := b.GetPhaseState(ctx, "phase-1")
		if err != nil {
			t.Fatalf("GetPhaseState: %v", err)
		}
		if got != StateQueued {
			t.Errorf("state = %q, want %q", got, StateQueued)
		}
	})

	t.Run("update existing", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		if err := b.SetPhaseState(ctx, "phase-1", StateQueued); err != nil {
			t.Fatalf("initial set: %v", err)
		}
		if err := b.SetPhaseState(ctx, "phase-1", StateRunning); err != nil {
			t.Fatalf("update: %v", err)
		}
		got, err := b.GetPhaseState(ctx, "phase-1")
		if err != nil {
			t.Fatalf("GetPhaseState: %v", err)
		}
		if got != StateRunning {
			t.Errorf("state = %q, want %q", got, StateRunning)
		}
	})

	t.Run("missing phase returns empty", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		got, err := b.GetPhaseState(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("GetPhaseState: %v", err)
		}
		if got != "" {
			t.Errorf("state = %q, want empty", got)
		}
	})
}

func TestEntanglements(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("publish and query single", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		c := Entanglement{
			Producer:  "phase-1",
			Kind:      KindInterface,
			Name:      "Fabric",
			Signature: "type Fabric interface { Close() error }",
			Package:   "fabric",
		}
		if err := b.PublishEntanglement(ctx, c); err != nil {
			t.Fatalf("PublishEntanglement: %v", err)
		}

		got, err := b.EntanglementsFor(ctx, "phase-1")
		if err != nil {
			t.Fatalf("EntanglementsFor: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("len(entanglements) = %d, want 1", len(got))
		}
		if got[0].Name != "Fabric" || got[0].Kind != KindInterface {
			t.Errorf("entanglement = %+v, want Name=Fabric Kind=interface", got[0])
		}
		if got[0].Status != StatusPending {
			t.Errorf("status = %q, want %q", got[0].Status, StatusPending)
		}
	})

	t.Run("publish batch", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		entanglements := []Entanglement{
			{Producer: "phase-2", Kind: KindType, Name: "Config", Signature: "type Config struct{}", Package: "config"},
			{Producer: "phase-2", Kind: KindFunction, Name: "NewConfig", Signature: "func NewConfig() *Config", Package: "config"},
		}
		if err := b.PublishEntanglements(ctx, entanglements); err != nil {
			t.Fatalf("PublishEntanglements: %v", err)
		}

		got, err := b.EntanglementsFor(ctx, "phase-2")
		if err != nil {
			t.Fatalf("EntanglementsFor: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len(entanglements) = %d, want 2", len(got))
		}
	})

	t.Run("upsert on duplicate", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		c := Entanglement{
			Producer:  "phase-1",
			Kind:      KindFunction,
			Name:      "Run",
			Signature: "func Run() error",
			Package:   "main",
		}
		if err := b.PublishEntanglement(ctx, c); err != nil {
			t.Fatalf("first publish: %v", err)
		}

		// Update signature — same producer/kind/name.
		c.Signature = "func Run(ctx context.Context) error"
		if err := b.PublishEntanglement(ctx, c); err != nil {
			t.Fatalf("upsert: %v", err)
		}

		got, err := b.EntanglementsFor(ctx, "phase-1")
		if err != nil {
			t.Fatalf("EntanglementsFor: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("len(entanglements) = %d, want 1 (upsert)", len(got))
		}
		if got[0].Signature != "func Run(ctx context.Context) error" {
			t.Errorf("signature = %q, want updated", got[0].Signature)
		}
	})

	t.Run("all entanglements across phases", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		if err := b.PublishEntanglement(ctx, Entanglement{Producer: "p1", Kind: KindType, Name: "A", Signature: "type A int"}); err != nil {
			t.Fatalf("publish A: %v", err)
		}
		if err := b.PublishEntanglement(ctx, Entanglement{Producer: "p2", Kind: KindType, Name: "B", Signature: "type B string"}); err != nil {
			t.Fatalf("publish B: %v", err)
		}

		got, err := b.AllEntanglements(ctx)
		if err != nil {
			t.Fatalf("AllEntanglements: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("len(AllEntanglements) = %d, want 2", len(got))
		}
	})

	t.Run("empty entanglements for unknown phase", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		got, err := b.EntanglementsFor(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("EntanglementsFor: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("len(entanglements) = %d, want 0", len(got))
		}
	})

	t.Run("empty batch is no-op", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		if err := b.PublishEntanglements(ctx, nil); err != nil {
			t.Fatalf("PublishEntanglements(nil): %v", err)
		}
	})
}

func TestFileClaims(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("claim and query", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		if err := b.ClaimFile(ctx, "internal/fabric/fabric.go", "phase-1"); err != nil {
			t.Fatalf("ClaimFile: %v", err)
		}

		owner, err := b.FileOwner(ctx, "internal/fabric/fabric.go")
		if err != nil {
			t.Fatalf("FileOwner: %v", err)
		}
		if owner != "phase-1" {
			t.Errorf("owner = %q, want %q", owner, "phase-1")
		}

		claims, err := b.ClaimsFor(ctx, "phase-1")
		if err != nil {
			t.Fatalf("ClaimsFor: %v", err)
		}
		if len(claims) != 1 || claims[0] != "internal/fabric/fabric.go" {
			t.Errorf("claims = %v, want [internal/fabric/fabric.go]", claims)
		}
	})

	t.Run("claim conflict returns error", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		if err := b.ClaimFile(ctx, "shared.go", "phase-1"); err != nil {
			t.Fatalf("first claim: %v", err)
		}
		err := b.ClaimFile(ctx, "shared.go", "phase-2")
		if !errors.Is(err, ErrFileAlreadyClaimed) {
			t.Errorf("err = %v, want ErrFileAlreadyClaimed", err)
		}
	})

	t.Run("re-claim by same phase is no-op", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		if err := b.ClaimFile(ctx, "mine.go", "phase-1"); err != nil {
			t.Fatalf("first claim: %v", err)
		}
		if err := b.ClaimFile(ctx, "mine.go", "phase-1"); err != nil {
			t.Fatalf("re-claim same phase: %v", err)
		}
	})

	t.Run("release and re-claim by another", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		if err := b.ClaimFile(ctx, "transferable.go", "phase-1"); err != nil {
			t.Fatalf("initial claim: %v", err)
		}
		if err := b.ReleaseClaims(ctx, "phase-1"); err != nil {
			t.Fatalf("release: %v", err)
		}
		if err := b.ClaimFile(ctx, "transferable.go", "phase-2"); err != nil {
			t.Fatalf("re-claim after release: %v", err)
		}

		owner, err := b.FileOwner(ctx, "transferable.go")
		if err != nil {
			t.Fatalf("FileOwner: %v", err)
		}
		if owner != "phase-2" {
			t.Errorf("owner = %q, want %q", owner, "phase-2")
		}
	})

	t.Run("unclaimed file returns empty", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		owner, err := b.FileOwner(ctx, "nobody.go")
		if err != nil {
			t.Fatalf("FileOwner: %v", err)
		}
		if owner != "" {
			t.Errorf("owner = %q, want empty", owner)
		}
	})

	t.Run("claims for unknown phase is empty", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		claims, err := b.ClaimsFor(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("ClaimsFor: %v", err)
		}
		if len(claims) != 0 {
			t.Errorf("claims = %v, want empty", claims)
		}
	})
}

func TestConcurrentAccess(t *testing.T) {
	t.Parallel()
	b := testFabric(t)
	ctx := context.Background()

	const goroutines = 10
	const opsPerGoroutine = 20

	var wg sync.WaitGroup
	errs := make(chan error, goroutines*opsPerGoroutine*3)

	// Concurrent writers: set phase states + publish entanglements.
	for i := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			phaseID := "phase-" + itoa(id)
			for j := range opsPerGoroutine {
				if err := b.SetPhaseState(ctx, phaseID, StateRunning); err != nil {
					errs <- err
				}
				c := Entanglement{
					Producer:  phaseID,
					Kind:      KindFunction,
					Name:      "Func" + itoa(j),
					Signature: "func() error",
				}
				if err := b.PublishEntanglement(ctx, c); err != nil {
					errs <- err
				}
			}
		}(i)
	}

	// Concurrent readers.
	for i := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			phaseID := "phase-" + itoa(id)
			for range opsPerGoroutine {
				if _, err := b.GetPhaseState(ctx, phaseID); err != nil {
					errs <- err
				}
				if _, err := b.EntanglementsFor(ctx, phaseID); err != nil {
					errs <- err
				}
				if _, err := b.AllEntanglements(ctx); err != nil {
					errs <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent operation error: %v", err)
	}
}

func TestDiscoveries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("post and query by task", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		d := Discovery{
			SourceTask: "phase-1",
			Kind:       DiscoveryFileConflict,
			Detail:     "both phases modify shared.go",
			Affects:    "phase-2",
		}
		if _, err := b.PostDiscovery(ctx, d); err != nil {
			t.Fatalf("PostDiscovery: %v", err)
		}

		got, err := b.Discoveries(ctx, "phase-1")
		if err != nil {
			t.Fatalf("Discoveries: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("len(discoveries) = %d, want 1", len(got))
		}
		if got[0].Kind != DiscoveryFileConflict {
			t.Errorf("kind = %q, want %q", got[0].Kind, DiscoveryFileConflict)
		}
		if got[0].Detail != "both phases modify shared.go" {
			t.Errorf("detail = %q, want %q", got[0].Detail, "both phases modify shared.go")
		}
		if got[0].Affects != "phase-2" {
			t.Errorf("affects = %q, want %q", got[0].Affects, "phase-2")
		}
		if got[0].Resolved {
			t.Error("expected resolved=false for new discovery")
		}
		if got[0].ID == 0 {
			t.Error("expected non-zero ID")
		}
	})

	t.Run("post without affects", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		d := Discovery{
			SourceTask: "phase-1",
			Kind:       DiscoveryBudgetAlert,
			Detail:     "approaching 80% budget",
		}
		if _, err := b.PostDiscovery(ctx, d); err != nil {
			t.Fatalf("PostDiscovery: %v", err)
		}

		got, err := b.Discoveries(ctx, "phase-1")
		if err != nil {
			t.Fatalf("Discoveries: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("len(discoveries) = %d, want 1", len(got))
		}
		if got[0].Affects != "" {
			t.Errorf("affects = %q, want empty", got[0].Affects)
		}
	})

	t.Run("all discoveries across tasks", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		if _, err := b.PostDiscovery(ctx, Discovery{SourceTask: "p1", Kind: DiscoveryFileConflict, Detail: "conflict A"}); err != nil {
			t.Fatalf("post A: %v", err)
		}
		if _, err := b.PostDiscovery(ctx, Discovery{SourceTask: "p2", Kind: DiscoveryMissingDependency, Detail: "missing dep B"}); err != nil {
			t.Fatalf("post B: %v", err)
		}

		got, err := b.AllDiscoveries(ctx)
		if err != nil {
			t.Fatalf("AllDiscoveries: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("len(AllDiscoveries) = %d, want 2", len(got))
		}
	})

	t.Run("resolve discovery", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		if _, err := b.PostDiscovery(ctx, Discovery{SourceTask: "p1", Kind: DiscoveryEntanglementDispute, Detail: "signature mismatch"}); err != nil {
			t.Fatalf("PostDiscovery: %v", err)
		}

		all, err := b.AllDiscoveries(ctx)
		if err != nil {
			t.Fatalf("AllDiscoveries: %v", err)
		}
		if len(all) != 1 {
			t.Fatalf("expected 1 discovery, got %d", len(all))
		}

		if err := b.ResolveDiscovery(ctx, all[0].ID); err != nil {
			t.Fatalf("ResolveDiscovery: %v", err)
		}

		unresolved, err := b.UnresolvedDiscoveries(ctx)
		if err != nil {
			t.Fatalf("UnresolvedDiscoveries: %v", err)
		}
		if len(unresolved) != 0 {
			t.Errorf("len(UnresolvedDiscoveries) = %d, want 0", len(unresolved))
		}
	})

	t.Run("unresolved discoveries", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		if _, err := b.PostDiscovery(ctx, Discovery{SourceTask: "p1", Kind: DiscoveryFileConflict, Detail: "conflict 1"}); err != nil {
			t.Fatalf("post 1: %v", err)
		}
		if _, err := b.PostDiscovery(ctx, Discovery{SourceTask: "p2", Kind: DiscoveryRequirementsAmbiguity, Detail: "ambiguity 2"}); err != nil {
			t.Fatalf("post 2: %v", err)
		}

		// Resolve the first one.
		all, err := b.AllDiscoveries(ctx)
		if err != nil {
			t.Fatalf("AllDiscoveries: %v", err)
		}
		if err := b.ResolveDiscovery(ctx, all[0].ID); err != nil {
			t.Fatalf("ResolveDiscovery: %v", err)
		}

		unresolved, err := b.UnresolvedDiscoveries(ctx)
		if err != nil {
			t.Fatalf("UnresolvedDiscoveries: %v", err)
		}
		if len(unresolved) != 1 {
			t.Fatalf("len(UnresolvedDiscoveries) = %d, want 1", len(unresolved))
		}
		if unresolved[0].Kind != DiscoveryRequirementsAmbiguity {
			t.Errorf("kind = %q, want %q", unresolved[0].Kind, DiscoveryRequirementsAmbiguity)
		}
	})

	t.Run("resolve nonexistent returns error", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		err := b.ResolveDiscovery(ctx, 9999)
		if err == nil {
			t.Fatal("expected error for nonexistent discovery")
		}
	})

	t.Run("empty discoveries for unknown task", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		got, err := b.Discoveries(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("Discoveries: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("len(discoveries) = %d, want 0", len(got))
		}
	})
}

func TestPulses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("emit and query", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		pulse := Pulse{
			TaskID:  "phase-1",
			Content: "decided to use observer pattern for event handling",
			Kind:    PulseDecision,
		}
		if err := b.EmitPulse(ctx, pulse); err != nil {
			t.Fatalf("EmitPulse: %v", err)
		}

		got, err := b.PulsesFor(ctx, "phase-1")
		if err != nil {
			t.Fatalf("PulsesFor: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("len(pulses) = %d, want 1", len(got))
		}
		if got[0].Kind != PulseDecision {
			t.Errorf("kind = %q, want %q", got[0].Kind, PulseDecision)
		}
		if got[0].Content != "decided to use observer pattern for event handling" {
			t.Errorf("content = %q, want expected", got[0].Content)
		}
		if got[0].TaskID != "phase-1" {
			t.Errorf("task_id = %q, want %q", got[0].TaskID, "phase-1")
		}
		if got[0].ID == 0 {
			t.Error("expected non-zero ID")
		}
	})

	t.Run("multiple pulses for same task", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		pulses := []Pulse{
			{TaskID: "phase-1", Content: "starting implementation", Kind: PulseNote},
			{TaskID: "phase-1", Content: "build failed: missing import", Kind: PulseFailure},
			{TaskID: "phase-1", Content: "reviewer says: add error handling", Kind: PulseReviewerFeedback},
		}
		for _, p := range pulses {
			if err := b.EmitPulse(ctx, p); err != nil {
				t.Fatalf("EmitPulse(%q): %v", p.Kind, err)
			}
		}

		got, err := b.PulsesFor(ctx, "phase-1")
		if err != nil {
			t.Fatalf("PulsesFor: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("len(pulses) = %d, want 3", len(got))
		}
		// Verify ordering by ID (insertion order).
		if got[0].Kind != PulseNote || got[1].Kind != PulseFailure || got[2].Kind != PulseReviewerFeedback {
			t.Errorf("pulse kinds = [%s, %s, %s], want [note, failure, reviewer_feedback]",
				got[0].Kind, got[1].Kind, got[2].Kind)
		}
	})

	t.Run("pulses isolated by task", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		if err := b.EmitPulse(ctx, Pulse{TaskID: "p1", Content: "note for p1", Kind: PulseNote}); err != nil {
			t.Fatalf("EmitPulse p1: %v", err)
		}
		if err := b.EmitPulse(ctx, Pulse{TaskID: "p2", Content: "note for p2", Kind: PulseNote}); err != nil {
			t.Fatalf("EmitPulse p2: %v", err)
		}

		got, err := b.PulsesFor(ctx, "p1")
		if err != nil {
			t.Fatalf("PulsesFor: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("len(pulses for p1) = %d, want 1", len(got))
		}
		if got[0].Content != "note for p1" {
			t.Errorf("content = %q, want %q", got[0].Content, "note for p1")
		}
	})

	t.Run("empty pulses for unknown task", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		got, err := b.PulsesFor(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("PulsesFor: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("len(pulses) = %d, want 0", len(got))
		}
	})

	t.Run("emit pulse returning ID", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		id, err := b.EmitPulseReturningID(ctx, Pulse{
			TaskID:  "phase-1",
			Content: "cursor-based pagination chosen",
			Kind:    PulseDecision,
		})
		if err != nil {
			t.Fatalf("EmitPulseReturningID: %v", err)
		}
		if id == 0 {
			t.Error("expected non-zero ID from EmitPulseReturningID")
		}
	})

	t.Run("all pulses", func(t *testing.T) {
		t.Parallel()
		b := testFabric(t)

		if err := b.EmitPulse(ctx, Pulse{TaskID: "t1", Content: "note 1", Kind: PulseNote}); err != nil {
			t.Fatalf("EmitPulse: %v", err)
		}
		if err := b.EmitPulse(ctx, Pulse{TaskID: "t2", Content: "note 2", Kind: PulseDecision}); err != nil {
			t.Fatalf("EmitPulse: %v", err)
		}

		got, err := b.AllPulses(ctx)
		if err != nil {
			t.Fatalf("AllPulses: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len(all pulses) = %d, want 2", len(got))
		}
	})
}

// itoa converts an int to its string representation using stdlib strconv.
func itoa(n int) string {
	return strconv.Itoa(n)
}
