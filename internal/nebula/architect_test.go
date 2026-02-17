package nebula

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aaronsalm/quasar/internal/agent"
)

// mockInvoker implements agent.Invoker for testing the architect.
type mockInvoker struct {
	result     agent.InvocationResult
	err        error
	lastAgent  agent.Agent
	lastPrompt string
}

func (m *mockInvoker) Invoke(_ context.Context, a agent.Agent, prompt string, _ string) (agent.InvocationResult, error) {
	m.lastAgent = a
	m.lastPrompt = prompt
	return m.result, m.err
}

func (m *mockInvoker) Validate() error { return nil }

func TestParseArchitectOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		output    string
		wantFile  string
		wantID    string
		wantTitle string
		wantDeps  []string
		wantBody  string
		wantErr   bool
	}{
		{
			name: "valid create output",
			output: `Some preamble text from the agent.

PHASE_FILE: implement-auth.md
+++
id = "implement-auth"
title = "Implement User Authentication"
depends_on = ["setup-database"]
+++

## Problem

We need user authentication.

## Solution

Use JWT tokens.

## Acceptance Criteria

- [ ] Users can log in
END_PHASE_FILE

Some trailing text.`,
			wantFile:  "implement-auth.md",
			wantID:    "implement-auth",
			wantTitle: "Implement User Authentication",
			wantDeps:  []string{"setup-database"},
			wantBody:  "## Problem\n\nWe need user authentication.\n\n## Solution\n\nUse JWT tokens.\n\n## Acceptance Criteria\n\n- [ ] Users can log in",
		},
		{
			name: "valid output with no dependencies",
			output: `PHASE_FILE: first-task.md
+++
id = "first-task"
title = "First Task"
+++

Do the thing.
END_PHASE_FILE`,
			wantFile:  "first-task.md",
			wantID:    "first-task",
			wantTitle: "First Task",
			wantDeps:  nil,
			wantBody:  "Do the thing.",
		},
		{
			name:    "missing PHASE_FILE marker",
			output:  "Just some text without markers",
			wantErr: true,
		},
		{
			name:    "missing END_PHASE_FILE marker",
			output:  "PHASE_FILE: test.md\n+++\nid = \"test\"\n+++\nbody",
			wantErr: true,
		},
		{
			name:    "missing frontmatter delimiters",
			output:  "PHASE_FILE: test.md\nno frontmatter\nEND_PHASE_FILE",
			wantErr: true,
		},
		{
			name:    "invalid TOML",
			output:  "PHASE_FILE: test.md\n+++\ninvalid = [toml\n+++\nbody\nEND_PHASE_FILE",
			wantErr: true,
		},
		{
			name:    "path traversal in filename",
			output:  "PHASE_FILE: ../../../etc/passwd\n+++\nid = \"test\"\ntitle = \"Test\"\n+++\nbody\nEND_PHASE_FILE",
			wantErr: true,
		},
		{
			name:    "directory separator in filename",
			output:  "PHASE_FILE: subdir/test.md\n+++\nid = \"test\"\ntitle = \"Test\"\n+++\nbody\nEND_PHASE_FILE",
			wantErr: true,
		},
		{
			name:    "filename without .md extension",
			output:  "PHASE_FILE: test.txt\n+++\nid = \"test\"\ntitle = \"Test\"\n+++\nbody\nEND_PHASE_FILE",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := parseArchitectOutput(tt.output)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Filename != tt.wantFile {
				t.Errorf("filename = %q, want %q", result.Filename, tt.wantFile)
			}
			if result.PhaseSpec.ID != tt.wantID {
				t.Errorf("id = %q, want %q", result.PhaseSpec.ID, tt.wantID)
			}
			if result.PhaseSpec.Title != tt.wantTitle {
				t.Errorf("title = %q, want %q", result.PhaseSpec.Title, tt.wantTitle)
			}
			if result.Body != tt.wantBody {
				t.Errorf("body = %q, want %q", result.Body, tt.wantBody)
			}

			// Check dependencies.
			if len(result.PhaseSpec.DependsOn) != len(tt.wantDeps) {
				t.Errorf("depends_on length = %d, want %d", len(result.PhaseSpec.DependsOn), len(tt.wantDeps))
			} else {
				for i, dep := range result.PhaseSpec.DependsOn {
					if dep != tt.wantDeps[i] {
						t.Errorf("depends_on[%d] = %q, want %q", i, dep, tt.wantDeps[i])
					}
				}
			}
		})
	}
}

func TestArchitectResultValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		result  ArchitectResult
		wantOK  bool
		wantErr string
	}{
		{
			name: "valid result",
			result: ArchitectResult{
				Filename:  "test.md",
				PhaseSpec: PhaseSpec{ID: "test", Title: "Test Phase"},
			},
			wantOK: true,
		},
		{
			name: "missing filename",
			result: ArchitectResult{
				PhaseSpec: PhaseSpec{ID: "test", Title: "Test Phase"},
			},
			wantOK:  false,
			wantErr: "missing filename",
		},
		{
			name: "missing id",
			result: ArchitectResult{
				Filename:  "test.md",
				PhaseSpec: PhaseSpec{Title: "Test Phase"},
			},
			wantOK:  false,
			wantErr: "missing phase id",
		},
		{
			name: "missing title",
			result: ArchitectResult{
				Filename:  "test.md",
				PhaseSpec: PhaseSpec{ID: "test"},
			},
			wantOK:  false,
			wantErr: "missing phase title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ok := tt.result.Validate()
			if ok != tt.wantOK {
				t.Errorf("Validate() = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantErr != "" {
				found := false
				for _, e := range tt.result.Errors {
					if strings.Contains(e, tt.wantErr) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, tt.result.Errors)
				}
			}
		})
	}
}

func TestBuildArchitectPrompt(t *testing.T) {
	t.Parallel()

	nebula := &Nebula{
		Manifest: Manifest{
			Nebula: Info{Name: "test-nebula", Description: "A test"},
			Defaults: Defaults{
				Type:     "task",
				Priority: 2,
			},
			Context: Context{
				Goals:       []string{"Build great software"},
				Constraints: []string{"Follow Go conventions"},
			},
		},
		Phases: []PhaseSpec{
			{ID: "phase-a", Title: "Phase A", Type: "task"},
			{ID: "phase-b", Title: "Phase B", Type: "task", DependsOn: []string{"phase-a"}},
		},
	}

	t.Run("create mode", func(t *testing.T) {
		t.Parallel()

		req := ArchitectRequest{
			Mode:       ArchitectModeCreate,
			UserPrompt: "Add user authentication",
			Nebula:     nebula,
		}

		prompt, err := buildArchitectPrompt(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		checks := []string{
			"test-nebula",
			"Build great software",
			"Follow Go conventions",
			"phase-a",
			"phase-b",
			"Create a New Phase",
			"Add user authentication",
		}
		for _, check := range checks {
			if !strings.Contains(prompt, check) {
				t.Errorf("prompt missing %q", check)
			}
		}
	})

	t.Run("refactor mode", func(t *testing.T) {
		t.Parallel()

		nebula := &Nebula{
			Manifest: Manifest{
				Nebula:   Info{Name: "test-nebula"},
				Defaults: Defaults{Type: "task", Priority: 2},
			},
			Phases: []PhaseSpec{
				{ID: "phase-a", Title: "Phase A", Body: "Original body content"},
			},
		}

		req := ArchitectRequest{
			Mode:       ArchitectModeRefactor,
			UserPrompt: "Add error handling",
			Nebula:     nebula,
			PhaseID:    "phase-a",
		}

		prompt, err := buildArchitectPrompt(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		checks := []string{
			"Refactor an Existing Phase",
			"phase-a",
			"Original body content",
			"Add error handling",
		}
		for _, check := range checks {
			if !strings.Contains(prompt, check) {
				t.Errorf("prompt missing %q", check)
			}
		}
	})

	t.Run("unknown mode", func(t *testing.T) {
		t.Parallel()

		req := ArchitectRequest{
			Mode:       "unknown",
			UserPrompt: "test",
			Nebula:     nebula,
		}

		_, err := buildArchitectPrompt(req)
		if err == nil {
			t.Fatal("expected error for unknown mode")
		}
	})
}

func TestRunArchitect(t *testing.T) {
	t.Parallel()

	validOutput := `PHASE_FILE: new-feature.md
+++
id = "new-feature"
title = "New Feature"
depends_on = ["phase-a"]
+++

## Problem

Need a new feature.

## Solution

Build it.
END_PHASE_FILE`

	nebula := &Nebula{
		Dir: "/tmp/test-nebula",
		Manifest: Manifest{
			Nebula:   Info{Name: "test-nebula"},
			Defaults: Defaults{Type: "task", Priority: 2},
			Execution: Execution{
				MaxBudgetUSD: 1.0,
				Model:        "sonnet",
			},
		},
		Phases: []PhaseSpec{
			{ID: "phase-a", Title: "Phase A", Type: "task"},
		},
	}

	t.Run("successful create", func(t *testing.T) {
		t.Parallel()

		invoker := &mockInvoker{
			result: agent.InvocationResult{ResultText: validOutput},
		}

		req := ArchitectRequest{
			Mode:       ArchitectModeCreate,
			UserPrompt: "Add a new feature",
			Nebula:     nebula,
		}

		result, err := RunArchitect(context.Background(), invoker, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.Filename != "new-feature.md" {
			t.Errorf("filename = %q, want %q", result.Filename, "new-feature.md")
		}
		if result.PhaseSpec.ID != "new-feature" {
			t.Errorf("id = %q, want %q", result.PhaseSpec.ID, "new-feature")
		}
		if result.PhaseSpec.Type != "task" {
			t.Errorf("type = %q, want %q (from defaults)", result.PhaseSpec.Type, "task")
		}
		if result.PhaseSpec.Priority != 2 {
			t.Errorf("priority = %d, want %d (from defaults)", result.PhaseSpec.Priority, 2)
		}
		if len(result.Errors) != 0 {
			t.Errorf("unexpected errors: %v", result.Errors)
		}

		// Verify the agent was configured correctly.
		if invoker.lastAgent.Role != agent.RoleArchitect {
			t.Errorf("agent role = %q, want %q", invoker.lastAgent.Role, agent.RoleArchitect)
		}
	})

	t.Run("nil nebula", func(t *testing.T) {
		t.Parallel()

		invoker := &mockInvoker{}
		req := ArchitectRequest{
			Mode:       ArchitectModeCreate,
			UserPrompt: "test",
		}

		_, err := RunArchitect(context.Background(), invoker, req)
		if err == nil {
			t.Fatal("expected error for nil nebula")
		}
	})

	t.Run("empty prompt", func(t *testing.T) {
		t.Parallel()

		invoker := &mockInvoker{}
		req := ArchitectRequest{
			Mode:   ArchitectModeCreate,
			Nebula: nebula,
		}

		_, err := RunArchitect(context.Background(), invoker, req)
		if err == nil {
			t.Fatal("expected error for empty prompt")
		}
	})

	t.Run("invoker error", func(t *testing.T) {
		t.Parallel()

		invoker := &mockInvoker{
			err: fmt.Errorf("connection refused"),
		}

		req := ArchitectRequest{
			Mode:       ArchitectModeCreate,
			UserPrompt: "test",
			Nebula:     nebula,
		}

		_, err := RunArchitect(context.Background(), invoker, req)
		if err == nil {
			t.Fatal("expected error from invoker")
		}
		if !strings.Contains(err.Error(), "connection refused") {
			t.Errorf("error = %q, want to contain 'connection refused'", err.Error())
		}
	})

	t.Run("duplicate phase id detected", func(t *testing.T) {
		t.Parallel()

		dupOutput := `PHASE_FILE: phase-a.md
+++
id = "phase-a"
title = "Duplicate Phase"
+++

Body.
END_PHASE_FILE`

		invoker := &mockInvoker{
			result: agent.InvocationResult{ResultText: dupOutput},
		}

		req := ArchitectRequest{
			Mode:       ArchitectModeCreate,
			UserPrompt: "test",
			Nebula:     nebula,
		}

		result, err := RunArchitect(context.Background(), invoker, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Errors) == 0 {
			t.Fatal("expected validation errors for duplicate ID")
		}

		found := false
		for _, e := range result.Errors {
			if strings.Contains(e, "duplicate phase ID") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected error about duplicate ID, got %v", result.Errors)
		}
		// Ensure no duplicate error messages (issue #2: only ValidateHotAdd should report this).
		if len(result.Errors) != 1 {
			t.Errorf("expected exactly 1 error for duplicate ID, got %d: %v", len(result.Errors), result.Errors)
		}
	})

	t.Run("unknown dependency detected", func(t *testing.T) {
		t.Parallel()

		badDepOutput := `PHASE_FILE: test.md
+++
id = "test-phase"
title = "Test Phase"
depends_on = ["nonexistent"]
+++

Body.
END_PHASE_FILE`

		invoker := &mockInvoker{
			result: agent.InvocationResult{ResultText: badDepOutput},
		}

		req := ArchitectRequest{
			Mode:       ArchitectModeCreate,
			UserPrompt: "test",
			Nebula:     nebula,
		}

		result, err := RunArchitect(context.Background(), invoker, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Errors) == 0 {
			t.Fatal("expected validation errors for unknown dependency")
		}

		found := false
		for _, e := range result.Errors {
			if strings.Contains(e, "nonexistent") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected error about unknown dep, got %v", result.Errors)
		}
	})
}

func TestValidateAgainstDAG(t *testing.T) {
	t.Parallel()

	nebula := &Nebula{
		Phases: []PhaseSpec{
			{ID: "a", Title: "A"},
			{ID: "b", Title: "B", DependsOn: []string{"a"}},
		},
	}

	t.Run("valid new phase", func(t *testing.T) {
		t.Parallel()

		result := &ArchitectResult{
			PhaseSpec: PhaseSpec{
				ID:        "c",
				Title:     "C",
				DependsOn: []string{"b"},
			},
		}

		req := ArchitectRequest{Mode: ArchitectModeCreate, Nebula: nebula}
		errs := validateAgainstDAG(result, req)
		if len(errs) != 0 {
			t.Errorf("unexpected errors: %v", errs)
		}
	})

	t.Run("would create cycle", func(t *testing.T) {
		t.Parallel()

		result := &ArchitectResult{
			PhaseSpec: PhaseSpec{
				ID:        "c",
				Title:     "C",
				DependsOn: []string{"b"},
				Blocks:    []string{"a"},
			},
		}

		req := ArchitectRequest{Mode: ArchitectModeCreate, Nebula: nebula}
		errs := validateAgainstDAG(result, req)
		if len(errs) == 0 {
			t.Fatal("expected cycle error")
		}
	})

	t.Run("create mode duplicate id produces single error", func(t *testing.T) {
		t.Parallel()

		result := &ArchitectResult{
			PhaseSpec: PhaseSpec{
				ID:    "a",
				Title: "Duplicate A",
			},
		}

		req := ArchitectRequest{Mode: ArchitectModeCreate, Nebula: nebula}
		errs := validateAgainstDAG(result, req)
		if len(errs) != 1 {
			t.Errorf("expected exactly 1 error for duplicate ID, got %d: %v", len(errs), errs)
		}
		if len(errs) > 0 && !strings.Contains(errs[0], "duplicate phase ID") {
			t.Errorf("expected duplicate phase ID error, got: %v", errs[0])
		}
	})

	t.Run("refactor mode allows same id", func(t *testing.T) {
		t.Parallel()

		result := &ArchitectResult{
			PhaseSpec: PhaseSpec{
				ID:    "a",
				Title: "Updated A",
			},
		}

		req := ArchitectRequest{Mode: ArchitectModeRefactor, Nebula: nebula, PhaseID: "a"}
		errs := validateAgainstDAG(result, req)
		if len(errs) != 0 {
			t.Errorf("expected no errors for refactor mode, got: %v", errs)
		}
	})
}

func TestApplyDefaults(t *testing.T) {
	t.Parallel()

	defaults := Defaults{
		Type:     "task",
		Priority: 2,
		Labels:   []string{"default-label"},
		Assignee: "default-user",
	}

	t.Run("fills empty fields", func(t *testing.T) {
		t.Parallel()

		spec := PhaseSpec{ID: "test", Title: "Test"}
		applyDefaults(&spec, defaults)

		if spec.Type != "task" {
			t.Errorf("type = %q, want %q", spec.Type, "task")
		}
		if spec.Priority != 2 {
			t.Errorf("priority = %d, want %d", spec.Priority, 2)
		}
		if len(spec.Labels) != 1 || spec.Labels[0] != "default-label" {
			t.Errorf("labels = %v, want [default-label]", spec.Labels)
		}
		if spec.Assignee != "default-user" {
			t.Errorf("assignee = %q, want %q", spec.Assignee, "default-user")
		}
	})

	t.Run("preserves set fields", func(t *testing.T) {
		t.Parallel()

		spec := PhaseSpec{
			ID:       "test",
			Title:    "Test",
			Type:     "feature",
			Priority: 1,
			Labels:   []string{"custom"},
			Assignee: "someone",
		}
		applyDefaults(&spec, defaults)

		if spec.Type != "feature" {
			t.Errorf("type = %q, want %q", spec.Type, "feature")
		}
		if spec.Priority != 1 {
			t.Errorf("priority = %d, want %d", spec.Priority, 1)
		}
		if spec.Labels[0] != "custom" {
			t.Errorf("labels = %v, want [custom]", spec.Labels)
		}
		if spec.Assignee != "someone" {
			t.Errorf("assignee = %q, want %q", spec.Assignee, "someone")
		}
	})
}

func TestArchitectAgent(t *testing.T) {
	t.Parallel()

	a := ArchitectAgent(5.0, "opus")

	if a.Role != agent.RoleArchitect {
		t.Errorf("role = %q, want %q", a.Role, agent.RoleArchitect)
	}
	if a.MaxBudgetUSD != 5.0 {
		t.Errorf("budget = %f, want %f", a.MaxBudgetUSD, 5.0)
	}
	if a.Model != "opus" {
		t.Errorf("model = %q, want %q", a.Model, "opus")
	}
	if a.SystemPrompt == "" {
		t.Error("system prompt should not be empty")
	}
}
