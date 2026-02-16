//go:build integration

// Integration tests for the agentmail coordination system.
//
// These tests exercise the full flow: Dolt database, MCP server with cleanup
// goroutine, and multiple simulated agents coordinating via the Store layer.
//
// Prerequisites:
//   - A Dolt (or MySQL-compatible) instance running on localhost:3306
//   - Database "agentmail_test" must exist
//   - Override via AGENTMAIL_TEST_DSN env var if needed
//
// Run:
//
//	go test -tags=integration ./internal/agentmail/...
package agentmail

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestIntegration_TwoAgentFileCoordination exercises the full file-claim
// lifecycle between two agents: claim, conflict, announce, release, re-claim.
func TestIntegration_TwoAgentFileCoordination(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	srv := NewServer(store, 0, nil)
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := srv.Stop(context.Background()); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	// Step 1-2: Register two agents.
	agentA, err := store.RegisterAgent(ctx, "agent-A", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent A: %v", err)
	}
	agentB, err := store.RegisterAgent(ctx, "agent-B", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent B: %v", err)
	}

	// Step 3: Agent-A claims two files.
	claimed, conflicts, err := store.ClaimFiles(ctx, agentA, []string{
		"internal/auth/auth.go",
		"internal/auth/middleware.go",
	})
	if err != nil {
		t.Fatalf("ClaimFiles A: %v", err)
	}
	if len(claimed) != 2 {
		t.Errorf("agent-A claimed %d files, want 2", len(claimed))
	}
	if len(conflicts) != 0 {
		t.Errorf("agent-A got %d conflicts, want 0", len(conflicts))
	}

	// Step 4: Agent-B tries to claim a file held by agent-A — expect conflict.
	claimed, conflicts, err = store.ClaimFiles(ctx, agentB, []string{"internal/auth/auth.go"})
	if err != nil {
		t.Fatalf("ClaimFiles B (conflict): %v", err)
	}
	if len(claimed) != 0 {
		t.Errorf("agent-B claimed %d files, want 0 (conflict expected)", len(claimed))
	}
	if len(conflicts) != 1 {
		t.Errorf("agent-B got %d conflicts, want 1", len(conflicts))
	}

	// Step 5: Agent-B claims a different file — expect success.
	claimed, conflicts, err = store.ClaimFiles(ctx, agentB, []string{"internal/api/handler.go"})
	if err != nil {
		t.Fatalf("ClaimFiles B (new file): %v", err)
	}
	if len(claimed) != 1 {
		t.Errorf("agent-B claimed %d files, want 1", len(claimed))
	}
	if len(conflicts) != 0 {
		t.Errorf("agent-B got %d conflicts on new file, want 0", len(conflicts))
	}

	// Step 6: Agent-A announces a change.
	beforeChange := time.Now().Add(-1 * time.Second)
	changeID, err := store.AnnounceChange(ctx, agentA, "internal/auth/auth.go", "refactored auth middleware")
	if err != nil {
		t.Fatalf("AnnounceChange: %v", err)
	}
	if changeID == 0 {
		t.Fatal("expected non-zero change ID")
	}

	// Step 7: Agent-B calls GetChangesSince — should see agent-A's change.
	changes, err := store.GetChangesSince(ctx, &beforeChange, nil)
	if err != nil {
		t.Fatalf("GetChangesSince: %v", err)
	}
	found := false
	for _, c := range changes {
		if c.AgentID == agentA && c.FilePath == "internal/auth/auth.go" {
			found = true
			break
		}
	}
	if !found {
		t.Error("agent-B did not see agent-A's change in GetChangesSince")
	}

	// Step 8: Agent-A releases files.
	released, err := store.ReleaseFiles(ctx, agentA, []string{
		"internal/auth/auth.go",
		"internal/auth/middleware.go",
	})
	if err != nil {
		t.Fatalf("ReleaseFiles A: %v", err)
	}
	if len(released) != 2 {
		t.Errorf("agent-A released %d files, want 2", len(released))
	}

	// Step 9: Agent-B successfully claims the now-released file.
	claimed, conflicts, err = store.ClaimFiles(ctx, agentB, []string{"internal/auth/auth.go"})
	if err != nil {
		t.Fatalf("ClaimFiles B (after release): %v", err)
	}
	if len(claimed) != 1 {
		t.Errorf("agent-B claimed %d files after release, want 1", len(claimed))
	}
	if len(conflicts) != 0 {
		t.Errorf("agent-B got %d conflicts after release, want 0", len(conflicts))
	}
}

// TestIntegration_MessagePassing exercises broadcast and directed messaging
// between agents, verifying channel isolation.
func TestIntegration_MessagePassing(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	srv := NewServer(store, 0, nil)
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := srv.Stop(context.Background()); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	// Step 1: Register two agents.
	agentA, err := store.RegisterAgent(ctx, "agent-A", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent A: %v", err)
	}
	agentB, err := store.RegisterAgent(ctx, "agent-B", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent B: %v", err)
	}

	beforeMsgs := time.Now().Add(-1 * time.Second)

	// Step 2: Agent-A sends a broadcast message.
	_, err = store.SendMessage(ctx, agentA, "broadcast", "refactoring", "Refactoring auth package")
	if err != nil {
		t.Fatalf("SendMessage broadcast: %v", err)
	}

	// Step 3: Agent-B reads messages — should see the broadcast.
	msgs, err := store.ReadMessages(ctx, &beforeMsgs, nil)
	if err != nil {
		t.Fatalf("ReadMessages (all): %v", err)
	}
	foundBroadcast := false
	for _, m := range msgs {
		if m.SenderID == agentA && m.Channel == "broadcast" && m.Body == "Refactoring auth package" {
			foundBroadcast = true
			break
		}
	}
	if !foundBroadcast {
		t.Error("agent-B did not see agent-A's broadcast message")
	}

	// Step 4: Agent-A sends a directed message to agent-B's channel.
	directedChannel := "agent:" + agentB
	_, err = store.SendMessage(ctx, agentA, directedChannel, "heads-up", "I changed auth.go")
	if err != nil {
		t.Fatalf("SendMessage directed: %v", err)
	}

	// Step 5: Agent-B reads messages filtered by directed channel — sees the directed message.
	msgs, err = store.ReadMessages(ctx, &beforeMsgs, &directedChannel)
	if err != nil {
		t.Fatalf("ReadMessages (directed channel): %v", err)
	}
	foundDirected := false
	for _, m := range msgs {
		if m.SenderID == agentA && m.Channel == directedChannel && m.Body == "I changed auth.go" {
			foundDirected = true
			break
		}
	}
	if !foundDirected {
		t.Error("agent-B did not see directed message on its channel")
	}

	// Step 6: Agent-B reads broadcast channel — should NOT see the directed message.
	broadcastCh := "broadcast"
	msgs, err = store.ReadMessages(ctx, &beforeMsgs, &broadcastCh)
	if err != nil {
		t.Fatalf("ReadMessages (broadcast only): %v", err)
	}
	for _, m := range msgs {
		if m.Channel != "broadcast" {
			t.Errorf("broadcast filter returned message on channel %q", m.Channel)
		}
	}
}

// TestIntegration_StaleAgentCleanup verifies that the server's cleanup
// goroutine releases file claims for agents that stop sending heartbeats.
func TestIntegration_StaleAgentCleanup(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	// Use very short intervals so the test completes quickly.
	cfg := &ServerConfig{
		CleanupInterval: 200 * time.Millisecond,
		StaleTimeout:    100 * time.Millisecond,
	}
	srv := NewServer(store, 0, cfg)
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := srv.Stop(context.Background()); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	// Step 1: Register agent-A and have it claim files.
	agentA, err := store.RegisterAgent(ctx, "stale-test-agent", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	claimed, _, err := store.ClaimFiles(ctx, agentA, []string{"stale/file1.go", "stale/file2.go"})
	if err != nil {
		t.Fatalf("ClaimFiles: %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("expected 2 claimed, got %d", len(claimed))
	}

	// Step 2: Force the agent's heartbeat far into the past (simulate no heartbeat).
	_, err = store.db.ExecContext(ctx,
		"UPDATE agents SET last_heartbeat = '2000-01-01 00:00:00' WHERE id = ?", agentA)
	if err != nil {
		t.Fatalf("setting stale heartbeat: %v", err)
	}

	// Step 3: Wait for the cleanup loop to run.
	// CleanupInterval is 200ms, so wait up to 2s for safety.
	deadline := time.Now().Add(2 * time.Second)
	var claimsRemain bool
	for time.Now().Before(deadline) {
		claims, err := store.GetFileClaims(ctx, []string{"stale/file1.go", "stale/file2.go"})
		if err != nil {
			t.Fatalf("GetFileClaims: %v", err)
		}
		if len(claims) == 0 {
			claimsRemain = false
			break
		}
		claimsRemain = true
		time.Sleep(100 * time.Millisecond)
	}

	// Step 4: Verify agent-A's claims are released.
	if claimsRemain {
		t.Error("stale agent's file claims were not released after cleanup")
	}

	// Step 5: Verify a broadcast message announces the cleanup.
	broadcastCh := "broadcast"
	msgs, err := store.ReadMessages(ctx, nil, &broadcastCh)
	if err != nil {
		t.Fatalf("ReadMessages: %v", err)
	}
	foundCleanupMsg := false
	for _, m := range msgs {
		if m.Subject == "agent-stale" && strings.Contains(m.Body, "stale-test-agent") {
			foundCleanupMsg = true
			break
		}
	}
	if !foundCleanupMsg {
		t.Error("expected broadcast message about stale agent cleanup, not found")
	}
}

// TestIntegration_ConcurrentClaimRace verifies that when two agents
// simultaneously attempt to claim the same file, exactly one succeeds.
func TestIntegration_ConcurrentClaimRace(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	srv := NewServer(store, 0, nil)
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if err := srv.Stop(context.Background()); err != nil {
			t.Errorf("Stop: %v", err)
		}
	}()

	// Register two agents.
	agentA, err := store.RegisterAgent(ctx, "racer-A", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent A: %v", err)
	}
	agentB, err := store.RegisterAgent(ctx, "racer-B", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent B: %v", err)
	}

	targetFile := "race/contested.go"

	type result struct {
		claimed   []string
		conflicts []string
		err       error
	}

	var (
		wg      sync.WaitGroup
		resultA result
		resultB result
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		resultA.claimed, resultA.conflicts, resultA.err = store.ClaimFiles(ctx, agentA, []string{targetFile})
	}()
	go func() {
		defer wg.Done()
		resultB.claimed, resultB.conflicts, resultB.err = store.ClaimFiles(ctx, agentB, []string{targetFile})
	}()
	wg.Wait()

	// Both should succeed without errors.
	if resultA.err != nil {
		t.Fatalf("agent-A ClaimFiles error: %v", resultA.err)
	}
	if resultB.err != nil {
		t.Fatalf("agent-B ClaimFiles error: %v", resultB.err)
	}

	aClaimed := len(resultA.claimed)
	bClaimed := len(resultB.claimed)
	aConflicts := len(resultA.conflicts)
	bConflicts := len(resultB.conflicts)

	// Exactly one should have claimed the file, the other should have a conflict.
	totalClaimed := aClaimed + bClaimed
	totalConflicts := aConflicts + bConflicts

	if totalClaimed != 1 {
		t.Errorf("expected exactly 1 total claim, got %d (A=%d, B=%d)", totalClaimed, aClaimed, bClaimed)
	}
	if totalConflicts != 1 {
		t.Errorf("expected exactly 1 total conflict, got %d (A=%d, B=%d)", totalConflicts, aConflicts, bConflicts)
	}

	// Verify the file is claimed by exactly one agent in the database.
	claims, err := store.GetFileClaims(ctx, []string{targetFile})
	if err != nil {
		t.Fatalf("GetFileClaims: %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("expected 1 file claim in DB, got %d", len(claims))
	}
	holder := claims[0].AgentID
	if holder != agentA && holder != agentB {
		t.Errorf("unexpected claim holder: %q", holder)
	}
}
