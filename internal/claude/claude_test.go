package claude

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
)

// ---------------------------------------------------------------------------
// Helpers for faking exec.CommandContext / exec.Command in tests.
//
// Each test sets the Invoker struct fields with a function that returns a
// command pointing to a small shell script that simulates the claude CLI binary.
// ---------------------------------------------------------------------------

// writeScript creates an executable shell script in dir and returns its path.
func writeScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+content), 0o755); err != nil {
		t.Fatalf("failed to write script %s: %v", path, err)
	}
	return path
}

// fakeExecContextWith returns a function matching the execCommandContext
// signature that always runs the given script path, ignoring the original
// binary name and arguments.
func fakeExecContextWith(scriptPath string) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, scriptPath)
	}
}

// fakeExecWith returns a function matching the execCommand signature that
// always runs the given script path, ignoring the original binary name and
// arguments.
func fakeExecWith(scriptPath string) func(string, ...string) *exec.Cmd {
	return func(_ string, _ ...string) *exec.Cmd {
		return exec.Command(scriptPath)
	}
}

// newTestInvoker creates an Invoker with the exec functions replaced by fakes.
func newTestInvoker(claudePath string, verbose bool, ctxFn func(context.Context, string, ...string) *exec.Cmd, cmdFn func(string, ...string) *exec.Cmd) *Invoker {
	inv := NewInvoker(claudePath, verbose)
	if ctxFn != nil {
		inv.execCommandContext = ctxFn
	}
	if cmdFn != nil {
		inv.execCommand = cmdFn
	}
	return inv
}

// ---------------------------------------------------------------------------
// Invoke tests
// ---------------------------------------------------------------------------

func TestInvoke_Success(t *testing.T) {
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
	jsonBytes, _ := json.Marshal(resp)

	dir := t.TempDir()
	script := writeScript(t, dir, "claude", "printf '%s' '"+string(jsonBytes)+"'")

	inv := newTestInvoker("claude", false, fakeExecContextWith(script), nil)
	a := agent.Agent{}
	result, err := inv.Invoke(context.Background(), a, "do stuff", dir)
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

func TestInvoke_PopulatesTotalTokens(t *testing.T) {
	resp := CLIResponse{
		Result:       "done",
		SessionID:    "sess-tokens",
		TotalCostUSD: 0.50,
		DurationMs:   2000,
		InputTokens:  15000,
		OutputTokens: 3000,
	}
	jsonBytes, _ := json.Marshal(resp)

	dir := t.TempDir()
	script := writeScript(t, dir, "claude", "printf '%s' '"+string(jsonBytes)+"'")

	inv := newTestInvoker("claude", false, fakeExecContextWith(script), nil)
	a := agent.Agent{}
	result, err := inv.Invoke(context.Background(), a, "do stuff", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TotalTokens != 18000 {
		t.Errorf("TotalTokens = %d, want %d", result.TotalTokens, 18000)
	}
}

func TestInvoke_IsError(t *testing.T) {
	resp := CLIResponse{
		IsError: true,
		Result:  "something went wrong in claude",
	}
	jsonBytes, _ := json.Marshal(resp)

	dir := t.TempDir()
	script := writeScript(t, dir, "claude", "printf '%s' '"+string(jsonBytes)+"'")

	inv := newTestInvoker("claude", false, fakeExecContextWith(script), nil)
	a := agent.Agent{}
	_, err := inv.Invoke(context.Background(), a, "do stuff", dir)
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
	dir := t.TempDir()
	script := writeScript(t, dir, "claude", `printf '%s' 'this is not json {{{'`)

	inv := newTestInvoker("claude", false, fakeExecContextWith(script), nil)
	a := agent.Agent{}
	_, err := inv.Invoke(context.Background(), a, "do stuff", dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse claude JSON output") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "failed to parse claude JSON output")
	}
}

func TestInvoke_ExitError(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "claude", `echo "fatal: out of tokens" >&2; exit 1`)

	inv := newTestInvoker("claude", false, fakeExecContextWith(script), nil)
	a := agent.Agent{}
	_, err := inv.Invoke(context.Background(), a, "do stuff", dir)
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
	dir := t.TempDir()
	// Script that blocks via exec (replaces shell process) so SIGKILL reaches it directly.
	script := writeScript(t, dir, "claude", "exec sleep 300")

	// Use a custom fake that sets WaitDelay so the process is reaped promptly
	// after context cancellation (default WaitDelay=0 waits for I/O indefinitely).
	fakeCtx := func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, script)
		cmd.WaitDelay = 100 * time.Millisecond
		return cmd
	}
	inv := newTestInvoker("claude", false, fakeCtx, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	a := agent.Agent{}
	_, err := inv.Invoke(ctx, a, "do stuff", dir)
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
	if !strings.Contains(err.Error(), "claude invocation failed") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "claude invocation failed")
	}
}

func TestInvoke_VerboseLogging(t *testing.T) {
	resp := CLIResponse{
		Result:    "done",
		SessionID: "sess-verbose",
	}
	jsonBytes, _ := json.Marshal(resp)

	dir := t.TempDir()
	script := writeScript(t, dir, "claude", "printf '%s' '"+string(jsonBytes)+"'")

	inv := newTestInvoker("claude", true, fakeExecContextWith(script), nil)
	a := agent.Agent{}
	result, err := inv.Invoke(context.Background(), a, "do stuff", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verbose mode should still produce a valid result.
	if result.ResultText != "done" {
		t.Errorf("ResultText = %q, want %q", result.ResultText, "done")
	}
}

// ---------------------------------------------------------------------------
// Validate tests
// ---------------------------------------------------------------------------

func TestValidate_Success(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "claude", `echo "claude 1.2.3"`)

	inv := newTestInvoker("claude", false, nil, fakeExecWith(script))
	if err := inv.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_VerboseLogging(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "claude", `echo "claude 1.2.3"`)

	inv := newTestInvoker("claude", true, nil, fakeExecWith(script))
	if err := inv.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_BinaryNotFound(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, dir, "claude", `exit 1`)

	inv := newTestInvoker("/nonexistent/path/to/claude", false, nil, fakeExecWith(script))
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
		{
			name:     "resume session present",
			agent:    agent.Agent{ResumeSessionID: "sess-abc123"},
			wantFlag: "--resume",
			present:  true,
		},
		{
			name:     "resume session absent",
			agent:    agent.Agent{},
			wantFlag: "--resume",
			present:  false,
		},
		{
			name:     "effort present",
			agent:    agent.Agent{Effort: "low"},
			wantFlag: "--effort",
			present:  true,
		},
		{
			name:     "effort absent",
			agent:    agent.Agent{},
			wantFlag: "--effort",
			present:  false,
		},
		{
			name:     "fallback model present",
			agent:    agent.Agent{FallbackModel: "sonnet"},
			wantFlag: "--fallback-model",
			present:  true,
		},
		{
			name:     "fallback model absent",
			agent:    agent.Agent{},
			wantFlag: "--fallback-model",
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

// ---------------------------------------------------------------------------
// sha256Hex tests
// ---------------------------------------------------------------------------

func TestSHA256Hex_KnownDigest(t *testing.T) {
	// SHA-256 of the empty string is a well-known constant.
	got := sha256Hex("")
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Errorf("sha256Hex(%q) = %q, want %q", "", got, want)
	}
}

func TestSHA256Hex_DeterministicAndDistinct(t *testing.T) {
	a := sha256Hex("hello")
	b := sha256Hex("hello")
	c := sha256Hex("world")

	if a != b {
		t.Errorf("sha256Hex is not deterministic: %q != %q", a, b)
	}
	if a == c {
		t.Errorf("sha256Hex returned same hash for different inputs: %q", a)
	}
	// SHA-256 hex is always 64 characters.
	if len(a) != 64 {
		t.Errorf("sha256Hex length = %d, want 64", len(a))
	}
}

// ---------------------------------------------------------------------------
// Invoke prompt metrics tests
// ---------------------------------------------------------------------------

func TestInvoke_PopulatesPromptMetrics(t *testing.T) {
	resp := CLIResponse{
		Result:       "ok",
		SessionID:    "sess-metrics",
		TotalCostUSD: 0.10,
		DurationMs:   500,
	}
	jsonBytes, _ := json.Marshal(resp)

	dir := t.TempDir()
	script := writeScript(t, dir, "claude", "printf '%s' '"+string(jsonBytes)+"'")

	inv := newTestInvoker("claude", false, fakeExecContextWith(script), nil)
	sysPrompt := "You are a helpful assistant."
	a := agent.Agent{SystemPrompt: sysPrompt}
	userPrompt := "Write a hello world program."
	result, err := inv.Invoke(context.Background(), a, userPrompt, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.SystemPromptLen != len(sysPrompt) {
		t.Errorf("SystemPromptLen = %d, want %d", result.SystemPromptLen, len(sysPrompt))
	}
	if result.UserPromptLen != len(userPrompt) {
		t.Errorf("UserPromptLen = %d, want %d", result.UserPromptLen, len(userPrompt))
	}
	wantHash := sha256Hex(sysPrompt)
	if result.SystemPromptHash != wantHash {
		t.Errorf("SystemPromptHash = %q, want %q", result.SystemPromptHash, wantHash)
	}
}

func TestInvoke_EmptySystemPrompt(t *testing.T) {
	resp := CLIResponse{
		Result:    "ok",
		SessionID: "sess-empty",
	}
	jsonBytes, _ := json.Marshal(resp)

	dir := t.TempDir()
	script := writeScript(t, dir, "claude", "printf '%s' '"+string(jsonBytes)+"'")

	inv := newTestInvoker("claude", false, fakeExecContextWith(script), nil)
	a := agent.Agent{} // no system prompt
	result, err := inv.Invoke(context.Background(), a, "do stuff", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.SystemPromptLen != 0 {
		t.Errorf("SystemPromptLen = %d, want 0", result.SystemPromptLen)
	}
	// Hash of empty string should still be populated.
	if result.SystemPromptHash == "" {
		t.Error("SystemPromptHash should be non-empty even for empty system prompt")
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
