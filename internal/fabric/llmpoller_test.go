package fabric

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/papapumpkin/quasar/internal/agent"
)

// mockInvoker satisfies agent.Invoker for testing the LLMPoller.
type mockInvoker struct {
	response string
	err      error
	// captured records the prompt passed to Invoke for assertion.
	captured string
}

func (m *mockInvoker) Invoke(_ context.Context, _ agent.Agent, prompt string, _ string) (agent.InvocationResult, error) {
	m.captured = prompt
	if m.err != nil {
		return agent.InvocationResult{}, m.err
	}
	return agent.InvocationResult{ResultText: m.response}, nil
}

func (m *mockInvoker) Validate() error { return nil }

func TestLLMPollerSatisfiesInterface(t *testing.T) {
	t.Parallel()
	// Compile-time check that LLMPoller implements Poller.
	var _ Poller = (*LLMPoller)(nil)
}

func TestLLMPollerPoll(t *testing.T) {
	t.Parallel()

	phases := map[string]*PhaseSpec{
		"phase-db": {ID: "phase-db", Body: "Implement database layer"},
	}

	snap := Snapshot{
		Entanglements: []Entanglement{
			{ID: 1, Producer: "phase-auth", Kind: KindInterface, Name: "Authenticator", Package: "auth"},
		},
		Completed:  []string{"phase-auth"},
		InProgress: []string{"phase-api"},
	}

	t.Run("proceed", func(t *testing.T) {
		t.Parallel()
		inv := &mockInvoker{response: "PROCEED All types are available."}
		poller := &LLMPoller{Invoker: inv, Phases: phases}

		result, err := poller.Poll(context.Background(), "phase-db", snap)
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		if result.Decision != PollProceed {
			t.Errorf("Decision = %q, want %q", result.Decision, PollProceed)
		}
		if !strings.Contains(result.Reason, "All types are available") {
			t.Errorf("Reason = %q, want containing 'All types are available'", result.Reason)
		}
	})

	t.Run("need_info with bullets", func(t *testing.T) {
		t.Parallel()
		response := "NEED_INFO Missing entanglements:\n- UserRepository interface\n- DatabaseConfig type"
		inv := &mockInvoker{response: response}
		poller := &LLMPoller{Invoker: inv, Phases: phases}

		result, err := poller.Poll(context.Background(), "phase-db", snap)
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		if result.Decision != PollNeedInfo {
			t.Errorf("Decision = %q, want %q", result.Decision, PollNeedInfo)
		}
		if len(result.MissingInfo) != 2 {
			t.Fatalf("MissingInfo length = %d, want 2", len(result.MissingInfo))
		}
		if result.MissingInfo[0] != "UserRepository interface" {
			t.Errorf("MissingInfo[0] = %q, want %q", result.MissingInfo[0], "UserRepository interface")
		}
		if result.MissingInfo[1] != "DatabaseConfig type" {
			t.Errorf("MissingInfo[1] = %q, want %q", result.MissingInfo[1], "DatabaseConfig type")
		}
	})

	t.Run("conflict with backtick target", func(t *testing.T) {
		t.Parallel()
		response := "CONFLICT File claim conflict on `internal/auth/auth.go` with `phase-auth`"
		inv := &mockInvoker{response: response}
		poller := &LLMPoller{Invoker: inv, Phases: phases}

		result, err := poller.Poll(context.Background(), "phase-db", snap)
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		if result.Decision != PollConflict {
			t.Errorf("Decision = %q, want %q", result.Decision, PollConflict)
		}
		if result.ConflictWith != "internal/auth/auth.go" {
			t.Errorf("ConflictWith = %q, want %q", result.ConflictWith, "internal/auth/auth.go")
		}
	})

	t.Run("unknown phase returns error", func(t *testing.T) {
		t.Parallel()
		inv := &mockInvoker{response: "PROCEED"}
		poller := &LLMPoller{Invoker: inv, Phases: phases}

		_, err := poller.Poll(context.Background(), "nonexistent", snap)
		if err == nil {
			t.Fatal("expected error for unknown phase, got nil")
		}
		if !strings.Contains(err.Error(), "unknown phase") {
			t.Errorf("error = %q, want containing 'unknown phase'", err.Error())
		}
	})

	t.Run("invoker error propagates", func(t *testing.T) {
		t.Parallel()
		inv := &mockInvoker{err: errors.New("LLM unavailable")}
		poller := &LLMPoller{Invoker: inv, Phases: phases}

		_, err := poller.Poll(context.Background(), "phase-db", snap)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "LLM unavailable") {
			t.Errorf("error = %q, want containing 'LLM unavailable'", err.Error())
		}
	})

	t.Run("prompt contains phase body and fabric snapshot", func(t *testing.T) {
		t.Parallel()
		inv := &mockInvoker{response: "PROCEED"}
		poller := &LLMPoller{Invoker: inv, Phases: phases}

		_, err := poller.Poll(context.Background(), "phase-db", snap)
		if err != nil {
			t.Fatalf("Poll: %v", err)
		}
		if !strings.Contains(inv.captured, "Implement database layer") {
			t.Error("prompt should contain phase body")
		}
		if !strings.Contains(inv.captured, "Fabric State") {
			t.Error("prompt should contain fabric state")
		}
		if !strings.Contains(inv.captured, "structural blockers") {
			t.Error("prompt should instruct to only block on structural issues")
		}
	})
}

func TestParseResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		raw          string
		wantDecision PollDecision
		wantMissing  int
		wantConflict string
	}{
		{
			name:         "simple proceed",
			raw:          "PROCEED",
			wantDecision: PollProceed,
		},
		{
			name:         "proceed with reason",
			raw:          "PROCEED — All interfaces are available on the fabric.",
			wantDecision: PollProceed,
		},
		{
			name:         "need_info no bullets",
			raw:          "NEED_INFO Missing the UserService type.",
			wantDecision: PollNeedInfo,
			wantMissing:  0,
		},
		{
			name:         "need_info with bullets",
			raw:          "NEED_INFO Missing:\n- UserService interface\n- Config struct\n- Logger interface",
			wantDecision: PollNeedInfo,
			wantMissing:  3,
		},
		{
			name:         "need_info with star bullets",
			raw:          "NEED_INFO Missing:\n* AuthProvider\n* TokenValidator",
			wantDecision: PollNeedInfo,
			wantMissing:  2,
		},
		{
			name:         "conflict with backtick",
			raw:          "CONFLICT The file `internal/db/store.go` is claimed by `phase-db`",
			wantDecision: PollConflict,
			wantConflict: "internal/db/store.go",
		},
		{
			name:         "conflict with preposition",
			raw:          "CONFLICT File conflict with phase-auth on auth.go",
			wantDecision: PollConflict,
			wantConflict: "phase-auth",
		},
		{
			name:         "empty response defaults to proceed",
			raw:          "",
			wantDecision: PollProceed,
		},
		{
			name:         "whitespace only defaults to proceed",
			raw:          "   \n  \t  ",
			wantDecision: PollProceed,
		},
		{
			name:         "malformed response defaults to proceed",
			raw:          "I think you should wait for more info",
			wantDecision: PollProceed,
		},
		{
			name:         "lowercase proceed defaults to proceed via uppercase",
			raw:          "proceed all good",
			wantDecision: PollProceed,
		},
		{
			name:         "lowercase need_info parses correctly",
			raw:          "need_info Missing:\n- SomeType",
			wantDecision: PollNeedInfo,
			wantMissing:  1,
		},
		{
			name:         "mixed case conflict",
			raw:          "Conflict with `phase-x`",
			wantDecision: PollConflict,
			wantConflict: "phase-x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := parseResponse(tt.raw)
			if result.Decision != tt.wantDecision {
				t.Errorf("Decision = %q, want %q", result.Decision, tt.wantDecision)
			}
			if tt.wantMissing > 0 && len(result.MissingInfo) != tt.wantMissing {
				t.Errorf("MissingInfo length = %d, want %d (items: %v)", len(result.MissingInfo), tt.wantMissing, result.MissingInfo)
			}
			if tt.wantConflict != "" && result.ConflictWith != tt.wantConflict {
				t.Errorf("ConflictWith = %q, want %q", result.ConflictWith, tt.wantConflict)
			}
		})
	}
}

func TestBuildPollPrompt(t *testing.T) {
	t.Parallel()

	body := "Implement the user authentication service."
	snap := Snapshot{
		Entanglements: []Entanglement{
			{ID: 1, Producer: "phase-core", Kind: KindType, Name: "User", Package: "models"},
		},
		Completed:  []string{"phase-core"},
		InProgress: []string{"phase-api"},
	}

	prompt := buildPollPrompt(body, snap)

	checks := []struct {
		name string
		want string
	}{
		{"contains phase body", "Implement the user authentication service."},
		{"contains fabric state header", "## Fabric State"},
		{"contains completed phases", "phase-core"},
		{"contains in-progress phases", "phase-api"},
		{"contains proceed instruction", "PROCEED"},
		{"contains need_info instruction", "NEED_INFO"},
		{"contains conflict instruction", "CONFLICT"},
		{"contains structural blockers instruction", "structural blockers"},
		{"contains fail-open guidance", "reasonable assumption"},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(prompt, c.want) {
				t.Errorf("prompt missing %q", c.want)
			}
		})
	}
}

func TestExtractBullets(t *testing.T) {
	t.Parallel()

	t.Run("dash bullets", func(t *testing.T) {
		t.Parallel()
		items := extractBullets("Missing:\n- Foo\n- Bar\nSome trailing text")
		if len(items) != 2 {
			t.Fatalf("got %d items, want 2", len(items))
		}
		if items[0] != "Foo" {
			t.Errorf("items[0] = %q, want %q", items[0], "Foo")
		}
		if items[1] != "Bar" {
			t.Errorf("items[1] = %q, want %q", items[1], "Bar")
		}
	})

	t.Run("star bullets", func(t *testing.T) {
		t.Parallel()
		items := extractBullets("* Alpha\n* Beta")
		if len(items) != 2 {
			t.Fatalf("got %d items, want 2", len(items))
		}
	})

	t.Run("no bullets", func(t *testing.T) {
		t.Parallel()
		items := extractBullets("Just a plain sentence.")
		if len(items) != 0 {
			t.Errorf("got %d items, want 0", len(items))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		t.Parallel()
		items := extractBullets("")
		if len(items) != 0 {
			t.Errorf("got %d items, want 0", len(items))
		}
	})
}

func TestExtractConflictTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		reason string
		want   string
	}{
		{
			name:   "backtick target",
			reason: "File `internal/auth.go` is claimed",
			want:   "internal/auth.go",
		},
		{
			name:   "with preposition",
			reason: "conflict with phase-auth on the file",
			want:   "phase-auth",
		},
		{
			name:   "on preposition",
			reason: "file claim conflict on auth.go by phase-x",
			want:   "auth.go",
		},
		{
			name:   "by preposition",
			reason: "claimed by phase-owner",
			want:   "phase-owner",
		},
		{
			name:   "no identifiable target",
			reason: "there is a conflict",
			want:   "",
		},
		{
			name:   "backtick takes precedence over preposition",
			reason: "conflict with `phase-real` not phase-fake",
			want:   "phase-real",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractConflictTarget(tt.reason)
			if got != tt.want {
				t.Errorf("extractConflictTarget(%q) = %q, want %q", tt.reason, got, tt.want)
			}
		})
	}
}
