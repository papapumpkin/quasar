package nebula

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
)

func TestParseMultiPhaseOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		output     string
		wantCount  int
		wantIDs    []string
		wantFiles  []string
		wantErr    bool
		errContain string
	}{
		{
			name: "three valid phases",
			output: `Here is the decomposition:

PHASE_FILE: 01-setup-database.md
+++
id = "setup-database"
title = "Set Up Database Schema"
scope = ["internal/db/**"]
+++

## Problem

We need a database schema.

## Solution

Create migration files.
END_PHASE_FILE

PHASE_FILE: 02-implement-auth.md
+++
id = "implement-auth"
title = "Implement Authentication"
depends_on = ["setup-database"]
scope = ["internal/auth/**"]
+++

## Problem

We need user authentication.

## Solution

Use JWT tokens.
END_PHASE_FILE

PHASE_FILE: 03-add-routes.md
+++
id = "add-routes"
title = "Add API Routes"
depends_on = ["implement-auth"]
scope = ["internal/api/**"]
+++

## Problem

We need API endpoints.

## Solution

Add REST routes.
END_PHASE_FILE
`,
			wantCount: 3,
			wantIDs:   []string{"setup-database", "implement-auth", "add-routes"},
			wantFiles: []string{"01-setup-database.md", "02-implement-auth.md", "03-add-routes.md"},
		},
		{
			name: "single phase",
			output: `PHASE_FILE: 01-single-task.md
+++
id = "single-task"
title = "Single Task"
+++

## Problem

A simple task.
END_PHASE_FILE
`,
			wantCount: 1,
			wantIDs:   []string{"single-task"},
			wantFiles: []string{"01-single-task.md"},
		},
		{
			name:       "missing END_PHASE_FILE",
			output:     "PHASE_FILE: broken.md\n+++\nid = \"broken\"\ntitle = \"Broken\"\n+++\n\nNo end marker.",
			wantErr:    true,
			errContain: "missing",
		},
		{
			name:      "no phases at all",
			output:    "The architect had nothing to say.",
			wantCount: 0,
			wantIDs:   nil,
			wantFiles: nil,
		},
		{
			name: "invalid filename in second block",
			output: `PHASE_FILE: 01-good.md
+++
id = "good"
title = "Good Phase"
+++

Body.
END_PHASE_FILE

PHASE_FILE: ../bad-path.md
+++
id = "bad"
title = "Bad Phase"
+++

Body.
END_PHASE_FILE
`,
			wantErr:    true,
			errContain: "path separators",
		},
		{
			name: "malformed frontmatter in one phase",
			output: `PHASE_FILE: 01-bad-toml.md
+++
id = this is not valid TOML ][
+++

Body.
END_PHASE_FILE
`,
			wantErr:    true,
			errContain: "parsing TOML",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			results, err := parseMultiPhaseOutput(tt.output)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Fatalf("expected error containing %q, got %q", tt.errContain, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(results) != tt.wantCount {
				t.Fatalf("expected %d phases, got %d", tt.wantCount, len(results))
			}

			for i, wantID := range tt.wantIDs {
				if results[i].PhaseSpec.ID != wantID {
					t.Errorf("phase %d: expected ID %q, got %q", i, wantID, results[i].PhaseSpec.ID)
				}
			}
			for i, wantFile := range tt.wantFiles {
				if results[i].Filename != wantFile {
					t.Errorf("phase %d: expected filename %q, got %q", i, wantFile, results[i].Filename)
				}
			}
		})
	}
}

func TestParseMultiPhaseOutput_DependsOnPreserved(t *testing.T) {
	t.Parallel()

	output := `PHASE_FILE: 01-base.md
+++
id = "base"
title = "Base Setup"
+++

Base body.
END_PHASE_FILE

PHASE_FILE: 02-derived.md
+++
id = "derived"
title = "Derived Work"
depends_on = ["base"]
+++

Derived body.
END_PHASE_FILE
`
	results, err := parseMultiPhaseOutput(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 phases, got %d", len(results))
	}

	deps := results[1].PhaseSpec.DependsOn
	if len(deps) != 1 || deps[0] != "base" {
		t.Errorf("expected depends_on=[\"base\"], got %v", deps)
	}
}

func TestParseMultiPhaseOutput_ScopePreserved(t *testing.T) {
	t.Parallel()

	output := `PHASE_FILE: 01-scoped.md
+++
id = "scoped"
title = "Scoped Phase"
scope = ["internal/auth/**", "cmd/auth*.go"]
+++

Scoped body.
END_PHASE_FILE
`
	results, err := parseMultiPhaseOutput(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(results))
	}

	scope := results[0].PhaseSpec.Scope
	if len(scope) != 2 {
		t.Fatalf("expected 2 scope entries, got %d: %v", len(scope), scope)
	}
	if scope[0] != "internal/auth/**" {
		t.Errorf("expected scope[0]=%q, got %q", "internal/auth/**", scope[0])
	}
}

func TestBuildManifest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		req         GenerateRequest
		wantName    string
		wantWorkers int
		wantCycles  int
		wantGate    GateMode
		wantType    string
	}{
		{
			name: "basic manifest",
			req: GenerateRequest{
				UserPrompt:   "Build a REST API for user management",
				NebulaName:   "user-api",
				MaxBudgetUSD: 50.0,
				Model:        "claude-opus-4",
			},
			wantName:    "user-api",
			wantWorkers: 2,
			wantCycles:  5,
			wantGate:    GateModeReview,
			wantType:    "task",
		},
		{
			name: "zero budget and empty model",
			req: GenerateRequest{
				UserPrompt: "Add logging",
				NebulaName: "logging",
			},
			wantName:    "logging",
			wantWorkers: 2,
			wantCycles:  5,
			wantGate:    GateModeReview,
			wantType:    "task",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := buildManifest(tt.req)

			if m.Nebula.Name != tt.wantName {
				t.Errorf("name: got %q, want %q", m.Nebula.Name, tt.wantName)
			}
			if m.Execution.MaxWorkers != tt.wantWorkers {
				t.Errorf("max_workers: got %d, want %d", m.Execution.MaxWorkers, tt.wantWorkers)
			}
			if m.Execution.MaxReviewCycles != tt.wantCycles {
				t.Errorf("max_review_cycles: got %d, want %d", m.Execution.MaxReviewCycles, tt.wantCycles)
			}
			if m.Execution.Gate != tt.wantGate {
				t.Errorf("gate: got %q, want %q", m.Execution.Gate, tt.wantGate)
			}
			if m.Defaults.Type != tt.wantType {
				t.Errorf("defaults.type: got %q, want %q", m.Defaults.Type, tt.wantType)
			}
			if m.Defaults.Priority != 2 {
				t.Errorf("defaults.priority: got %d, want 2", m.Defaults.Priority)
			}

			// Goals should contain the user prompt.
			if len(m.Context.Goals) != 1 || m.Context.Goals[0] != tt.req.UserPrompt {
				t.Errorf("goals: got %v, want [%q]", m.Context.Goals, tt.req.UserPrompt)
			}
		})
	}
}

func TestBuildManifest_DescriptionTruncation(t *testing.T) {
	t.Parallel()

	longPrompt := strings.Repeat("x", 300)
	m := buildManifest(GenerateRequest{
		UserPrompt: longPrompt,
		NebulaName: "test",
	})

	if len(m.Nebula.Description) > 200 {
		t.Errorf("description not truncated: len=%d", len(m.Nebula.Description))
	}
	if !strings.HasSuffix(m.Nebula.Description, "...") {
		t.Error("truncated description should end with '...'")
	}
}

func TestTruncateDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is too long", 10, "this is..."},
		{"ab", 3, "ab"},
		{"abcdef", 3, "abc"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d_%s", tt.maxLen, tt.input), func(t *testing.T) {
			t.Parallel()
			got := truncateDescription(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateDescription(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestGenerate(t *testing.T) {
	t.Parallel()

	cannedOutput := `Here are the phases for your nebula:

PHASE_FILE: 01-setup-models.md
+++
id = "setup-models"
title = "Define Data Models"
scope = ["internal/models/**"]
+++

## Problem

We need data models for user management.

## Solution

Define User and Role structs.

## Files

- ` + "`internal/models/user.go`" + ` — User struct
- ` + "`internal/models/role.go`" + ` — Role struct

## Acceptance Criteria

- [ ] User and Role structs defined
END_PHASE_FILE

PHASE_FILE: 02-add-handlers.md
+++
id = "add-handlers"
title = "Add HTTP Handlers"
depends_on = ["setup-models"]
scope = ["internal/api/**"]
+++

## Problem

We need HTTP handlers for CRUD operations.

## Solution

Implement REST handlers using the data models.

## Files

- ` + "`internal/api/handlers.go`" + ` — HTTP handlers

## Acceptance Criteria

- [ ] CRUD handlers implemented
END_PHASE_FILE

PHASE_FILE: 03-add-tests.md
+++
id = "add-tests"
title = "Add Integration Tests"
depends_on = ["add-handlers"]
scope = ["internal/tests/**"]
+++

## Problem

We need tests for the API handlers.

## Solution

Write table-driven tests for each endpoint.

## Files

- ` + "`internal/tests/handlers_test.go`" + ` — Handler tests

## Acceptance Criteria

- [ ] All handlers have test coverage
END_PHASE_FILE
`

	mock := &mockInvoker{
		result: agent.InvocationResult{
			ResultText: cannedOutput,
			CostUSD:    0.05,
		},
	}

	req := GenerateRequest{
		UserPrompt:   "Build a REST API for user management",
		NebulaName:   "user-api",
		OutputDir:    "/tmp/test-nebula",
		WorkDir:      "/tmp/test-repo",
		MaxBudgetUSD: 50.0,
	}

	result, err := Generate(context.Background(), mock, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify we got 3 phases.
	if len(result.Phases) != 3 {
		t.Fatalf("expected 3 phases, got %d", len(result.Phases))
	}

	// Verify phase IDs.
	wantIDs := []string{"setup-models", "add-handlers", "add-tests"}
	for i, wantID := range wantIDs {
		if result.Phases[i].ID != wantID {
			t.Errorf("phase %d: expected ID %q, got %q", i, wantID, result.Phases[i].ID)
		}
	}

	// Verify dependency chain is intact.
	if len(result.Phases[0].DependsOn) != 0 {
		t.Errorf("phase 0 should have no deps, got %v", result.Phases[0].DependsOn)
	}
	if deps := result.Phases[1].DependsOn; len(deps) != 1 || deps[0] != "setup-models" {
		t.Errorf("phase 1 deps: want [setup-models], got %v", deps)
	}
	if deps := result.Phases[2].DependsOn; len(deps) != 1 || deps[0] != "add-handlers" {
		t.Errorf("phase 2 deps: want [add-handlers], got %v", deps)
	}

	// Verify defaults were applied.
	for _, p := range result.Phases {
		if p.Type != "task" {
			t.Errorf("phase %q: expected type 'task', got %q", p.ID, p.Type)
		}
		if p.Priority != 2 {
			t.Errorf("phase %q: expected priority 2, got %d", p.ID, p.Priority)
		}
	}

	// Verify manifest.
	if result.Manifest.Nebula.Name != "user-api" {
		t.Errorf("manifest name: got %q, want %q", result.Manifest.Nebula.Name, "user-api")
	}

	// Verify cost.
	if result.CostUSD != 0.05 {
		t.Errorf("cost: got %f, want 0.05", result.CostUSD)
	}

	// Verify inference was run.
	if result.InferResult == nil {
		t.Error("expected non-nil InferResult")
	}

	// Verify the Nebula is fully assembled.
	if result.Nebula == nil {
		t.Fatal("expected non-nil Nebula")
	}
	if len(result.Nebula.Phases) != 3 {
		t.Errorf("nebula phases: got %d, want 3", len(result.Nebula.Phases))
	}
}

func TestGenerate_EmptyPromptError(t *testing.T) {
	t.Parallel()

	_, err := Generate(context.Background(), &mockInvoker{}, GenerateRequest{
		NebulaName: "test",
	})
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}
	if !strings.Contains(err.Error(), "non-empty user prompt") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGenerate_EmptyNameError(t *testing.T) {
	t.Parallel()

	_, err := Generate(context.Background(), &mockInvoker{}, GenerateRequest{
		UserPrompt: "do something",
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "non-empty nebula name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGenerate_InvokerError(t *testing.T) {
	t.Parallel()

	mock := &mockInvoker{
		err: fmt.Errorf("API rate limit exceeded"),
	}

	_, err := Generate(context.Background(), mock, GenerateRequest{
		UserPrompt: "build something",
		NebulaName: "test",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "architect invocation failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGenerate_NoPhasesParsed(t *testing.T) {
	t.Parallel()

	mock := &mockInvoker{
		result: agent.InvocationResult{
			ResultText: "I cannot generate phases for this request.",
		},
	}

	_, err := Generate(context.Background(), mock, GenerateRequest{
		UserPrompt: "build something",
		NebulaName: "test",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no phases") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGenerate_WithAnalysis(t *testing.T) {
	t.Parallel()

	cannedOutput := `PHASE_FILE: 01-add-feature.md
+++
id = "add-feature"
title = "Add Feature"
scope = ["internal/feature/**"]
+++

## Problem

Add a feature.

## Solution

Implement it.
END_PHASE_FILE
`

	mock := &mockInvoker{
		result: agent.InvocationResult{
			ResultText: cannedOutput,
			CostUSD:    0.01,
		},
	}

	analysis := &CodebaseAnalysis{
		ModulePath: "github.com/example/project",
		Packages: []PackageSummary{
			{ImportPath: "github.com/example/project/internal/feature", RelativePath: "internal/feature"},
		},
	}

	result, err := Generate(context.Background(), mock, GenerateRequest{
		UserPrompt: "add a feature",
		NebulaName: "feature",
		WorkDir:    "/tmp/repo",
		Analysis:   analysis,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the prompt included codebase context.
	if !strings.Contains(mock.lastPrompt, "Codebase Context") {
		t.Error("expected prompt to include codebase context section")
	}

	if len(result.Phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(result.Phases))
	}
}
