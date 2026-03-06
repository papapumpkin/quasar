package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestStore creates a FileStore rooted in a temporary directory.
func newTestStore(t *testing.T) *FileStore {
	t.Helper()
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore(%q): %v", dir, err)
	}
	return fs
}

// sampleConversation returns a conversation with one user and one assistant message.
func sampleConversation(title string) *Conversation {
	now := time.Now()
	return &Conversation{
		Title: title,
		Model: "claude-3",
		Messages: []Message{
			{Role: RoleUser, Content: "Hello", Timestamp: now},
			{Role: RoleAssistant, Content: "Hi there!", Timestamp: now.Add(time.Second)},
		},
	}
}

func TestNewFileStore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T) string
		wantErr bool
	}{
		{
			name: "creates new directory",
			setup: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "new", "nested", "dir")
			},
		},
		{
			name: "existing directory is fine",
			setup: func(t *testing.T) string {
				return t.TempDir()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := tt.setup(t)
			fs, err := NewFileStore(dir)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fs.dir != dir {
				t.Errorf("fs.dir = %q, want %q", fs.dir, dir)
			}
			// Verify directory exists.
			info, err := os.Stat(dir)
			if err != nil {
				t.Fatalf("directory not created: %v", err)
			}
			if !info.IsDir() {
				t.Error("path is not a directory")
			}
		})
	}
}

func TestSave(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		conv      *Conversation
		wantTitle string
		checkID   bool
	}{
		{
			name:      "assigns ID when empty",
			conv:      sampleConversation(""),
			wantTitle: "Hello",
			checkID:   true,
		},
		{
			name:      "preserves explicit title",
			conv:      sampleConversation("My Chat"),
			wantTitle: "My Chat",
		},
		{
			name: "auto-titles from first user message",
			conv: &Conversation{
				Messages: []Message{
					{Role: RoleSystem, Content: "You are helpful"},
					{Role: RoleUser, Content: "What is Go?", Timestamp: time.Now()},
				},
			},
			wantTitle: "What is Go?",
			checkID:   true,
		},
		{
			name: "preserves existing ID",
			conv: &Conversation{
				ID:    "my-custom-id",
				Title: "custom",
				Messages: []Message{
					{Role: RoleUser, Content: "hi", Timestamp: time.Now()},
				},
			},
			wantTitle: "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := newTestStore(t)

			if err := fs.Save(tt.conv); err != nil {
				t.Fatalf("Save: %v", err)
			}

			if tt.checkID && tt.conv.ID == "" {
				t.Error("expected ID to be assigned, got empty")
			}
			if !strings.HasPrefix(tt.conv.ID, "chat-") && tt.conv.ID != "my-custom-id" {
				t.Errorf("ID = %q, want prefix %q", tt.conv.ID, "chat-")
			}
			if tt.conv.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", tt.conv.Title, tt.wantTitle)
			}
			if tt.conv.UpdatedAt.IsZero() {
				t.Error("UpdatedAt should be set")
			}
			if tt.conv.CreatedAt.IsZero() {
				t.Error("CreatedAt should be set")
			}

			// Verify the file exists on disk.
			path := fs.path(tt.conv.ID)
			if _, err := os.Stat(path); err != nil {
				t.Errorf("conversation file not found: %v", err)
			}
		})
	}
}

func TestSave_PreservesCreatedAt(t *testing.T) {
	t.Parallel()
	fs := newTestStore(t)

	created := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	conv := &Conversation{
		Title:     "old chat",
		CreatedAt: created,
		Messages:  []Message{{Role: RoleUser, Content: "hi", Timestamp: time.Now()}},
	}

	if err := fs.Save(conv); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !conv.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt changed: got %v, want %v", conv.CreatedAt, created)
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T, fs *FileStore) string // returns ID
		wantErr string
	}{
		{
			name: "loads saved conversation",
			setup: func(t *testing.T, fs *FileStore) string {
				conv := sampleConversation("test chat")
				if err := fs.Save(conv); err != nil {
					t.Fatalf("Save: %v", err)
				}
				return conv.ID
			},
		},
		{
			name: "missing file returns ErrNotFound",
			setup: func(_ *testing.T, _ *FileStore) string {
				return "nonexistent-id"
			},
			wantErr: "conversation not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := newTestStore(t)
			id := tt.setup(t, fs)

			conv, err := fs.Load(id)

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
			if conv.ID != id {
				t.Errorf("ID = %q, want %q", conv.ID, id)
			}
			if conv.Title != "test chat" {
				t.Errorf("Title = %q, want %q", conv.Title, "test chat")
			}
			if len(conv.Messages) != 2 {
				t.Errorf("len(Messages) = %d, want 2", len(conv.Messages))
			}
			if conv.Messages[0].Role != RoleUser {
				t.Errorf("Messages[0].Role = %q, want %q", conv.Messages[0].Role, RoleUser)
			}
			if conv.Messages[1].Role != RoleAssistant {
				t.Errorf("Messages[1].Role = %q, want %q", conv.Messages[1].Role, RoleAssistant)
			}
		})
	}
}

func TestLoad_CorruptFile(t *testing.T) {
	t.Parallel()
	fs := newTestStore(t)

	// Write invalid JSON directly to disk.
	path := filepath.Join(fs.dir, "bad-id.json")
	if err := os.WriteFile(path, []byte("not valid json{{{"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := fs.Load("bad-id")
	if err == nil {
		t.Fatal("expected error for corrupt file, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse conversation file") {
		t.Errorf("error = %q, want containing %q", err.Error(), "failed to parse conversation file")
	}
}

func TestList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(t *testing.T, fs *FileStore)
		wantCount int
		checkSort bool
	}{
		{
			name:      "empty directory returns nil",
			setup:     func(_ *testing.T, _ *FileStore) {},
			wantCount: 0,
		},
		{
			name: "lists all conversations",
			setup: func(t *testing.T, fs *FileStore) {
				for _, title := range []string{"first", "second", "third"} {
					conv := sampleConversation(title)
					if err := fs.Save(conv); err != nil {
						t.Fatalf("Save(%q): %v", title, err)
					}
				}
			},
			wantCount: 3,
		},
		{
			name: "sorted by UpdatedAt descending",
			setup: func(t *testing.T, fs *FileStore) {
				// Create conversations with explicit timestamps to control order.
				for i, title := range []string{"oldest", "middle", "newest"} {
					conv := sampleConversation(title)
					conv.ID = title
					conv.UpdatedAt = time.Date(2025, 1, 1+i, 0, 0, 0, 0, time.UTC)
					data, err := json.MarshalIndent(conv, "", "  ")
					if err != nil {
						t.Fatalf("marshal: %v", err)
					}
					path := filepath.Join(fs.dir, title+".json")
					if err := os.WriteFile(path, data, 0o644); err != nil {
						t.Fatalf("write: %v", err)
					}
				}
			},
			wantCount: 3,
			checkSort: true,
		},
		{
			name: "skips corrupt files",
			setup: func(t *testing.T, fs *FileStore) {
				conv := sampleConversation("good")
				if err := fs.Save(conv); err != nil {
					t.Fatalf("Save: %v", err)
				}
				// Write a corrupt file alongside.
				bad := filepath.Join(fs.dir, "corrupt.json")
				if err := os.WriteFile(bad, []byte("{invalid"), 0o644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
			},
			wantCount: 1,
		},
		{
			name: "skips non-JSON files",
			setup: func(t *testing.T, fs *FileStore) {
				conv := sampleConversation("chat")
				if err := fs.Save(conv); err != nil {
					t.Fatalf("Save: %v", err)
				}
				// Write a non-JSON file.
				other := filepath.Join(fs.dir, "notes.txt")
				if err := os.WriteFile(other, []byte("not a chat"), 0o644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
			},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := newTestStore(t)
			tt.setup(t, fs)

			convs, err := fs.List()
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(convs) != tt.wantCount {
				t.Fatalf("len(convs) = %d, want %d", len(convs), tt.wantCount)
			}

			if tt.checkSort && len(convs) >= 2 {
				if convs[0].Title != "newest" {
					t.Errorf("convs[0].Title = %q, want %q", convs[0].Title, "newest")
				}
				if convs[1].Title != "middle" {
					t.Errorf("convs[1].Title = %q, want %q", convs[1].Title, "middle")
				}
				if convs[2].Title != "oldest" {
					t.Errorf("convs[2].Title = %q, want %q", convs[2].Title, "oldest")
				}
			}
		})
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T, fs *FileStore) string // returns ID to delete
		wantErr string
	}{
		{
			name: "deletes existing conversation",
			setup: func(t *testing.T, fs *FileStore) string {
				conv := sampleConversation("doomed")
				if err := fs.Save(conv); err != nil {
					t.Fatalf("Save: %v", err)
				}
				return conv.ID
			},
		},
		{
			name: "missing ID returns ErrNotFound",
			setup: func(_ *testing.T, _ *FileStore) string {
				return "does-not-exist"
			},
			wantErr: "conversation not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := newTestStore(t)
			id := tt.setup(t, fs)

			err := fs.Delete(id)

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

			// Verify the file is gone.
			_, loadErr := fs.Load(id)
			if loadErr == nil {
				t.Error("expected Load after Delete to fail, got nil")
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	fs := newTestStore(t)

	original := sampleConversation("round-trip test")
	if err := fs.Save(original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := fs.Load(original.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.ID != original.ID {
		t.Errorf("ID = %q, want %q", loaded.ID, original.ID)
	}
	if loaded.Title != original.Title {
		t.Errorf("Title = %q, want %q", loaded.Title, original.Title)
	}
	if loaded.Model != original.Model {
		t.Errorf("Model = %q, want %q", loaded.Model, original.Model)
	}
	if len(loaded.Messages) != len(original.Messages) {
		t.Fatalf("len(Messages) = %d, want %d", len(loaded.Messages), len(original.Messages))
	}
	for i, msg := range loaded.Messages {
		if msg.Role != original.Messages[i].Role {
			t.Errorf("Messages[%d].Role = %q, want %q", i, msg.Role, original.Messages[i].Role)
		}
		if msg.Content != original.Messages[i].Content {
			t.Errorf("Messages[%d].Content = %q, want %q", i, msg.Content, original.Messages[i].Content)
		}
	}
}

func TestAutoTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		conv      Conversation
		wantTitle string
	}{
		{
			name:      "existing title unchanged",
			conv:      Conversation{Title: "Existing Title"},
			wantTitle: "Existing Title",
		},
		{
			name: "title from first user message",
			conv: Conversation{
				Messages: []Message{
					{Role: RoleSystem, Content: "system prompt"},
					{Role: RoleUser, Content: "How do I write tests in Go?"},
				},
			},
			wantTitle: "How do I write tests in Go?",
		},
		{
			name:      "no messages returns default",
			conv:      Conversation{},
			wantTitle: "New conversation",
		},
		{
			name: "no user messages returns default",
			conv: Conversation{
				Messages: []Message{
					{Role: RoleSystem, Content: "system prompt"},
					{Role: RoleAssistant, Content: "hello"},
				},
			},
			wantTitle: "New conversation",
		},
		{
			name: "long message is truncated",
			conv: Conversation{
				Messages: []Message{
					{Role: RoleUser, Content: strings.Repeat("a", 200)},
				},
			},
			wantTitle: strings.Repeat("a", 71) + "\u2026",
		},
		{
			name: "newlines replaced with spaces",
			conv: Conversation{
				Messages: []Message{
					{Role: RoleUser, Content: "line one\nline two\nline three"},
				},
			},
			wantTitle: "line one line two line three",
		},
		{
			name: "leading and trailing whitespace trimmed",
			conv: Conversation{
				Messages: []Message{
					{Role: RoleUser, Content: "  hello world  "},
				},
			},
			wantTitle: "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.conv.AutoTitle()
			if got != tt.wantTitle {
				t.Errorf("AutoTitle() = %q, want %q", got, tt.wantTitle)
			}
		})
	}
}

func TestDefaultDir(t *testing.T) {
	t.Parallel()

	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if !strings.HasSuffix(dir, filepath.Join(".quasar", "chats")) {
		t.Errorf("DefaultDir() = %q, want suffix %q", dir, filepath.Join(".quasar", "chats"))
	}
}

func TestGenerateID(t *testing.T) {
	t.Parallel()

	id1 := generateID()
	id2 := generateID()

	if !strings.HasPrefix(id1, "chat-") {
		t.Errorf("id1 = %q, want prefix %q", id1, "chat-")
	}
	if !strings.HasPrefix(id2, "chat-") {
		t.Errorf("id2 = %q, want prefix %q", id2, "chat-")
	}
	if id1 == id2 {
		t.Errorf("two generated IDs should differ: %q == %q", id1, id2)
	}
}
