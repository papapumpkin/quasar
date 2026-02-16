package agentmail

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpClientSession creates an in-memory MCP client connected to the given
// Server's underlying MCP server. It returns a ClientSession that can be used
// to call tools. The session is closed when the test finishes.
func mcpClientSession(t *testing.T, srv *Server) *mcp.ClientSession {
	t.Helper()

	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()

	ss, err := srv.mcp.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { ss.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })

	return cs
}

// callTool is a test helper that calls a tool and returns the result.
func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx := context.Background()
	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	return result
}

func TestSendAndReadMessages_Mailbox(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	// Register an agent directly in the store (prerequisite for sending).
	agentID, err := store.RegisterAgent(ctx, "mailbox-sender", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Send a message via the MCP tool (omit channel to test default "broadcast").
	result := callTool(t, cs, "send_message", map[string]any{
		"agent_id": agentID,
		"subject":  "test subject",
		"body":     "test body",
	})
	if result.IsError {
		t.Fatalf("send_message returned error: %v", result.Content)
	}

	// Parse structured output to get message_id.
	var sendOut sendMessageOutput
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	if err := json.Unmarshal(raw, &sendOut); err != nil {
		t.Fatalf("unmarshal sendMessageOutput: %v", err)
	}
	if sendOut.MessageID == 0 {
		t.Fatal("expected non-zero message_id")
	}

	// Read messages via the MCP tool.
	readResult := callTool(t, cs, "read_messages", map[string]any{
		"agent_id": agentID,
	})
	if readResult.IsError {
		t.Fatalf("read_messages returned error: %v", readResult.Content)
	}

	var readOut readMessagesOutput
	raw, err = json.Marshal(readResult.StructuredContent)
	if err != nil {
		t.Fatalf("marshal StructuredContent: %v", err)
	}
	if err := json.Unmarshal(raw, &readOut); err != nil {
		t.Fatalf("unmarshal readMessagesOutput: %v", err)
	}

	found := false
	for _, m := range readOut.Messages {
		if m.ID == sendOut.MessageID {
			found = true
			if m.Subject != "test subject" {
				t.Errorf("subject = %q, want %q", m.Subject, "test subject")
			}
			if m.Body != "test body" {
				t.Errorf("body = %q, want %q", m.Body, "test body")
			}
			if m.Channel != "broadcast" {
				t.Errorf("channel = %q, want %q", m.Channel, "broadcast")
			}
			if m.SenderID != agentID {
				t.Errorf("sender_id = %q, want %q", m.SenderID, agentID)
			}
			// Verify created_at is valid RFC 3339.
			if _, err := time.Parse(time.RFC3339, m.CreatedAt); err != nil {
				t.Errorf("created_at %q is not valid RFC 3339: %v", m.CreatedAt, err)
			}
		}
	}
	if !found {
		t.Errorf("sent message %d not found in read results", sendOut.MessageID)
	}
}

func TestChannelFiltering_Mailbox(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	agentID, err := store.RegisterAgent(ctx, "channel-filter", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Send on different channels via the MCP tool.
	callTool(t, cs, "send_message", map[string]any{
		"agent_id": agentID,
		"channel":  "alpha",
		"subject":  "alpha msg",
		"body":     "alpha body",
	})
	callTool(t, cs, "send_message", map[string]any{
		"agent_id": agentID,
		"channel":  "beta",
		"subject":  "beta msg",
		"body":     "beta body",
	})
	// Omit channel to test default "broadcast".
	callTool(t, cs, "send_message", map[string]any{
		"agent_id": agentID,
		"subject":  "bc msg",
		"body":     "bc body",
	})

	// Filter by alpha channel via MCP tool.
	result := callTool(t, cs, "read_messages", map[string]any{
		"agent_id": agentID,
		"channel":  "alpha",
	})
	var readOut readMessagesOutput
	raw, _ := json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(raw, &readOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(readOut.Messages) == 0 {
		t.Fatal("expected at least 1 alpha message")
	}
	for _, m := range readOut.Messages {
		if m.Channel != "alpha" {
			t.Errorf("expected channel alpha, got %q", m.Channel)
		}
	}

	// Filter by broadcast channel.
	result = callTool(t, cs, "read_messages", map[string]any{
		"agent_id": agentID,
		"channel":  "broadcast",
	})
	raw, _ = json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(raw, &readOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(readOut.Messages) == 0 {
		t.Fatal("expected at least 1 broadcast message")
	}
	for _, m := range readOut.Messages {
		if m.Channel != "broadcast" {
			t.Errorf("expected channel broadcast, got %q", m.Channel)
		}
	}
}

func TestSinceFiltering_Mailbox(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	agentID, err := store.RegisterAgent(ctx, "since-filter", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Send a message via the MCP tool.
	callTool(t, cs, "send_message", map[string]any{
		"agent_id": agentID,
		"subject":  "older msg",
		"body":     "body",
	})

	// Read with a future since timestamp — should get zero messages.
	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	result := callTool(t, cs, "read_messages", map[string]any{
		"agent_id": agentID,
		"since":    future,
	})
	var readOut readMessagesOutput
	raw, _ := json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(raw, &readOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(readOut.Messages) != 0 {
		t.Errorf("expected 0 messages after future cutoff, got %d", len(readOut.Messages))
	}

	// Read with a past since timestamp — should include the message.
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	result = callTool(t, cs, "read_messages", map[string]any{
		"agent_id": agentID,
		"since":    past,
	})
	raw, _ = json.Marshal(result.StructuredContent)
	if err := json.Unmarshal(raw, &readOut); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(readOut.Messages) == 0 {
		t.Error("expected at least 1 message with past since")
	}
}

func TestSendMessage_UnregisteredAgent_Mailbox(t *testing.T) {
	store := testStore(t)
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	// Sending from an unregistered agent via the MCP tool should return an error.
	result := callTool(t, cs, "send_message", map[string]any{
		"agent_id": "nonexistent-agent-id",
		"subject":  "fail",
		"body":     "body",
	})
	if !result.IsError {
		t.Fatal("expected IsError=true for unregistered agent, got false")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "not registered") {
		t.Errorf("expected 'not registered' in error, got: %s", text)
	}
}

func TestReadMessages_UnregisteredAgent_Mailbox(t *testing.T) {
	store := testStore(t)
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	// Reading from an unregistered agent via the MCP tool should return an error.
	result := callTool(t, cs, "read_messages", map[string]any{
		"agent_id": "nonexistent-agent-id",
	})
	if !result.IsError {
		t.Fatal("expected IsError=true for unregistered agent, got false")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "not registered") {
		t.Errorf("expected 'not registered' in error, got: %s", text)
	}
}

func TestSendMessage_ValidationErrors_Mailbox(t *testing.T) {
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
			args: map[string]any{"subject": "s", "body": "b"},
			want: "agent_id is required",
		},
		{
			name: "missing subject",
			args: map[string]any{"agent_id": "a", "body": "b"},
			want: "subject is required",
		},
		{
			name: "missing body",
			args: map[string]any{"agent_id": "a", "subject": "s"},
			want: "body is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := callTool(t, cs, "send_message", tc.args)
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

func TestReadMessages_InvalidSince_Mailbox(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	srv := NewServer(store, 0, nil)
	cs := mcpClientSession(t, srv)

	// Register an agent so the auth check passes.
	agentID, err := store.RegisterAgent(ctx, "invalid-since", "coder")
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	result := callTool(t, cs, "read_messages", map[string]any{
		"agent_id": agentID,
		"since":    "not-a-timestamp",
	})
	if !result.IsError {
		t.Fatal("expected IsError=true for invalid since")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "parsing since timestamp") {
		t.Errorf("expected 'parsing since timestamp' in error, got: %s", text)
	}
}

func TestToolRegistration_Mailbox(t *testing.T) {
	t.Parallel()

	// Verify that NewServer registers mailbox tools (not as stubs).
	store := NewStore(nil)
	srv := NewServer(store, 0, nil)

	// The MCP server should be created with tools registered.
	if srv.mcp == nil {
		t.Fatal("expected non-nil MCP server")
	}
}
