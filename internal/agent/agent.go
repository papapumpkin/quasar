package agent

import "context"

type Role string

const (
	RoleCoder    Role = "coder"
	RoleReviewer Role = "reviewer"
)

type Agent struct {
	Role         Role
	SystemPrompt string
	Model        string
	MaxBudgetUSD float64
	AllowedTools []string // Tool permissions for this agent (passed as --allowedTools flags)
}

type InvocationResult struct {
	ResultText string
	CostUSD    float64
	DurationMs int64
	SessionID  string
}

type Invoker interface {
	Invoke(ctx context.Context, agent Agent, prompt string, workDir string) (InvocationResult, error)
	Validate() error
}
