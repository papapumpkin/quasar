package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/papapumpkin/quasar/internal/agent"
)

type Invoker struct {
	ClaudePath string
	Verbose    bool
}

// buildEnv constructs the environment for a claude invocation.
// It strips the CLAUDECODE variable (to allow nested invocation) and adds
// CLAUDE_CODE_DISABLE_MCP_POPUPS=1 to suppress MCP server UI popups
// during headless agent runs.
func buildEnv(base []string) []string {
	env := make([]string, 0, len(base)+1)
	for _, e := range base {
		if !strings.HasPrefix(e, "CLAUDECODE=") {
			env = append(env, e)
		}
	}
	env = append(env, "CLAUDE_CODE_DISABLE_MCP_POPUPS=1")
	return env
}

// buildArgs constructs the CLI arguments for a claude invocation.
func buildArgs(a agent.Agent, prompt string) []string {
	args := []string{
		"-p", prompt,
		"--output-format", "json",
	}

	if a.SystemPrompt != "" {
		args = append(args, "--system-prompt", a.SystemPrompt)
	}

	if a.Model != "" {
		args = append(args, "--model", a.Model)
	}

	if a.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", a.MaxBudgetUSD))
	}

	for _, tool := range a.AllowedTools {
		args = append(args, "--allowedTools", tool)
	}

	if a.MCP != nil && a.MCP.ConfigPath != "" {
		args = append(args, "--mcp-config", a.MCP.ConfigPath)
	}

	return args
}

func (inv *Invoker) Invoke(ctx context.Context, a agent.Agent, prompt string, workDir string) (agent.InvocationResult, error) {
	args := buildArgs(a, prompt)

	cmd := exec.CommandContext(ctx, inv.ClaudePath, args...)
	cmd.Dir = workDir
	cmd.SysProcAttr = sessionAttr()

	cmd.Env = buildEnv(os.Environ())

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if inv.Verbose {
		fmt.Fprintf(os.Stderr, "[claude] running: %s %s\n", inv.ClaudePath, strings.Join(args, " "))
	}

	if err := cmd.Run(); err != nil {
		return agent.InvocationResult{}, fmt.Errorf("claude invocation failed: %w\nstderr: %s", err, stderr.String())
	}

	var resp CLIResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return agent.InvocationResult{}, fmt.Errorf("failed to parse claude JSON output: %w\nraw output: %s", err, stdout.String())
	}

	if resp.IsError {
		return agent.InvocationResult{}, fmt.Errorf("claude returned error: %s", resp.Result)
	}

	return agent.InvocationResult{
		ResultText: resp.Result,
		CostUSD:    resp.TotalCostUSD,
		DurationMs: resp.DurationMs,
		SessionID:  resp.SessionID,
	}, nil
}

func (inv *Invoker) Validate() error {
	cmd := exec.Command(inv.ClaudePath, "--version")
	cmd.Env = buildEnv(os.Environ())

	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("claude CLI not found at %q: %w", inv.ClaudePath, err)
	}
	if inv.Verbose {
		fmt.Fprintf(os.Stderr, "[claude] version: %s", string(out))
	}
	return nil
}
