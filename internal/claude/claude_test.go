package claude

import (
	"testing"

	"github.com/aaronsalm/quasar/internal/agent"
)

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
