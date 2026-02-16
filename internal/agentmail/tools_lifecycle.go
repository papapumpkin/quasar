package agentmail

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerInput is the input schema for the register tool.
type registerInput struct {
	Name string `json:"name" jsonschema:"description=Human-readable agent name (e.g. coder-1)"`
	Role string `json:"role" jsonschema:"description=Agent role (e.g. coder or reviewer)"`
}

// registerOutput is the output schema for the register tool.
type registerOutput struct {
	AgentID string `json:"agent_id"`
}

// heartbeatInput is the input schema for the heartbeat tool.
type heartbeatInput struct {
	AgentID string `json:"agent_id" jsonschema:"description=The agent's ID"`
}

// heartbeatOutput is the output schema for the heartbeat tool.
type heartbeatOutput struct {
	OK bool `json:"ok"`
}

// registerLifecycleTools registers the register and heartbeat MCP tools.
func (s *Server) registerLifecycleTools() {
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "register",
		Description: "Register an agent with the coordination server",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input registerInput) (*mcp.CallToolResult, registerOutput, error) {
		if input.Name == "" {
			return nil, registerOutput{}, fmt.Errorf("name is required")
		}
		if input.Role == "" {
			return nil, registerOutput{}, fmt.Errorf("role is required")
		}

		agentID, err := s.store.RegisterAgent(ctx, input.Name, input.Role)
		if err != nil {
			return nil, registerOutput{}, fmt.Errorf("registering agent: %w", err)
		}

		return nil, registerOutput{AgentID: agentID}, nil
	})

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "heartbeat",
		Description: "Send a heartbeat to indicate agent liveness",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input heartbeatInput) (*mcp.CallToolResult, heartbeatOutput, error) {
		if input.AgentID == "" {
			return nil, heartbeatOutput{}, fmt.Errorf("agent_id is required")
		}

		if err := s.store.Heartbeat(ctx, input.AgentID); err != nil {
			return nil, heartbeatOutput{}, fmt.Errorf("heartbeat: %w", err)
		}

		return nil, heartbeatOutput{OK: true}, nil
	})
}
