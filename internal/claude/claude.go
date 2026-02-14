package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/aaronsalm/quasar/internal/agent"
)

type Invoker struct {
	ClaudePath string
	Verbose    bool
}

func (inv *Invoker) Invoke(ctx context.Context, a agent.Agent, prompt string, workDir string) (agent.InvocationResult, error) {
	args := []string{
		"-p", prompt,
		"--output-format", "json",
	}

	systemPrompt := a.SystemPrompt
	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}

	if a.Model != "" {
		args = append(args, "--model", a.Model)
	}

	if a.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", a.MaxBudgetUSD))
	}

	cmd := exec.CommandContext(ctx, inv.ClaudePath, args...)
	cmd.Dir = workDir

	// Strip CLAUDECODE env var to allow nested invocation.
	env := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "CLAUDECODE=") {
			env = append(env, e)
		}
	}
	cmd.Env = env

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
	// Strip CLAUDECODE here too so validation works from within Claude Code.
	env := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "CLAUDECODE=") {
			env = append(env, e)
		}
	}
	cmd.Env = env

	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("claude CLI not found at %q: %w", inv.ClaudePath, err)
	}
	if inv.Verbose {
		fmt.Fprintf(os.Stderr, "[claude] version: %s", string(out))
	}
	return nil
}
