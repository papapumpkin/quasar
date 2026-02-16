package agentmail

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// sendMessageInput is the input schema for the send_message tool.
type sendMessageInput struct {
	AgentID string `json:"agent_id" jsonschema:"description=Sender's agent ID"`
	Channel string `json:"channel,omitempty" jsonschema:"description=Target channel (default: broadcast)"`
	Subject string `json:"subject" jsonschema:"description=Message subject"`
	Body    string `json:"body" jsonschema:"description=Message body"`
}

// sendMessageOutput is the output schema for the send_message tool.
type sendMessageOutput struct {
	MessageID int64 `json:"message_id"`
}

// readMessagesInput is the input schema for the read_messages tool.
type readMessagesInput struct {
	AgentID string `json:"agent_id" jsonschema:"description=Requesting agent ID (for auth)"`
	Since   string `json:"since,omitempty" jsonschema:"description=ISO 8601 timestamp — only return messages after this"`
	Channel string `json:"channel,omitempty" jsonschema:"description=Filter to specific channel"`
}

// messageEntry is a single message in the read_messages response.
type messageEntry struct {
	ID        int64  `json:"id"`
	SenderID  string `json:"sender_id"`
	Channel   string `json:"channel"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

// readMessagesOutput is the output schema for the read_messages tool.
type readMessagesOutput struct {
	Messages []messageEntry `json:"messages"`
}

// registerMailboxTools registers the send_message and read_messages MCP tools.
func (s *Server) registerMailboxTools() {
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "send_message",
		Description: "Send a message to other agents",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input sendMessageInput) (*mcp.CallToolResult, sendMessageOutput, error) {
		if input.AgentID == "" {
			return nil, sendMessageOutput{}, fmt.Errorf("agent_id is required")
		}
		if input.Subject == "" {
			return nil, sendMessageOutput{}, fmt.Errorf("subject is required")
		}
		if input.Body == "" {
			return nil, sendMessageOutput{}, fmt.Errorf("body is required")
		}

		// Verify the sender is a registered agent.
		exists, err := s.store.AgentExists(ctx, input.AgentID)
		if err != nil {
			return nil, sendMessageOutput{}, fmt.Errorf("checking agent: %w", err)
		}
		if !exists {
			return nil, sendMessageOutput{}, fmt.Errorf("agent %q is not registered", input.AgentID)
		}

		channel := input.Channel
		if channel == "" {
			channel = "broadcast"
		}

		msgID, err := s.store.SendMessage(ctx, input.AgentID, channel, input.Subject, input.Body)
		if err != nil {
			return nil, sendMessageOutput{}, fmt.Errorf("sending message: %w", err)
		}

		return nil, sendMessageOutput{MessageID: msgID}, nil
	})

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "read_messages",
		Description: "Read messages from the coordination channel",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input readMessagesInput) (*mcp.CallToolResult, readMessagesOutput, error) {
		if input.AgentID == "" {
			return nil, readMessagesOutput{}, fmt.Errorf("agent_id is required")
		}

		// Verify the requesting agent is registered.
		exists, err := s.store.AgentExists(ctx, input.AgentID)
		if err != nil {
			return nil, readMessagesOutput{}, fmt.Errorf("checking agent: %w", err)
		}
		if !exists {
			return nil, readMessagesOutput{}, fmt.Errorf("agent %q is not registered", input.AgentID)
		}

		var since *time.Time
		if input.Since != "" {
			t, err := time.Parse(time.RFC3339, input.Since)
			if err != nil {
				return nil, readMessagesOutput{}, fmt.Errorf("parsing since timestamp: %w", err)
			}
			since = &t
		}

		var channel *string
		if input.Channel != "" {
			channel = &input.Channel
		}

		msgs, err := s.store.ReadMessages(ctx, since, channel)
		if err != nil {
			return nil, readMessagesOutput{}, fmt.Errorf("reading messages: %w", err)
		}

		entries := make([]messageEntry, len(msgs))
		for i, m := range msgs {
			entries[i] = messageEntry{
				ID:        m.ID,
				SenderID:  m.SenderID,
				Channel:   m.Channel,
				Subject:   m.Subject,
				Body:      m.Body,
				CreatedAt: m.CreatedAt.Format(time.RFC3339),
			}
		}

		return nil, readMessagesOutput{Messages: entries}, nil
	})
}
