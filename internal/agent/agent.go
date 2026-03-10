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
	Role            Role
	SystemPrompt    string
	Model           string
	MaxBudgetUSD    float64
	AllowedTools    []string   // Tool permissions for this agent (passed as --allowedTools flags)
	MCP             *MCPConfig // Optional MCP server configuration
	ResumeSessionID string     // When set, passes --resume <id> to resume a prior session.
	Effort          string     // "low", "medium", "high", or "" (Claude's default).
	FallbackModel   string     // Automatic fallback model when primary is overloaded.
}

// InvocationResult holds the output and cost metrics from a single agent invocation.
type InvocationResult struct {
	ResultText       string
	CostUSD          float64
	DurationMs       int64
	TotalTokens      int    // Combined input + output tokens consumed by this invocation.
	SessionID        string
	SystemPromptLen  int    // Length of the system prompt in bytes.
	UserPromptLen    int    // Length of the user prompt in bytes.
	SystemPromptHash string // SHA-256 hex digest of the system prompt for cache identity tracking.
}

// ReviewReport captures structured metadata from the reviewer's REPORT: block.
type ReviewReport struct {
	Satisfaction     string `toml:"satisfaction"` // high, medium, low
	Risk             string `toml:"risk"`         // high, medium, low
	NeedsHumanReview bool   `toml:"needs_human_review"`
	Summary          string `toml:"summary"`
}

// ActivityEvent describes a single tool-use or reasoning step observed during
// an agent invocation. Consumers display these as a rolling status line so
// developers can intuit whether work is on track.
type ActivityEvent struct {
	// Kind classifies the activity: "read", "edit", "bash", "think", etc.
	Kind string
	// Summary is a short human-readable description (e.g. "reading internal/loop/run.go").
	Summary string
}

// ActivityCallback is invoked by the Invoker for each observable tool-use or
// reasoning step during a streaming invocation. Implementations must be safe
// for concurrent use — the callback may be called from a dedicated reader
// goroutine.
type ActivityCallback func(ActivityEvent)

// Invoker abstracts the execution of an agent, allowing different backends
// (e.g. Claude CLI, mocks) to satisfy the interface.
type Invoker interface {
	Invoke(ctx context.Context, agent Agent, prompt string, workDir string) (InvocationResult, error)
	Validate() error
}
