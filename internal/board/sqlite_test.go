package board

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

// testBoard creates a temporary SQLite board for testing and registers cleanup.
func testBoard(t *testing.T) *SQLiteBoard {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.board.db")
	b, err := NewSQLiteBoard(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteBoard(%q): %v", dbPath, err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

func TestNewSQLiteBoard(t *testing.T) {
	t.Parallel()

	t.Run("creates database and tables", func(t *testing.T) {
		t.Parallel()
		b := testBoard(t)

		// Verify WAL mode is active.
		var mode string
		if err := b.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
			t.Fatalf("query journal_mode: %v", err)
		}
		if mode != "wal" {
			t.Errorf("journal_mode = %q, want %q", mode, "wal")
		}

		// Verify all three tables exist by querying sqlite_master.
		tables := map[string]bool{"board": false, "contracts": false, "file_claims": false}
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
		dbPath := filepath.Join(dir, "idempotent.board.db")

		// Open twice — second open should succeed without error.
		b1, err := NewSQLiteBoard(context.Background(), dbPath)
		if err != nil {
			t.Fatalf("first open: %v", err)
		}
		b1.Close()

		b2, err := NewSQLiteBoard(context.Background(), dbPath)
		if err != nil {
			t.Fatalf("second open: %v", err)
		}
		b2.Close()
	})

	t.Run("invalid path returns error", func(t *testing.T) {
		t.Parallel()
		_, err := NewSQLiteBoard(context.Background(), filepath.Join(os.DevNull, "nonexistent", "path.db"))
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
		b := testBoard(t)

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
		b := testBoard(t)

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
		b := testBoard(t)

		got, err := b.GetPhaseState(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("GetPhaseState: %v", err)
		}
		if got != "" {
			t.Errorf("state = %q, want empty", got)
		}
	})
}

func TestContracts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("publish and query single", func(t *testing.T) {
		t.Parallel()
		b := testBoard(t)

		c := Contract{
			Producer:  "phase-1",
			Kind:      KindInterface,
			Name:      "Board",
			Signature: "type Board interface { Close() error }",
			Package:   "board",
		}
		if err := b.PublishContract(ctx, c); err != nil {
			t.Fatalf("PublishContract: %v", err)
		}

		got, err := b.ContractsFor(ctx, "phase-1")
		if err != nil {
			t.Fatalf("ContractsFor: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("len(contracts) = %d, want 1", len(got))
		}
		if got[0].Name != "Board" || got[0].Kind != KindInterface {
			t.Errorf("contract = %+v, want Name=Board Kind=interface", got[0])
		}
		if got[0].Status != StatusFulfilled {
			t.Errorf("status = %q, want %q", got[0].Status, StatusFulfilled)
		}
	})

	t.Run("publish batch", func(t *testing.T) {
		t.Parallel()
		b := testBoard(t)

		contracts := []Contract{
			{Producer: "phase-2", Kind: KindType, Name: "Config", Signature: "type Config struct{}", Package: "config"},
			{Producer: "phase-2", Kind: KindFunction, Name: "NewConfig", Signature: "func NewConfig() *Config", Package: "config"},
		}
		if err := b.PublishContracts(ctx, contracts); err != nil {
			t.Fatalf("PublishContracts: %v", err)
		}

		got, err := b.ContractsFor(ctx, "phase-2")
		if err != nil {
			t.Fatalf("ContractsFor: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len(contracts) = %d, want 2", len(got))
		}
	})

	t.Run("upsert on duplicate", func(t *testing.T) {
		t.Parallel()
		b := testBoard(t)

		c := Contract{
			Producer:  "phase-1",
			Kind:      KindFunction,
			Name:      "Run",
			Signature: "func Run() error",
			Package:   "main",
		}
		if err := b.PublishContract(ctx, c); err != nil {
			t.Fatalf("first publish: %v", err)
		}

		// Update signature — same producer/kind/name.
		c.Signature = "func Run(ctx context.Context) error"
		if err := b.PublishContract(ctx, c); err != nil {
			t.Fatalf("upsert: %v", err)
		}

		got, err := b.ContractsFor(ctx, "phase-1")
		if err != nil {
			t.Fatalf("ContractsFor: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("len(contracts) = %d, want 1 (upsert)", len(got))
		}
		if got[0].Signature != "func Run(ctx context.Context) error" {
			t.Errorf("signature = %q, want updated", got[0].Signature)
		}
	})

	t.Run("all contracts across phases", func(t *testing.T) {
		t.Parallel()
		b := testBoard(t)

		if err := b.PublishContract(ctx, Contract{Producer: "p1", Kind: KindType, Name: "A", Signature: "type A int"}); err != nil {
			t.Fatalf("publish A: %v", err)
		}
		if err := b.PublishContract(ctx, Contract{Producer: "p2", Kind: KindType, Name: "B", Signature: "type B string"}); err != nil {
			t.Fatalf("publish B: %v", err)
		}

		got, err := b.AllContracts(ctx)
		if err != nil {
			t.Fatalf("AllContracts: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("len(AllContracts) = %d, want 2", len(got))
		}
	})

	t.Run("empty contracts for unknown phase", func(t *testing.T) {
		t.Parallel()
		b := testBoard(t)

		got, err := b.ContractsFor(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("ContractsFor: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("len(contracts) = %d, want 0", len(got))
		}
	})

	t.Run("empty batch is no-op", func(t *testing.T) {
		t.Parallel()
		b := testBoard(t)

		if err := b.PublishContracts(ctx, nil); err != nil {
			t.Fatalf("PublishContracts(nil): %v", err)
		}
	})
}

func TestFileClaims(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("claim and query", func(t *testing.T) {
		t.Parallel()
		b := testBoard(t)

		if err := b.ClaimFile(ctx, "internal/board/board.go", "phase-1"); err != nil {
			t.Fatalf("ClaimFile: %v", err)
		}

		owner, err := b.FileOwner(ctx, "internal/board/board.go")
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
		if len(claims) != 1 || claims[0] != "internal/board/board.go" {
			t.Errorf("claims = %v, want [internal/board/board.go]", claims)
		}
	})

	t.Run("claim conflict returns error", func(t *testing.T) {
		t.Parallel()
		b := testBoard(t)

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
		b := testBoard(t)

		if err := b.ClaimFile(ctx, "mine.go", "phase-1"); err != nil {
			t.Fatalf("first claim: %v", err)
		}
		if err := b.ClaimFile(ctx, "mine.go", "phase-1"); err != nil {
			t.Fatalf("re-claim same phase: %v", err)
		}
	})

	t.Run("release and re-claim by another", func(t *testing.T) {
		t.Parallel()
		b := testBoard(t)

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
		b := testBoard(t)

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
		b := testBoard(t)

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
	b := testBoard(t)
	ctx := context.Background()

	const goroutines = 10
	const opsPerGoroutine = 20

	var wg sync.WaitGroup
	errs := make(chan error, goroutines*opsPerGoroutine*3)

	// Concurrent writers: set phase states + publish contracts.
	for i := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			phaseID := "phase-" + itoa(id)
			for j := range opsPerGoroutine {
				if err := b.SetPhaseState(ctx, phaseID, StateRunning); err != nil {
					errs <- err
				}
				c := Contract{
					Producer:  phaseID,
					Kind:      KindFunction,
					Name:      "Func" + itoa(j),
					Signature: "func() error",
				}
				if err := b.PublishContract(ctx, c); err != nil {
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
				if _, err := b.ContractsFor(ctx, phaseID); err != nil {
					errs <- err
				}
				if _, err := b.AllContracts(ctx); err != nil {
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

// itoa converts an int to its string representation using stdlib strconv.
func itoa(n int) string {
	return strconv.Itoa(n)
}
