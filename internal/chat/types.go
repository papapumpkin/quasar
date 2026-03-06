// Package chat provides types and persistence for user-initiated chat
// conversations with AI models. Unlike the dialog package (which handles
// agent-human escalation), this package manages full conversational threads
// that the user starts directly via the TUI.
package chat

import "time"

// Role identifies the sender of a chat message.
type Role string

const (
	// RoleUser represents a message sent by the human user.
	RoleUser Role = "user"
	// RoleAssistant represents a message sent by the AI model.
	RoleAssistant Role = "assistant"
	// RoleSystem represents a system-level instruction or context.
	RoleSystem Role = "system"
)

// maxAutoTitleLen is the maximum character length for auto-generated titles.
const maxAutoTitleLen = 72

// Message is a single entry in a chat conversation.
type Message struct {
	Role      Role      `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Conversation holds metadata and the full message history for a single
// chat thread. Each conversation is persisted as a JSON file.
type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Model     string    `json:"model,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Messages  []Message `json:"messages"`
}

// AutoTitle generates a conversation title from the first user message.
// If the conversation already has a non-empty title, it is returned unchanged.
// If no user messages exist, "New conversation" is returned.
func (c *Conversation) AutoTitle() string {
	if c.Title != "" {
		return c.Title
	}
	for _, m := range c.Messages {
		if m.Role == RoleUser {
			c.Title = truncateTitle(m.Content)
			return c.Title
		}
	}
	c.Title = "New conversation"
	return c.Title
}

// truncateTitle shortens s to maxAutoTitleLen runes, appending an ellipsis
// if truncation occurs. Leading/trailing whitespace is stripped, and internal
// newlines are replaced with spaces.
func truncateTitle(s string) string {
	// Normalise whitespace.
	cleaned := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		}
		cleaned = append(cleaned, r)
	}

	// Trim leading/trailing spaces.
	start, end := 0, len(cleaned)
	for start < end && cleaned[start] == ' ' {
		start++
	}
	for end > start && cleaned[end-1] == ' ' {
		end--
	}
	cleaned = cleaned[start:end]

	if len(cleaned) <= maxAutoTitleLen {
		return string(cleaned)
	}
	return string(cleaned[:maxAutoTitleLen-1]) + "\u2026"
}
