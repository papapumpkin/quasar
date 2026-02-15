package agent

import "context"

// Role identifies the function an agent plays in the coder-reviewer loop.
type Role string

const (
	// RoleCoder is the agent role that writes and modifies code.
	RoleCoder Role = "coder"
	// RoleReviewer is the agent role that reviews code changes.
	RoleReviewer Role = "reviewer"
)

// Agent describes the configuration for a single agent invocation.
type Agent struct {
	Role         Role
	SystemPrompt string
	Model        string
	MaxBudgetUSD float64
	AllowedTools []string // Tool permissions for this agent (passed as --allowedTools flags)
}

// InvocationResult holds the output and cost metrics from a single agent invocation.
type InvocationResult struct {
	ResultText string
	CostUSD    float64
	DurationMs int64
	SessionID  string
}

// Invoker abstracts the execution of an agent, allowing different backends
// (e.g. Claude CLI, mocks) to satisfy the interface.
type Invoker interface {
	Invoke(ctx context.Context, agent Agent, prompt string, workDir string) (InvocationResult, error)
	Validate() error
}
