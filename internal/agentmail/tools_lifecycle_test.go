package agentmail

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRegister_ReturnsValidUUID(t *testing.T) {
	store := testStore(t)
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	result := callTool(t, cs, "register", map[string]any{
		"name": "coder-1",
		"role": "coder",
	})
	if result.IsError {
		t.Fatalf("register returned error: %v", result.Content)
	}

	var out registerOutput
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal registerOutput: %v", err)
	}

	if out.AgentID == "" {
		t.Fatal("expected non-empty agent_id")
	}

	// Validate UUID v4 format: 8-4-4-4-12 hex characters.
	parts := strings.Split(out.AgentID, "-")
	if len(parts) != 5 {
		t.Fatalf("agent_id %q is not a valid UUID (expected 5 dash-separated parts)", out.AgentID)
	}
	expectedLens := []int{8, 4, 4, 4, 12}
	for i, p := range parts {
		if len(p) != expectedLens[i] {
			t.Errorf("UUID part %d has length %d, want %d (full UUID: %q)", i, len(p), expectedLens[i], out.AgentID)
		}
	}
}

func TestRegister_ValidationErrors(t *testing.T) {
	store := testStore(t)
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "missing name",
			args: map[string]any{"role": "coder"},
			want: "name is required",
		},
		{
			name: "missing role",
			args: map[string]any{"name": "coder-1"},
			want: "role is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := callTool(t, cs, "register", tc.args)
			if !result.IsError {
				t.Fatal("expected IsError=true")
			}
			text := result.Content[0].(*mcp.TextContent).Text
			if !strings.Contains(text, tc.want) {
				t.Errorf("expected %q in error, got: %s", tc.want, text)
			}
		})
	}
}

func TestHeartbeat_UpdatesTimestamp(t *testing.T) {
	store := testStore(t)
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	// Register via the MCP tool.
	regResult := callTool(t, cs, "register", map[string]any{
		"name": "heartbeat-agent",
		"role": "coder",
	})
	var regOut registerOutput
	raw, _ := json.Marshal(regResult.StructuredContent)
	if err := json.Unmarshal(raw, &regOut); err != nil {
		t.Fatalf("unmarshal registerOutput: %v", err)
	}

	// Small delay so the heartbeat timestamp will differ from registration.
	time.Sleep(50 * time.Millisecond)

	// Send heartbeat via the MCP tool.
	hbResult := callTool(t, cs, "heartbeat", map[string]any{
		"agent_id": regOut.AgentID,
	})
	if hbResult.IsError {
		t.Fatalf("heartbeat returned error: %v", hbResult.Content)
	}

	var hbOut heartbeatOutput
	raw, _ = json.Marshal(hbResult.StructuredContent)
	if err := json.Unmarshal(raw, &hbOut); err != nil {
		t.Fatalf("unmarshal heartbeatOutput: %v", err)
	}
	if !hbOut.OK {
		t.Error("expected ok=true from heartbeat")
	}

	// Verify the agent is not stale (heartbeat was recent).
	ctx := context.Background()
	stale, err := store.FindStaleAgents(ctx, 10*time.Second)
	if err != nil {
		t.Fatalf("FindStaleAgents: %v", err)
	}
	for _, a := range stale {
		if a.ID == regOut.AgentID {
			t.Error("agent should not be stale after heartbeat")
		}
	}
}

func TestHeartbeat_InvalidID(t *testing.T) {
	store := testStore(t)
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	result := callTool(t, cs, "heartbeat", map[string]any{
		"agent_id": "nonexistent-agent-id",
	})
	if !result.IsError {
		t.Fatal("expected IsError=true for nonexistent agent")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "not found") {
		t.Errorf("expected 'not found' in error, got: %s", text)
	}
}

func TestHeartbeat_MissingAgentID(t *testing.T) {
	store := testStore(t)
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	result := callTool(t, cs, "heartbeat", map[string]any{})
	if !result.IsError {
		t.Fatal("expected IsError=true for missing agent_id")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "agent_id is required") {
		t.Errorf("expected 'agent_id is required' in error, got: %s", text)
	}
}

func TestStaleCleanup_ReleasesClaims(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	// Use a very short stale timeout so we can trigger cleanup quickly.
	cfg := &ServerConfig{
		CleanupInterval: 100 * time.Millisecond,
		StaleTimeout:    1 * time.Millisecond,
	}
	srv := NewServer(store, 0, cfg)

	// Register an agent directly and claim files.
	agentID, err := store.RegisterAgent(ctx, "stale-agent", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	claimed, _, err := store.ClaimFiles(ctx, agentID, []string{"main.go", "util.go"})
	if err != nil {
		t.Fatalf("ClaimFiles: %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("expected 2 claimed files, got %d", len(claimed))
	}

	// Wait for the agent to become stale, then run cleanup directly.
	time.Sleep(50 * time.Millisecond)
	srv.runCleanup(ctx)

	// Verify the agent is gone.
	exists, err := store.AgentExists(ctx, agentID)
	if err != nil {
		t.Fatalf("AgentExists: %v", err)
	}
	if exists {
		t.Error("expected agent to be deleted after cleanup")
	}

	// Verify file claims were released by claiming them with a new agent.
	newAgent, err := store.RegisterAgent(ctx, "new-agent", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent new: %v", err)
	}
	reclaimed, conflicts, err := store.ClaimFiles(ctx, newAgent, []string{"main.go", "util.go"})
	if err != nil {
		t.Fatalf("ClaimFiles reclaim: %v", err)
	}
	if len(conflicts) != 0 {
		t.Errorf("expected no conflicts after cleanup, got %v", conflicts)
	}
	if len(reclaimed) != 2 {
		t.Errorf("expected 2 reclaimed files, got %d", len(reclaimed))
	}
}

func TestStaleCleanup_SendsBroadcast(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	cfg := &ServerConfig{
		CleanupInterval: 100 * time.Millisecond,
		StaleTimeout:    1 * time.Millisecond,
	}
	srv := NewServer(store, 0, cfg)

	// Register an agent that will go stale.
	if _, err := store.RegisterAgent(ctx, "stale-broadcaster", "reviewer"); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Wait for the first agent to become stale, then run cleanup.
	time.Sleep(50 * time.Millisecond)
	srv.runCleanup(ctx)

	// Read broadcast messages — should contain the stale agent notification.
	channel := "broadcast"
	msgs, err := store.ReadMessages(ctx, nil, &channel)
	if err != nil {
		t.Fatalf("ReadMessages: %v", err)
	}

	found := false
	for _, m := range msgs {
		if strings.Contains(m.Body, "stale-broadcaster") &&
			strings.Contains(m.Body, "reviewer") &&
			strings.Contains(m.Body, "went stale") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected broadcast message about stale agent removal")
	}
}
