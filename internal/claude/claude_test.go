package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
)

// ---------------------------------------------------------------------------
// TestHelperProcess — fake claude binary for subprocess tests.
//
// This is not a real test. Tests that need to simulate the claude CLI set
// execCommandContext / execCommand to return a command that re-execs the test
// binary with -test.run=^TestHelperProcess$ and GO_WANT_HELPER_PROCESS=1.
// The HELPER_MODE env var selects which fake behavior to produce.
// ---------------------------------------------------------------------------

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	mode := os.Getenv("HELPER_MODE")
	switch mode {
	case "success":
		resp := CLIResponse{
			Type:         "result",
			Subtype:      "success",
			IsError:      false,
			DurationMs:   1234,
			NumTurns:     3,
			Result:       "all tests passed",
			SessionID:    "sess-abc123",
			TotalCostUSD: 0.42,
		}
		out, _ := json.Marshal(resp)
		fmt.Fprint(os.Stdout, string(out))
		os.Exit(0)
	case "is_error":
		resp := CLIResponse{
			IsError: true,
			Result:  "something went wrong in claude",
		}
		out, _ := json.Marshal(resp)
		fmt.Fprint(os.Stdout, string(out))
		os.Exit(0)
	case "invalid_json":
		fmt.Fprint(os.Stdout, "this is not json {{{")
		os.Exit(0)
	case "exit_error":
		fmt.Fprint(os.Stderr, "fatal: out of tokens")
		os.Exit(1)
	case "version":
		fmt.Fprint(os.Stdout, "claude 1.2.3\n")
		os.Exit(0)
	case "hang":
		// Block until killed to test context cancellation.
		select {}
	default:
		fmt.Fprintf(os.Stderr, "unknown HELPER_MODE: %s", mode)
		os.Exit(2)
	}
}

// fakeExecCommandContext returns a function matching the exec.CommandContext
// signature that spawns the test binary as a helper process with the given mode.
func fakeExecCommandContext(mode string) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHelperProcess$")
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			"HELPER_MODE="+mode,
		)
		return cmd
	}
}

// fakeExecCommand returns a function matching the exec.Command signature that
// spawns the test binary as a helper process with the given mode.
func fakeExecCommand(mode string) func(string, ...string) *exec.Cmd {
	return func(_ string, _ ...string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			"HELPER_MODE="+mode,
		)
		return cmd
	}
}

// ---------------------------------------------------------------------------
// Invoke tests
// ---------------------------------------------------------------------------

func TestInvoke_Success(t *testing.T) {
	origExec := execCommandContext
	execCommandContext = fakeExecCommandContext("success")
	defer func() { execCommandContext = origExec }()

	inv := &Invoker{ClaudePath: "claude"}
	a := agent.Agent{}
	result, err := inv.Invoke(context.Background(), a, "do stuff", t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ResultText != "all tests passed" {
		t.Errorf("ResultText = %q, want %q", result.ResultText, "all tests passed")
	}
	if result.CostUSD != 0.42 {
		t.Errorf("CostUSD = %v, want %v", result.CostUSD, 0.42)
	}
	if result.DurationMs != 1234 {
		t.Errorf("DurationMs = %v, want %v", result.DurationMs, 1234)
	}
	if result.SessionID != "sess-abc123" {
		t.Errorf("SessionID = %q, want %q", result.SessionID, "sess-abc123")
	}
}

func TestInvoke_IsError(t *testing.T) {
	origExec := execCommandContext
	execCommandContext = fakeExecCommandContext("is_error")
	defer func() { execCommandContext = origExec }()

	inv := &Invoker{ClaudePath: "claude"}
	a := agent.Agent{}
	_, err := inv.Invoke(context.Background(), a, "do stuff", t.TempDir())
	if err == nil {
		t.Fatal("expected error when IsError is true, got nil")
	}
	if !strings.Contains(err.Error(), "claude returned error") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "claude returned error")
	}
	if !strings.Contains(err.Error(), "something went wrong in claude") {
		t.Errorf("error = %q, want it to contain the error message", err.Error())
	}
}

func TestInvoke_InvalidJSON(t *testing.T) {
	origExec := execCommandContext
	execCommandContext = fakeExecCommandContext("invalid_json")
	defer func() { execCommandContext = origExec }()

	inv := &Invoker{ClaudePath: "claude"}
	a := agent.Agent{}
	_, err := inv.Invoke(context.Background(), a, "do stuff", t.TempDir())
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse claude JSON output") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "failed to parse claude JSON output")
	}
}

func TestInvoke_ExitError(t *testing.T) {
	origExec := execCommandContext
	execCommandContext = fakeExecCommandContext("exit_error")
	defer func() { execCommandContext = origExec }()

	inv := &Invoker{ClaudePath: "claude"}
	a := agent.Agent{}
	_, err := inv.Invoke(context.Background(), a, "do stuff", t.TempDir())
	if err == nil {
		t.Fatal("expected error for non-zero exit code, got nil")
	}
	if !strings.Contains(err.Error(), "claude invocation failed") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "claude invocation failed")
	}
	if !strings.Contains(err.Error(), "fatal: out of tokens") {
		t.Errorf("error = %q, want it to contain stderr output", err.Error())
	}
}

func TestInvoke_ContextCancellation(t *testing.T) {
	origExec := execCommandContext
	execCommandContext = fakeExecCommandContext("hang")
	defer func() { execCommandContext = origExec }()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	inv := &Invoker{ClaudePath: "claude"}
	a := agent.Agent{}
	_, err := inv.Invoke(ctx, a, "do stuff", t.TempDir())
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
	if !strings.Contains(err.Error(), "claude invocation failed") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "claude invocation failed")
	}
}

func TestInvoke_VerboseLogging(t *testing.T) {
	origExec := execCommandContext
	execCommandContext = fakeExecCommandContext("success")
	defer func() { execCommandContext = origExec }()

	inv := &Invoker{ClaudePath: "claude", Verbose: true}
	a := agent.Agent{}
	result, err := inv.Invoke(context.Background(), a, "do stuff", t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verbose mode should still produce a valid result.
	if result.ResultText != "all tests passed" {
		t.Errorf("ResultText = %q, want %q", result.ResultText, "all tests passed")
	}
}

// ---------------------------------------------------------------------------
// Validate tests
// ---------------------------------------------------------------------------

func TestValidate_Success(t *testing.T) {
	origExec := execCommand
	execCommand = fakeExecCommand("version")
	defer func() { execCommand = origExec }()

	inv := &Invoker{ClaudePath: "claude"}
	if err := inv.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_VerboseLogging(t *testing.T) {
	origExec := execCommand
	execCommand = fakeExecCommand("version")
	defer func() { execCommand = origExec }()

	inv := &Invoker{ClaudePath: "claude", Verbose: true}
	if err := inv.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_BinaryNotFound(t *testing.T) {
	origExec := execCommand
	execCommand = fakeExecCommand("exit_error")
	defer func() { execCommand = origExec }()

	inv := &Invoker{ClaudePath: "/nonexistent/path/to/claude"}
	err := inv.Validate()
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
	if !strings.Contains(err.Error(), "claude CLI not found") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "claude CLI not found")
	}
}

// ---------------------------------------------------------------------------
// Existing buildArgs / buildEnv tests
// ---------------------------------------------------------------------------

func TestBuildArgs_AllowedTools(t *testing.T) {
	a := agent.Agent{
		AllowedTools: []string{"Read", "Edit", "Bash(go *)"},
	}
	args := buildArgs(a, "do stuff")

	// Collect all --allowedTools values.
	var tools []string
	for i, arg := range args {
		if arg == "--allowedTools" && i+1 < len(args) {
			tools = append(tools, args[i+1])
		}
	}

	if len(tools) != 3 {
		t.Fatalf("expected 3 --allowedTools flags, got %d: %v", len(tools), tools)
	}

	expected := []string{"Read", "Edit", "Bash(go *)"}
	for i, e := range expected {
		if tools[i] != e {
			t.Errorf("allowedTools[%d] = %q, want %q", i, tools[i], e)
		}
	}
}

func TestBuildArgs_NoAllowedTools(t *testing.T) {
	a := agent.Agent{}
	args := buildArgs(a, "do stuff")

	for _, arg := range args {
		if arg == "--allowedTools" {
			t.Fatal("expected no --allowedTools flags when AllowedTools is empty")
		}
	}
}

func TestBuildArgs_OptionalFlags(t *testing.T) {
	tests := []struct {
		name     string
		agent    agent.Agent
		wantFlag string
		present  bool
	}{
		{
			name:     "system prompt present",
			agent:    agent.Agent{SystemPrompt: "be helpful"},
			wantFlag: "--system-prompt",
			present:  true,
		},
		{
			name:     "system prompt absent",
			agent:    agent.Agent{},
			wantFlag: "--system-prompt",
			present:  false,
		},
		{
			name:     "model present",
			agent:    agent.Agent{Model: "opus"},
			wantFlag: "--model",
			present:  true,
		},
		{
			name:     "model absent",
			agent:    agent.Agent{},
			wantFlag: "--model",
			present:  false,
		},
		{
			name:     "budget present",
			agent:    agent.Agent{MaxBudgetUSD: 1.50},
			wantFlag: "--max-budget-usd",
			present:  true,
		},
		{
			name:     "budget absent",
			agent:    agent.Agent{},
			wantFlag: "--max-budget-usd",
			present:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildArgs(tt.agent, "test prompt")
			found := false
			for _, arg := range args {
				if arg == tt.wantFlag {
					found = true
					break
				}
			}
			if found != tt.present {
				t.Errorf("flag %q: found=%v, want present=%v (args: %v)", tt.wantFlag, found, tt.present, args)
			}
		})
	}
}

func TestBuildArgs_BaseFlags(t *testing.T) {
	a := agent.Agent{}
	args := buildArgs(a, "hello world")

	// Should always have -p and --output-format json
	if args[0] != "-p" || args[1] != "hello world" {
		t.Errorf("expected args[0:2] = [-p, hello world], got %v", args[0:2])
	}
	if args[2] != "--output-format" || args[3] != "json" {
		t.Errorf("expected args[2:4] = [--output-format, json], got %v", args[2:4])
	}
}

func TestBuildArgs_MCPConfigPresent(t *testing.T) {
	a := agent.Agent{
		MCP: &agent.MCPConfig{ConfigPath: "/tmp/mcp-config.json"},
	}
	args := buildArgs(a, "do stuff")

	found := false
	for i, arg := range args {
		if arg == "--mcp-config" && i+1 < len(args) && args[i+1] == "/tmp/mcp-config.json" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected --mcp-config /tmp/mcp-config.json in args, got: %v", args)
	}
}

func TestBuildArgs_MCPConfigAbsent(t *testing.T) {
	a := agent.Agent{} // MCP is nil
	args := buildArgs(a, "do stuff")

	for _, arg := range args {
		if arg == "--mcp-config" {
			t.Fatal("expected no --mcp-config flag when MCP is nil")
		}
	}
}

func TestBuildArgs_MCPConfigEmptyPath(t *testing.T) {
	a := agent.Agent{
		MCP: &agent.MCPConfig{ConfigPath: ""},
	}
	args := buildArgs(a, "do stuff")

	for _, arg := range args {
		if arg == "--mcp-config" {
			t.Fatal("expected no --mcp-config flag when ConfigPath is empty")
		}
	}
}

func TestBuildEnv_StripsCLAUDECODE(t *testing.T) {
	base := []string{"PATH=/usr/bin", "CLAUDECODE=something", "HOME=/home/user"}
	env := buildEnv(base)

	for _, e := range env {
		if strings.HasPrefix(e, "CLAUDECODE=") {
			t.Errorf("CLAUDECODE should be stripped, but found: %s", e)
		}
	}
}

func TestBuildEnv_SuppressesMCPPopups(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/home/user"}
	env := buildEnv(base)

	found := false
	for _, e := range env {
		if e == "CLAUDE_CODE_DISABLE_MCP_POPUPS=1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected CLAUDE_CODE_DISABLE_MCP_POPUPS=1 in env, but it was not present")
	}
}
