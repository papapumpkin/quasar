package chat

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/papapumpkin/quasar/internal/agent"
)

// ---------------------------------------------------------------------------
// fakeInvoker implements agent.Invoker for testing the provider.
// ---------------------------------------------------------------------------

type fakeInvoker struct {
	mu       sync.Mutex
	result   agent.InvocationResult
	err      error
	prompts  []string
	agents   []agent.Agent
	workDirs []string
}

func (f *fakeInvoker) Invoke(_ context.Context, a agent.Agent, prompt string, workDir string) (agent.InvocationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts = append(f.prompts, prompt)
	f.agents = append(f.agents, a)
	f.workDirs = append(f.workDirs, workDir)
	return f.result, f.err
}

func (f *fakeInvoker) Validate() error { return nil }

// ---------------------------------------------------------------------------
// BuildPrompt tests
// ---------------------------------------------------------------------------

func TestBuildPrompt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		messages []Message
		want     string
	}{
		{
			name:     "empty messages returns empty string",
			messages: nil,
			want:     "",
		},
		{
			name: "single user message",
			messages: []Message{
				{Role: RoleUser, Content: "Hello"},
			},
			want: "[user]\nHello",
		},
		{
			name: "user and assistant exchange",
			messages: []Message{
				{Role: RoleUser, Content: "What is Go?"},
				{Role: RoleAssistant, Content: "Go is a programming language."},
			},
			want: "[user]\nWhat is Go?\n\n[assistant]\nGo is a programming language.",
		},
		{
			name: "system then user then assistant",
			messages: []Message{
				{Role: RoleSystem, Content: "You are helpful."},
				{Role: RoleUser, Content: "Hi"},
				{Role: RoleAssistant, Content: "Hello!"},
			},
			want: "[system]\nYou are helpful.\n\n[user]\nHi\n\n[assistant]\nHello!",
		},
		{
			name: "multi-turn conversation",
			messages: []Message{
				{Role: RoleUser, Content: "First question"},
				{Role: RoleAssistant, Content: "First answer"},
				{Role: RoleUser, Content: "Follow-up"},
				{Role: RoleAssistant, Content: "Follow-up answer"},
				{Role: RoleUser, Content: "One more"},
			},
			want: "[user]\nFirst question\n\n[assistant]\nFirst answer\n\n[user]\nFollow-up\n\n[assistant]\nFollow-up answer\n\n[user]\nOne more",
		},
		{
			name: "multiline content preserved",
			messages: []Message{
				{Role: RoleUser, Content: "line 1\nline 2\nline 3"},
			},
			want: "[user]\nline 1\nline 2\nline 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := BuildPrompt(tt.messages)
			if got != tt.want {
				t.Errorf("BuildPrompt() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// roleLabel tests
// ---------------------------------------------------------------------------

func TestRoleLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role Role
		want string
	}{
		{RoleUser, "[user]"},
		{RoleAssistant, "[assistant]"},
		{RoleSystem, "[system]"},
		{Role("other"), "[unknown]"},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			t.Parallel()
			got := roleLabel(tt.role)
			if got != tt.want {
				t.Errorf("roleLabel(%q) = %q, want %q", tt.role, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Chat tests
// ---------------------------------------------------------------------------

func TestChat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		messages   []Message
		model      string
		invokerRes agent.InvocationResult
		invokerErr error
		want       string
		wantErr    string
	}{
		{
			name: "successful response",
			messages: []Message{
				{Role: RoleUser, Content: "Hello"},
			},
			model:      "claude-3-sonnet",
			invokerRes: agent.InvocationResult{ResultText: "Hi there!"},
			want:       "Hi there!",
		},
		{
			name: "model passed through to agent",
			messages: []Message{
				{Role: RoleUser, Content: "test"},
			},
			model:      "claude-3-opus",
			invokerRes: agent.InvocationResult{ResultText: "ok"},
			want:       "ok",
		},
		{
			name: "empty model uses default",
			messages: []Message{
				{Role: RoleUser, Content: "test"},
			},
			model:      "",
			invokerRes: agent.InvocationResult{ResultText: "ok"},
			want:       "ok",
		},
		{
			name:     "empty messages returns error",
			messages: nil,
			wantErr:  "no messages to send",
		},
		{
			name: "invoker error propagated",
			messages: []Message{
				{Role: RoleUser, Content: "Hello"},
			},
			invokerErr: fmt.Errorf("connection timeout"),
			wantErr:    "chat invocation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inv := &fakeInvoker{
				result: tt.invokerRes,
				err:    tt.invokerErr,
			}
			provider := NewClaudeProvider(inv, "/test/dir")

			got, err := provider.Chat(context.Background(), tt.messages, tt.model)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Chat() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChat_PromptConstruction(t *testing.T) {
	t.Parallel()

	messages := []Message{
		{Role: RoleSystem, Content: "Be concise."},
		{Role: RoleUser, Content: "What is Go?"},
		{Role: RoleAssistant, Content: "A programming language."},
		{Role: RoleUser, Content: "Tell me more."},
	}

	inv := &fakeInvoker{
		result: agent.InvocationResult{ResultText: "ok"},
	}
	provider := NewClaudeProvider(inv, "/work")

	_, err := provider.Chat(context.Background(), messages, "test-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	inv.mu.Lock()
	defer inv.mu.Unlock()

	if len(inv.prompts) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(inv.prompts))
	}

	prompt := inv.prompts[0]
	// Verify all messages are in the prompt.
	if !strings.Contains(prompt, "[system]\nBe concise.") {
		t.Error("prompt missing system message")
	}
	if !strings.Contains(prompt, "[user]\nWhat is Go?") {
		t.Error("prompt missing first user message")
	}
	if !strings.Contains(prompt, "[assistant]\nA programming language.") {
		t.Error("prompt missing assistant message")
	}
	if !strings.Contains(prompt, "[user]\nTell me more.") {
		t.Error("prompt missing second user message")
	}

	// Verify the agent was configured correctly.
	a := inv.agents[0]
	if a.Model != "test-model" {
		t.Errorf("agent.Model = %q, want %q", a.Model, "test-model")
	}
	if a.SystemPrompt != chatSystemPrompt {
		t.Errorf("agent.SystemPrompt = %q, want %q", a.SystemPrompt, chatSystemPrompt)
	}
}

func TestChat_WorkDirPassedThrough(t *testing.T) {
	t.Parallel()

	inv := &fakeInvoker{
		result: agent.InvocationResult{ResultText: "ok"},
	}
	provider := NewClaudeProvider(inv, "/my/project")

	_, err := provider.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "test"},
	}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	inv.mu.Lock()
	defer inv.mu.Unlock()

	if len(inv.workDirs) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(inv.workDirs))
	}
	if inv.workDirs[0] != "/my/project" {
		t.Errorf("workDir = %q, want %q", inv.workDirs[0], "/my/project")
	}
}

// ---------------------------------------------------------------------------
// ChatStream tests
// ---------------------------------------------------------------------------

func TestChatStream(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		messages   []Message
		invokerRes agent.InvocationResult
		invokerErr error
		wantChunk  string
		wantErr    string
	}{
		{
			name: "successful stream delivers full response",
			messages: []Message{
				{Role: RoleUser, Content: "Hello"},
			},
			invokerRes: agent.InvocationResult{ResultText: "Hi there!"},
			wantChunk:  "Hi there!",
		},
		{
			name: "error delivered on error channel",
			messages: []Message{
				{Role: RoleUser, Content: "Hello"},
			},
			invokerErr: fmt.Errorf("model unavailable"),
			wantErr:    "chat invocation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inv := &fakeInvoker{
				result: tt.invokerRes,
				err:    tt.invokerErr,
			}
			provider := NewClaudeProvider(inv, "/test")

			chunks, errs := provider.ChatStream(context.Background(), tt.messages, "")

			if tt.wantErr != "" {
				select {
				case err := <-errs:
					if err == nil {
						t.Fatal("expected error, got nil")
					}
					if !strings.Contains(err.Error(), tt.wantErr) {
						t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErr)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("timed out waiting for error")
				}
				return
			}

			var received []string
			for chunk := range chunks {
				received = append(received, chunk)
			}

			if len(received) != 1 {
				t.Fatalf("expected 1 chunk, got %d", len(received))
			}
			if received[0] != tt.wantChunk {
				t.Errorf("chunk = %q, want %q", received[0], tt.wantChunk)
			}

			// Error channel should be closed with no errors.
			err, ok := <-errs
			if ok && err != nil {
				t.Errorf("unexpected error on error channel: %v", err)
			}
		})
	}
}

func TestChatStream_ChannelsClosed(t *testing.T) {
	t.Parallel()

	inv := &fakeInvoker{
		result: agent.InvocationResult{ResultText: "done"},
	}
	provider := NewClaudeProvider(inv, "/test")

	chunks, errs := provider.ChatStream(context.Background(), []Message{
		{Role: RoleUser, Content: "test"},
	}, "")

	// Drain chunks.
	for range chunks {
	}

	// Both channels should be closed.
	select {
	case _, ok := <-chunks:
		if ok {
			t.Error("chunks channel should be closed")
		}
	default:
		// Already closed, good.
	}

	select {
	case <-errs:
		// Channel closed or value received — either is acceptable.
	case <-time.After(2 * time.Second):
		t.Error("errs channel was not closed")
	}
}

func TestChatStream_EmptyMessages(t *testing.T) {
	t.Parallel()

	inv := &fakeInvoker{}
	provider := NewClaudeProvider(inv, "/test")

	_, errs := provider.ChatStream(context.Background(), nil, "")

	select {
	case err := <-errs:
		if err == nil {
			t.Fatal("expected error for empty messages, got nil")
		}
		if !strings.Contains(err.Error(), "no messages to send") {
			t.Errorf("error = %q, want containing %q", err.Error(), "no messages to send")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for error")
	}
}

// ---------------------------------------------------------------------------
// NewClaudeProvider tests
// ---------------------------------------------------------------------------

func TestNewClaudeProvider(t *testing.T) {
	t.Parallel()

	inv := &fakeInvoker{}
	p := NewClaudeProvider(inv, "/work/dir")

	if p.invoker != inv {
		t.Error("invoker not set correctly")
	}
	if p.workDir != "/work/dir" {
		t.Errorf("workDir = %q, want %q", p.workDir, "/work/dir")
	}
}

// ---------------------------------------------------------------------------
// Provider interface compliance
// ---------------------------------------------------------------------------

var _ Provider = (*ClaudeProvider)(nil)
