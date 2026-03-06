package tui

import "github.com/papapumpkin/quasar/internal/chat"

// Chat mode message types — sent by ChatModel to coordinate sidebar,
// chat view, and store interactions.

// MsgChatChunk delivers a single streaming token from the AI model.
// The ChatModel appends the content to the in-progress assistant message
// and issues a command to read the next chunk from the stream.
type MsgChatChunk struct {
	ConversationID string
	Content        string
}

// MsgChatResponse delivers a complete AI response (non-streaming path).
// The ChatModel appends the content to the active conversation's latest
// assistant message.
type MsgChatResponse struct {
	ConversationID string
	Content        string
}

// MsgChatDone signals that inference is complete for the active conversation.
// The ChatModel saves the conversation via the store and clears the loading
// state.
type MsgChatDone struct {
	ConversationID string
	Err            error
}

// MsgChatError delivers an inference error for inline display. The ChatModel
// renders it as a system message and cleans up streaming state.
type MsgChatError struct {
	ConversationID string
	Err            error
}

// MsgConvLoaded delivers a conversation loaded from the store. The ChatModel
// populates the chat view with the conversation's messages and metadata.
type MsgConvLoaded struct {
	Conversation *chat.Conversation
	Err          error
}

// MsgConvListUpdated delivers a refreshed conversation list from the store.
// The ChatModel updates the sidebar with the new list.
type MsgConvListUpdated struct {
	Conversations []chat.Conversation
	Err           error
}

// MsgConvDeleted signals that a conversation was deleted from the store.
type MsgConvDeleted struct {
	ConversationID string
	Err            error
}

// MsgConvSaved signals that a conversation was persisted to the store.
type MsgConvSaved struct {
	ConversationID string
	Err            error
}
