package chat

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Sentinel errors for store operations.
var (
	// ErrNotFound is returned when a conversation ID does not exist on disk.
	ErrNotFound = errors.New("conversation not found")
)

// Store defines the persistence interface for chat conversations.
// Implementations handle CRUD operations for the conversation store.
type Store interface {
	// List returns all conversations ordered by most-recently updated first.
	// Only metadata is guaranteed to be populated; implementations may omit
	// full message bodies for performance.
	List() ([]Conversation, error)

	// Load retrieves a single conversation by ID, including all messages.
	Load(id string) (*Conversation, error)

	// Save persists a conversation to the store. If the conversation has no
	// ID, one is generated. The UpdatedAt timestamp is set automatically.
	Save(conv *Conversation) error

	// Delete removes a conversation by ID. Returns ErrNotFound if the
	// conversation does not exist.
	Delete(id string) error
}

// FileStore implements Store using one JSON file per conversation in a
// directory on disk. The default location is ~/.quasar/chats/.
type FileStore struct {
	dir string
}

// NewFileStore creates a FileStore rooted at the given directory. The
// directory is created (with parents) if it does not already exist.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create chat directory: %w", err)
	}
	return &FileStore{dir: dir}, nil
}

// DefaultDir returns the default chat storage directory (~/.quasar/chats).
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".quasar", "chats"), nil
}

// List scans the store directory for conversation files and returns them
// sorted by UpdatedAt descending (most recent first).
func (fs *FileStore) List() ([]Conversation, error) {
	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read chat directory: %w", err)
	}

	var convs []Conversation
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		conv, err := fs.loadFile(filepath.Join(fs.dir, entry.Name()))
		if err != nil {
			// Skip corrupt files rather than failing the whole listing.
			continue
		}
		convs = append(convs, *conv)
	}

	sort.Slice(convs, func(i, j int) bool {
		return convs[i].UpdatedAt.After(convs[j].UpdatedAt)
	})
	return convs, nil
}

// Load reads a single conversation by ID.
func (fs *FileStore) Load(id string) (*Conversation, error) {
	path := fs.path(id)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return fs.loadFile(path)
}

// Save writes a conversation to disk as a JSON file. If the conversation
// has no ID, one is generated. If no title is set, an auto-title is
// derived from the first user message.
func (fs *FileStore) Save(conv *Conversation) error {
	now := time.Now()
	if conv.ID == "" {
		conv.ID = generateID()
	}
	if conv.CreatedAt.IsZero() {
		conv.CreatedAt = now
	}
	conv.UpdatedAt = now
	conv.AutoTitle()

	data, err := json.MarshalIndent(conv, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal conversation: %w", err)
	}

	path := fs.path(conv.ID)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write conversation file: %w", err)
	}
	return nil
}

// Delete removes a conversation file by ID.
func (fs *FileStore) Delete(id string) error {
	path := fs.path(id)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to delete conversation file: %w", err)
	}
	return nil
}

// path returns the filesystem path for a conversation ID.
func (fs *FileStore) path(id string) string {
	return filepath.Join(fs.dir, id+".json")
}

// loadFile reads and unmarshals a single conversation JSON file.
func (fs *FileStore) loadFile(path string) (*Conversation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read conversation file: %w", err)
	}
	var conv Conversation
	if err := json.Unmarshal(data, &conv); err != nil {
		return nil, fmt.Errorf("failed to parse conversation file: %w", err)
	}
	return &conv, nil
}

// generateID produces a short random hex identifier for a new conversation.
func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) //nolint:errcheck // crypto/rand.Read never returns error on supported platforms
	return fmt.Sprintf("chat-%x", b)
}
