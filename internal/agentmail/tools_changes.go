package agentmail

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// announceChangeInput is the input schema for the announce_change tool.
type announceChangeInput struct {
	AgentID  string `json:"agent_id" jsonschema:"Agent announcing the change"`
	FilePath string `json:"file_path" jsonschema:"Path of the modified file"`
	Summary  string `json:"summary" jsonschema:"Human-readable description of the change"`
}

// announceChangeOutput is the output schema for the announce_change tool.
type announceChangeOutput struct {
	ChangeID int64 `json:"change_id"`
}

// getChangesInput is the input schema for the get_changes tool.
type getChangesInput struct {
	Since   string `json:"since,omitempty" jsonschema:"ISO 8601 timestamp — only changes after this"`
	AgentID string `json:"agent_id,omitempty" jsonschema:"Filter to changes by a specific agent"`
}

// changeEntry is a single change in the get_changes response.
type changeEntry struct {
	ID          int64  `json:"id"`
	AgentID     string `json:"agent_id"`
	FilePath    string `json:"file_path"`
	Summary     string `json:"summary"`
	AnnouncedAt string `json:"announced_at"`
}

// getChangesOutput is the output schema for the get_changes tool.
type getChangesOutput struct {
	Changes []changeEntry `json:"changes"`
}

// registerChangeTools registers the announce_change and get_changes MCP tools.
func (s *Server) registerChangeTools() {
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "announce_change",
		Description: "Announce a file change to other agents",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input announceChangeInput) (*mcp.CallToolResult, announceChangeOutput, error) {
		if input.AgentID == "" {
			return nil, announceChangeOutput{}, fmt.Errorf("agent_id is required")
		}
		if input.FilePath == "" {
			return nil, announceChangeOutput{}, fmt.Errorf("file_path is required")
		}
		if input.Summary == "" {
			return nil, announceChangeOutput{}, fmt.Errorf("summary is required")
		}

		// Verify the announcing agent is registered.
		exists, err := s.store.AgentExists(ctx, input.AgentID)
		if err != nil {
			return nil, announceChangeOutput{}, fmt.Errorf("checking agent: %w", err)
		}
		if !exists {
			return nil, announceChangeOutput{}, fmt.Errorf("agent %q is not registered", input.AgentID)
		}

		changeID, err := s.store.AnnounceChange(ctx, input.AgentID, input.FilePath, input.Summary)
		if err != nil {
			return nil, announceChangeOutput{}, fmt.Errorf("announcing change: %w", err)
		}

		return nil, announceChangeOutput{ChangeID: changeID}, nil
	})

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "get_changes",
		Description: "Get recent change announcements",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getChangesInput) (*mcp.CallToolResult, getChangesOutput, error) {
		var since *time.Time
		if input.Since != "" {
			t, err := time.Parse(time.RFC3339, input.Since)
			if err != nil {
				return nil, getChangesOutput{}, fmt.Errorf("parsing since timestamp: %w", err)
			}
			since = &t
		}

		var agentID *string
		if input.AgentID != "" {
			agentID = &input.AgentID
		}

		changes, err := s.store.GetChangesSince(ctx, since, agentID)
		if err != nil {
			return nil, getChangesOutput{}, fmt.Errorf("getting changes: %w", err)
		}

		entries := make([]changeEntry, len(changes))
		for i, c := range changes {
			entries[i] = changeEntry{
				ID:          c.ID,
				AgentID:     c.AgentID,
				FilePath:    c.FilePath,
				Summary:     c.Summary,
				AnnouncedAt: c.AnnouncedAt.Format(time.RFC3339),
			}
		}

		return nil, getChangesOutput{Changes: entries}, nil
	})
}
