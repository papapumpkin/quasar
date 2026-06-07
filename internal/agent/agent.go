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
	// CacheOptimization, when true, instructs the invoker to pass
	// --exclude-dynamic-system-prompt-sections so the system-prompt prefix stays
	// byte-stable across invocations and remains eligible for prompt caching.
	CacheOptimization bool
}

// InvocationResult holds the output and cost metrics from a single agent invocation.
type InvocationResult struct {
	ResultText       string
	CostUSD          float64
	DurationMs       int64
	SessionID        string
	SystemPromptLen  int    // Length of the system prompt in bytes.
	UserPromptLen    int    // Length of the user prompt in bytes.
	SystemPromptHash string // SHA-256 hex digest of the system prompt for cache identity tracking.

	// InputTokens is the count of fresh (uncached) input tokens billed at full
	// rate for this invocation, as reported by the model's usage block.
	InputTokens int
	// CacheCreationTokens is the count of input tokens written into the prompt
	// cache on this invocation (billed at the cache-write rate).
	CacheCreationTokens int
	// CacheReadTokens is the count of input tokens served from the prompt cache
	// on this invocation (billed at the discounted cache-read rate). A non-zero
	// value on a repeat invocation is the signal that caching is working.
	CacheReadTokens int
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
