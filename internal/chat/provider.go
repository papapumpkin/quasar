package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/papapumpkin/quasar/internal/agent"
)

// Provider abstracts multi-turn chat interaction with an AI model.
// Implementations adapt a specific backend (e.g. Claude CLI) for
// conversational use, taking the full message history each call.
type Provider interface {
	// Chat sends the conversation history to the model and returns the
	// complete response synchronously.
	Chat(ctx context.Context, messages []Message, model string) (string, error)

	// ChatStream sends the conversation history and returns two channels:
	// a string channel that delivers response chunks as they arrive, and
	// an error channel that receives at most one error (or is closed on
	// success). Both channels are closed when the response is complete.
	ChatStream(ctx context.Context, messages []Message, model string) (<-chan string, <-chan error)
}

// chatSystemPrompt is the default system prompt used for chat conversations.
const chatSystemPrompt = "You are a helpful AI assistant. Respond clearly and concisely."

// ClaudeProvider implements Provider using agent.Invoker (Claude CLI).
// It serializes conversation history into a prompt format suitable for
// the Claude CLI's -p flag.
type ClaudeProvider struct {
	invoker agent.Invoker
	workDir string
}

// NewClaudeProvider creates a ClaudeProvider backed by the given invoker.
// The workDir is passed to the invoker as the working directory for
// subprocess execution.
func NewClaudeProvider(invoker agent.Invoker, workDir string) *ClaudeProvider {
	return &ClaudeProvider{
		invoker: invoker,
		workDir: workDir,
	}
}

// Chat sends the full conversation history to Claude and returns the
// model's response. The model parameter selects which Claude model to use;
// an empty string uses the invoker's default.
func (p *ClaudeProvider) Chat(ctx context.Context, messages []Message, model string) (string, error) {
	prompt := BuildPrompt(messages)
	if prompt == "" {
		return "", fmt.Errorf("no messages to send")
	}

	a := agent.Agent{
		SystemPrompt: chatSystemPrompt,
		Model:        model,
	}

	result, err := p.invoker.Invoke(ctx, a, prompt, p.workDir)
	if err != nil {
		return "", fmt.Errorf("chat invocation failed: %w", err)
	}

	return result.ResultText, nil
}

// ChatStream sends the conversation history and returns channels for
// streaming the response. Because agent.Invoker is synchronous (waits
// for the full CLI response), this implementation delivers the entire
// response as a single chunk on the string channel.
//
// The error channel receives at most one error; both channels are closed
// when the operation completes.
func (p *ClaudeProvider) ChatStream(ctx context.Context, messages []Message, model string) (<-chan string, <-chan error) {
	chunks := make(chan string, 1)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)

		result, err := p.Chat(ctx, messages, model)
		if err != nil {
			errs <- err
			return
		}
		chunks <- result
	}()

	return chunks, errs
}

// BuildPrompt serializes a slice of chat messages into a single prompt
// string for the Claude CLI. Each message is formatted as a labeled block
// so Claude can distinguish between user, assistant, and system turns.
//
// System messages are placed as [system] preamble blocks. User and
// assistant messages alternate as [user] and [assistant] blocks.
// An empty slice returns an empty string.
func BuildPrompt(messages []Message) string {
	if len(messages) == 0 {
		return ""
	}

	var b strings.Builder
	for i, msg := range messages {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(roleLabel(msg.Role))
		b.WriteString("\n")
		b.WriteString(msg.Content)
	}
	return b.String()
}

// roleLabel returns the bracketed label for a message role.
func roleLabel(r Role) string {
	switch r {
	case RoleUser:
		return "[user]"
	case RoleAssistant:
		return "[assistant]"
	case RoleSystem:
		return "[system]"
	default:
		return "[unknown]"
	}
}
