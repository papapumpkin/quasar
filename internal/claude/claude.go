package claude

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
	"github.com/papapumpkin/quasar/internal/telemetry"
)

// Invoker runs the Claude CLI as a subprocess and parses JSON output.
type Invoker struct {
	ClaudePath         string
	Verbose            bool
	execCommandContext func(ctx context.Context, name string, arg ...string) *exec.Cmd
	execCommand        func(name string, arg ...string) *exec.Cmd

	// Health, when non-nil and Enabled, attaches a multi-signal healthcheck to
	// every Invoke so a stalled or thrashing coder is terminated rather than
	// allowed to burn inference silently. A nil/disabled config preserves the
	// legacy blocking cmd.Run path exactly.
	Health *HealthConfig
}

// HealthConfig controls subprocess dead-coder detection for an Invoker.
type HealthConfig struct {
	// Enabled turns on healthcheck monitoring.
	Enabled bool
	// Policy holds the (possibly per-star-overridden) thresholds. Zero-valued
	// fields fall back to the conservative defaults.
	Policy HealthPolicy
	// Events, when non-nil, receives a JSONL record per state transition.
	Events *telemetry.HealthEventStore
}

// NewInvoker creates an Invoker with sensible defaults for command execution.
func NewInvoker(claudePath string, verbose bool) *Invoker {
	return &Invoker{
		ClaudePath:         claudePath,
		Verbose:            verbose,
		execCommandContext: exec.CommandContext,
		execCommand:        exec.Command,
	}
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

	// Keep dynamic content (timestamps, env) out of the system prompt so the
	// cache prefix is byte-stable across invocations and prompt caching hits.
	if a.CacheOptimization {
		args = append(args, "--exclude-dynamic-system-prompt-sections")
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

	if a.ResumeSessionID != "" {
		args = append(args, "--resume", a.ResumeSessionID)
	}

	if a.Effort != "" {
		args = append(args, "--effort", a.Effort)
	}

	if a.FallbackModel != "" {
		args = append(args, "--fallback-model", a.FallbackModel)
	}

	return args
}

func (inv *Invoker) Invoke(ctx context.Context, a agent.Agent, prompt string, workDir string) (agent.InvocationResult, error) {
	args := buildArgs(a, prompt)

	// Install the per-tool budget hook when the agent opts in. Failure is
	// fail-open: log and proceed without the hook so a setup glitch never
	// blocks the invocation (correctness over throughput).
	if a.ContextBudget != nil && a.ContextBudget.EnableToolHook {
		settingsPath, cleanup, err := writeToolHookSettings(*a.ContextBudget)
		if err != nil {
			if inv.Verbose {
				fmt.Fprintf(os.Stderr, "[claude] tool-budget hook setup failed: %v\n", err)
			}
		} else {
			defer cleanup()
			args = append(args, "--settings", settingsPath)
		}
	}

	cmd := inv.execCommandContext(ctx, inv.ClaudePath, args...)
	cmd.Dir = workDir
	cmd.SysProcAttr = sessionAttr()

	cmd.Env = buildEnv(os.Environ())

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if inv.Verbose {
		fmt.Fprintf(os.Stderr, "[claude] running: %s %s\n", inv.ClaudePath, strings.Join(args, " "))
	}

	if err := inv.run(ctx, cmd, workDir, &stderr); err != nil {
		return agent.InvocationResult{}, err
	}

	var resp CLIResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return agent.InvocationResult{}, fmt.Errorf("failed to parse claude JSON output: %w\nraw output: %s", err, stdout.String())
	}

	if resp.IsError {
		return agent.InvocationResult{}, fmt.Errorf("claude returned error: %s", resp.Result)
	}

	// Bound the result before it flows on: a coder's output becomes the
	// reviewer's input context and vice versa, so an unbounded result silently
	// inflates the next invocation's token cost. The cap comes from the agent's
	// context budget (ToolResultMaxBytes) or its per-role default.
	resultText, wasTruncated := TruncateResult(resp.Result, truncationPolicyFor(a))
	if wasTruncated && inv.Verbose {
		fmt.Fprintf(os.Stderr, "[claude] truncated result from %d to %d bytes\n", len(resp.Result), len(resultText))
	}

	return agent.InvocationResult{
		ResultText:          resultText,
		CostUSD:             resp.TotalCostUSD,
		DurationMs:          resp.DurationMs,
		SessionID:           resp.SessionID,
		SystemPromptLen:     len(a.SystemPrompt),
		UserPromptLen:       len(prompt),
		SystemPromptHash:    sha256Hex(a.SystemPrompt),
		InputTokens:         resp.Usage.InputTokens,
		CacheCreationTokens: resp.Usage.CacheCreationInputTokens,
		CacheReadTokens:     resp.Usage.CacheReadInputTokens,
	}, nil
}

func (inv *Invoker) Validate() error {
	cmd := inv.execCommand(inv.ClaudePath, "--version")
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

// run executes cmd, optionally under a dead-coder healthcheck. With no health
// config it preserves the legacy blocking cmd.Run path. With health enabled it
// starts the subprocess, monitors it via the multi-signal probe, and returns a
// *DeadCoderError if the coder was terminated for stalling or thrashing.
func (inv *Invoker) run(ctx context.Context, cmd *exec.Cmd, workDir string, stderr *bytes.Buffer) error {
	if inv.Health == nil || !inv.Health.Enabled {
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("claude invocation failed: %w\nstderr: %s", err, stderr.String())
		}
		return nil
	}
	return inv.runMonitored(ctx, cmd, workDir, stderr)
}

// runMonitored runs cmd alongside a Healthcheck. The subprocess is reaped by a
// dedicated goroutine that closes `exited`; the healthcheck blocks on the same
// channel, so the termination handshake never double-waits on the process.
func (inv *Invoker) runMonitored(ctx context.Context, cmd *exec.Cmd, workDir string, stderr *bytes.Buffer) error {
	hc := &Healthcheck{
		Cmd:          cmd,
		Workdir:      workDir,
		Policy:       inv.Health.Policy,
		Events:       inv.Health.Events,
		InvocationID: fmt.Sprintf("inv-%d", time.Now().UnixNano()),
	}
	if inv.Verbose {
		hc.OnStateChange = func(s HealthState, reason string) {
			fmt.Fprintf(os.Stderr, "[health] state=%s %s\n", s, reason)
		}
	}

	// Attach the cheap, always-available signals (file-write idle, CPU idle).
	// The token-rate signal needs stream-json output and is wired by callers
	// that switch the output format; the probe is left nil here (valid=false).
	if fw, err := newFileWriteWatcher(workDir, nil); err == nil {
		defer fw.Close()
		hc.writeIdleFn = fw.IdleSince
	} else if inv.Verbose {
		fmt.Fprintf(os.Stderr, "[health] file-write watcher unavailable: %v\n", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("claude start failed: %w", err)
	}

	// CPU poller needs the started pid; run it until the subprocess is reaped.
	pollCtx, stopPoll := context.WithCancel(ctx)
	defer stopPoll()
	if cmd.Process != nil {
		cp := newCPUPoller(cmd.Process.Pid, nil)
		hc.cpuIdleFn = cp.IdleSince
		go runCPUPoller(pollCtx, cp, DefaultTick)
	}

	exited := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(exited)
	}()

	hcErr := hc.Run(ctx, exited)
	<-exited // guarantee waitErr is set (close happens-before this receive)

	if dce, ok := hcErr.(*DeadCoderError); ok {
		return dce
	}
	if waitErr != nil {
		return fmt.Errorf("claude invocation failed: %w\nstderr: %s", waitErr, stderr.String())
	}
	return nil
}

// sha256Hex returns the SHA-256 hex digest of s.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
