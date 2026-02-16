package agentmail

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAnnounceAndGetChanges_Changes(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	// Register an agent directly in the store (prerequisite for announcing).
	agentID, err := store.RegisterAgent(ctx, "change-announcer", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Announce a change via the MCP tool.
	result := callTool(t, cs, "announce_change", map[string]any{
		"agent_id":  agentID,
		"file_path": "internal/loop/loop.go",
		"summary":   "refactored state machine transitions",
	})
	if result.IsError {
		t.Fatalf("announce_change returned error: %v", result.Content)
	}

	// Parse structured output to get change_id.
	var announceOut announceChangeOutput
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	if err := json.Unmarshal(raw, &announceOut); err != nil {
		t.Fatalf("unmarshal announceChangeOutput: %v", err)
	}
	if announceOut.ChangeID == 0 {
		t.Fatal("expected non-zero change_id")
	}

	// Retrieve changes via the MCP tool.
	getResult := callTool(t, cs, "get_changes", map[string]any{})
	if getResult.IsError {
		t.Fatalf("get_changes returned error: %v", getResult.Content)
	}

	var getOut getChangesOutput
	raw, err = json.Marshal(getResult.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	if err := json.Unmarshal(raw, &getOut); err != nil {
		t.Fatalf("unmarshal getChangesOutput: %v", err)
	}

	found := false
	for _, c := range getOut.Changes {
		if c.ID == announceOut.ChangeID {
			found = true
			if c.FilePath != "internal/loop/loop.go" {
				t.Errorf("file_path = %q, want %q", c.FilePath, "internal/loop/loop.go")
			}
			if c.Summary != "refactored state machine transitions" {
				t.Errorf("summary = %q, want %q", c.Summary, "refactored state machine transitions")
			}
			if c.AgentID != agentID {
				t.Errorf("agent_id = %q, want %q", c.AgentID, agentID)
			}
			// Verify announced_at is valid RFC 3339.
			if _, err := time.Parse(time.RFC3339, c.AnnouncedAt); err != nil {
				t.Errorf("announced_at %q is not valid RFC 3339: %v", c.AnnouncedAt, err)
			}
		}
	}
	if !found {
		t.Errorf("announced change %d not found in get_changes results", announceOut.ChangeID)
	}
}

func TestSinceFiltering_Changes(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	agentID, err := store.RegisterAgent(ctx, "since-change", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Announce a change via the MCP tool.
	callTool(t, cs, "announce_change", map[string]any{
		"agent_id":  agentID,
		"file_path": "cmd/run.go",
		"summary":   "older change",
	})

	// Get changes with a future since timestamp — should get zero results.
	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	result := callTool(t, cs, "get_changes", map[string]any{
		"since": future,
	})
	var getOut getChangesOutput
	raw, _ := json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(raw, &getOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(getOut.Changes) != 0 {
		t.Errorf("expected 0 changes after future cutoff, got %d", len(getOut.Changes))
	}

	// Get changes with a past since timestamp — should include the change.
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	result = callTool(t, cs, "get_changes", map[string]any{
		"since": past,
	})
	raw, _ = json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(raw, &getOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(getOut.Changes) == 0 {
		t.Error("expected at least 1 change with past since")
	}
}

func TestAgentIDFiltering_Changes(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	agent1, err := store.RegisterAgent(ctx, "agent-alpha", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent 1: %v", err)
	}
	agent2, err := store.RegisterAgent(ctx, "agent-beta", "reviewer")
	if err != nil {
		t.Fatalf("RegisterAgent 2: %v", err)
	}

	// Both agents announce changes.
	callTool(t, cs, "announce_change", map[string]any{
		"agent_id":  agent1,
		"file_path": "a.go",
		"summary":   "change by alpha",
	})
	callTool(t, cs, "announce_change", map[string]any{
		"agent_id":  agent2,
		"file_path": "b.go",
		"summary":   "change by beta",
	})

	// Filter by agent1.
	result := callTool(t, cs, "get_changes", map[string]any{
		"agent_id": agent1,
	})
	var getOut getChangesOutput
	raw, _ := json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(raw, &getOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(getOut.Changes) == 0 {
		t.Fatal("expected at least 1 change for agent1")
	}
	for _, c := range getOut.Changes {
		if c.AgentID != agent1 {
			t.Errorf("expected agent_id %q, got %q", agent1, c.AgentID)
		}
	}
}

func TestAnnounceChange_UnregisteredAgent(t *testing.T) {
	store := testStore(t)
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	// Announcing from an unregistered agent should return an error.
	result := callTool(t, cs, "announce_change", map[string]any{
		"agent_id":  "nonexistent-agent-id",
		"file_path": "foo.go",
		"summary":   "should fail",
	})
	if !result.IsError {
		t.Fatal("expected IsError=true for unregistered agent, got false")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "not registered") {
		t.Errorf("expected 'not registered' in error, got: %s", text)
	}
}

func TestAnnounceChange_ValidationErrors(t *testing.T) {
	store := testStore(t)
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "missing agent_id",
			args: map[string]any{"file_path": "f.go", "summary": "s"},
			want: "agent_id is required",
		},
		{
			name: "missing file_path",
			args: map[string]any{"agent_id": "a", "summary": "s"},
			want: "file_path is required",
		},
		{
			name: "missing summary",
			args: map[string]any{"agent_id": "a", "file_path": "f.go"},
			want: "summary is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := callTool(t, cs, "announce_change", tc.args)
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

func TestGetChanges_InvalidSince(t *testing.T) {
	store := testStore(t)
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	result := callTool(t, cs, "get_changes", map[string]any{
		"since": "not-a-timestamp",
	})
	if !result.IsError {
		t.Fatal("expected IsError=true for invalid since")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "parsing since timestamp") {
		t.Errorf("expected 'parsing since timestamp' in error, got: %s", text)
	}
}
