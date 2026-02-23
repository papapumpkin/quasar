package agent

import "context"

// Role identifies the function an agent plays in the coder-reviewer loop.
type Role string

const (
	// RoleCoder is the agent role that writes and modifies code.
	RoleCoder Role = "coder"
	// RoleReviewer is the agent role that reviews code changes.
	RoleReviewer Role = "reviewer"
	// RoleArchitect is the agent role that creates and refactors nebula phase files.
	RoleArchitect Role = "architect"
)

// MCPConfig holds optional MCP server configuration for an agent invocation.
type MCPConfig struct {
	ConfigPath string // path to generated MCP config JSON
}

// Agent describes the configuration for a single agent invocation.
type Agent struct {
	Role          Role
	SystemPrompt  string
	ContextPrefix string // Project context prepended to SystemPrompt for cache-friendly prefixing.
	Model         string
	MaxBudgetUSD  float64
	AllowedTools  []string   // Tool permissions for this agent (passed as --allowedTools flags)
	MCP           *MCPConfig // Optional MCP server configuration
}

// InvocationResult holds the output and cost metrics from a single agent invocation.
type InvocationResult struct {
	ResultText string
	CostUSD    float64
	DurationMs int64
	SessionID  string
}

// ReviewReport captures structured metadata from the reviewer's REPORT: block.
type ReviewReport struct {
	Satisfaction     string `toml:"satisfaction"` // high, medium, low
	Risk             string `toml:"risk"`         // high, medium, low
	NeedsHumanReview bool   `toml:"needs_human_review"`
	Summary          string `toml:"summary"`
}

// Invoker abstracts the execution of an agent, allowing different backends
// (e.g. Claude CLI, mocks) to satisfy the interface.
type Invoker interface {
	Invoke(ctx context.Context, agent Agent, prompt string, workDir string) (InvocationResult, error)
	Validate() error
}
