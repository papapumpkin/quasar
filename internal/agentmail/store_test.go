package agentmail

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// testStore opens a connection to the test database, initializes the schema,
// and cleans all tables before returning a ready-to-use Store. Tests that
// require a live Dolt/MySQL instance are skipped when the database is not
// reachable.
func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := testDSN(t)
	db, err := sql.Open("mysql", dsn+"?parseTime=true")
	if err != nil {
		t.Skipf("skipping integration test: cannot open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("skipping integration test: database not reachable: %v", err)
	}

	if err := InitDB(ctx, db); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// Clean tables in dependency order.
	for _, table := range []string{"changes", "file_claims", "messages", "agents"} {
		if _, err := db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			t.Fatalf("cleaning table %s: %v", table, err)
		}
	}

	return NewStore(db)
}

func TestNewUUID(t *testing.T) {
	t.Parallel()
	id, err := newUUID()
	if err != nil {
		t.Fatalf("newUUID: %v", err)
	}
	// UUID v4 format: 8-4-4-4-12 hex chars.
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Fatalf("expected 5 parts, got %d: %q", len(parts), id)
	}
	if len(id) != 36 {
		t.Errorf("expected 36 chars, got %d: %q", len(id), id)
	}

	// Uniqueness: two calls should produce different IDs.
	id2, err := newUUID()
	if err != nil {
		t.Fatalf("second newUUID: %v", err)
	}
	if id == id2 {
		t.Errorf("two UUIDs are identical: %q", id)
	}
}

func TestRegisterAgent(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	id, err := store.RegisterAgent(ctx, "coder-1", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty agent ID")
	}
}

func TestHeartbeat(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	id, err := store.RegisterAgent(ctx, "coder-hb", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	if err := store.Heartbeat(ctx, id); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
}

func TestHeartbeat_UnknownAgent(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	err := store.Heartbeat(ctx, "nonexistent-agent-id")
	if err == nil {
		t.Fatal("expected error for unknown agent, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestCleanupStaleAgents(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	// Register an agent and claim a file.
	id, err := store.RegisterAgent(ctx, "stale-agent", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	claimed, _, err := store.ClaimFiles(ctx, id, []string{"stale.go"})
	if err != nil {
		t.Fatalf("ClaimFiles: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("expected 1 claimed, got %d", len(claimed))
	}

	// Set the agent's heartbeat far in the past.
	_, err = store.db.ExecContext(ctx,
		"UPDATE agents SET last_heartbeat = '2000-01-01 00:00:00' WHERE id = ?", id)
	if err != nil {
		t.Fatalf("setting stale heartbeat: %v", err)
	}

	// Cleanup should release the file claim.
	staleAgents, err := store.CleanupStaleAgents(ctx, 1*time.Second)
	if err != nil {
		t.Fatalf("CleanupStaleAgents: %v", err)
	}
	if len(staleAgents) != 1 {
		t.Fatalf("expected 1 stale agent, got %d", len(staleAgents))
	}
	if staleAgents[0].Name != "stale-agent" {
		t.Errorf("stale agent name = %q, want %q", staleAgents[0].Name, "stale-agent")
	}

	claims, err := store.GetFileClaims(ctx, []string{"stale.go"})
	if err != nil {
		t.Fatalf("GetFileClaims: %v", err)
	}
	if len(claims) != 0 {
		t.Errorf("expected 0 claims after cleanup, got %d", len(claims))
	}
}

func TestSendAndReadMessages(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	id, err := store.RegisterAgent(ctx, "sender-1", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	msgID, err := store.SendMessage(ctx, id, "broadcast", "hello", "world")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if msgID == 0 {
		t.Fatal("expected non-zero message ID")
	}

	// Read all messages — should include the one we just sent.
	msgs, err := store.ReadMessages(ctx, nil, nil)
	if err != nil {
		t.Fatalf("ReadMessages: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected at least 1 message")
	}
	if msgs[0].Subject != "hello" {
		t.Errorf("subject = %q, want %q", msgs[0].Subject, "hello")
	}
}

func TestReadMessages_Filters(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	id, err := store.RegisterAgent(ctx, "filter-agent", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Send messages on different channels.
	if _, err := store.SendMessage(ctx, id, "alpha", "a1", "body"); err != nil {
		t.Fatalf("SendMessage alpha: %v", err)
	}
	if _, err := store.SendMessage(ctx, id, "beta", "b1", "body"); err != nil {
		t.Fatalf("SendMessage beta: %v", err)
	}

	// Filter by channel.
	ch := "alpha"
	msgs, err := store.ReadMessages(ctx, nil, &ch)
	if err != nil {
		t.Fatalf("ReadMessages with channel filter: %v", err)
	}
	for _, m := range msgs {
		if m.Channel != "alpha" {
			t.Errorf("expected channel alpha, got %q", m.Channel)
		}
	}

	// Filter by time — use a time in the future to get zero results.
	future := time.Now().Add(1 * time.Hour)
	msgs, err = store.ReadMessages(ctx, &future, nil)
	if err != nil {
		t.Fatalf("ReadMessages with since filter: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after future cutoff, got %d", len(msgs))
	}
}

func TestClaimFiles_Basic(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	id, err := store.RegisterAgent(ctx, "claimer", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	claimed, conflicts, err := store.ClaimFiles(ctx, id, []string{"a.go", "b.go"})
	if err != nil {
		t.Fatalf("ClaimFiles: %v", err)
	}
	if len(claimed) != 2 {
		t.Errorf("expected 2 claimed, got %d", len(claimed))
	}
	if len(conflicts) != 0 {
		t.Errorf("expected 0 conflicts, got %d", len(conflicts))
	}
}

func TestClaimFiles_Conflict(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	id1, err := store.RegisterAgent(ctx, "agent-1", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent 1: %v", err)
	}
	id2, err := store.RegisterAgent(ctx, "agent-2", "reviewer")
	if err != nil {
		t.Fatalf("RegisterAgent 2: %v", err)
	}

	// Agent 1 claims the file.
	claimed, _, err := store.ClaimFiles(ctx, id1, []string{"conflict.go"})
	if err != nil {
		t.Fatalf("ClaimFiles agent1: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("expected 1 claimed by agent1, got %d", len(claimed))
	}

	// Agent 2 tries to claim the same file — should get a conflict.
	claimed2, conflicts2, err := store.ClaimFiles(ctx, id2, []string{"conflict.go"})
	if err != nil {
		t.Fatalf("ClaimFiles agent2: %v", err)
	}
	if len(claimed2) != 0 {
		t.Errorf("expected 0 claimed by agent2, got %d", len(claimed2))
	}
	if len(conflicts2) != 1 {
		t.Errorf("expected 1 conflict, got %d", len(conflicts2))
	}
	if len(conflicts2) > 0 && conflicts2[0] != "conflict.go" {
		t.Errorf("conflict path = %q, want %q", conflicts2[0], "conflict.go")
	}
}

func TestClaimFiles_SameAgent(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	id, err := store.RegisterAgent(ctx, "reclaimer", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Claim the file once.
	claimed, _, err := store.ClaimFiles(ctx, id, []string{"same.go"})
	if err != nil {
		t.Fatalf("ClaimFiles first: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("expected 1 claimed, got %d", len(claimed))
	}

	// Claim it again — should succeed (idempotent).
	claimed2, conflicts2, err := store.ClaimFiles(ctx, id, []string{"same.go"})
	if err != nil {
		t.Fatalf("ClaimFiles second: %v", err)
	}
	if len(claimed2) != 1 {
		t.Errorf("expected 1 claimed on re-claim, got %d", len(claimed2))
	}
	if len(conflicts2) != 0 {
		t.Errorf("expected 0 conflicts on re-claim, got %d", len(conflicts2))
	}
}

func TestReleaseFiles(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	id, err := store.RegisterAgent(ctx, "releaser", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Claim and then release.
	if _, _, err := store.ClaimFiles(ctx, id, []string{"rel.go"}); err != nil {
		t.Fatalf("ClaimFiles: %v", err)
	}

	released, err := store.ReleaseFiles(ctx, id, []string{"rel.go"})
	if err != nil {
		t.Fatalf("ReleaseFiles: %v", err)
	}
	if len(released) != 1 {
		t.Errorf("expected 1 released, got %d", len(released))
	}

	// Release again — should return empty since nothing to release.
	released2, err := store.ReleaseFiles(ctx, id, []string{"rel.go"})
	if err != nil {
		t.Fatalf("ReleaseFiles second: %v", err)
	}
	if len(released2) != 0 {
		t.Errorf("expected 0 released on second call, got %d", len(released2))
	}
}

func TestGetFileClaims(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	id, err := store.RegisterAgent(ctx, "claim-getter", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	if _, _, err := store.ClaimFiles(ctx, id, []string{"x.go", "y.go"}); err != nil {
		t.Fatalf("ClaimFiles: %v", err)
	}

	t.Run("all claims", func(t *testing.T) {
		claims, err := store.GetFileClaims(ctx, nil)
		if err != nil {
			t.Fatalf("GetFileClaims(nil): %v", err)
		}
		if len(claims) < 2 {
			t.Errorf("expected at least 2 claims, got %d", len(claims))
		}
	})

	t.Run("specific files", func(t *testing.T) {
		claims, err := store.GetFileClaims(ctx, []string{"x.go"})
		if err != nil {
			t.Fatalf("GetFileClaims([x.go]): %v", err)
		}
		if len(claims) != 1 {
			t.Errorf("expected 1 claim, got %d", len(claims))
		}
	})

	t.Run("empty list", func(t *testing.T) {
		claims, err := store.GetFileClaims(ctx, []string{})
		if err != nil {
			t.Fatalf("GetFileClaims([]): %v", err)
		}
		if claims != nil {
			t.Errorf("expected nil for empty list, got %v", claims)
		}
	})
}

func TestAnnounceAndGetChanges(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	id, err := store.RegisterAgent(ctx, "changer", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	changeID, err := store.AnnounceChange(ctx, id, "main.go", "refactored init")
	if err != nil {
		t.Fatalf("AnnounceChange: %v", err)
	}
	if changeID == 0 {
		t.Fatal("expected non-zero change ID")
	}

	changes, err := store.GetChangesSince(ctx, nil, nil)
	if err != nil {
		t.Fatalf("GetChangesSince: %v", err)
	}
	if len(changes) == 0 {
		t.Fatal("expected at least 1 change")
	}
	if changes[0].Summary != "refactored init" {
		t.Errorf("summary = %q, want %q", changes[0].Summary, "refactored init")
	}
}

func TestGetChangesSince_Filters(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	id1, err := store.RegisterAgent(ctx, "a1", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent 1: %v", err)
	}
	id2, err := store.RegisterAgent(ctx, "a2", "reviewer")
	if err != nil {
		t.Fatalf("RegisterAgent 2: %v", err)
	}

	if _, err := store.AnnounceChange(ctx, id1, "f1.go", "change by a1"); err != nil {
		t.Fatalf("AnnounceChange a1: %v", err)
	}
	if _, err := store.AnnounceChange(ctx, id2, "f2.go", "change by a2"); err != nil {
		t.Fatalf("AnnounceChange a2: %v", err)
	}

	// Filter by agent.
	changes, err := store.GetChangesSince(ctx, nil, &id1)
	if err != nil {
		t.Fatalf("GetChangesSince by agent: %v", err)
	}
	for _, c := range changes {
		if c.AgentID != id1 {
			t.Errorf("expected agentID %q, got %q", id1, c.AgentID)
		}
	}

	// Filter by time — future cutoff yields zero results.
	future := time.Now().Add(1 * time.Hour)
	changes, err = store.GetChangesSince(ctx, &future, nil)
	if err != nil {
		t.Fatalf("GetChangesSince with future: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("expected 0 changes after future cutoff, got %d", len(changes))
	}
}
