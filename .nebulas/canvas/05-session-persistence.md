+++
id = "session-persistence"
title = "Save and resume canvas sessions to .quasar/canvas/"
type = "feature"
priority = 2
depends_on = ["canvas-types", "phase-generation"]
scope = ["internal/canvas/session.go", "internal/canvas/session_test.go"]
+++

## Problem

A canvas conversation can take significant time — the user describes goals, the architect asks clarifying questions, phases are iteratively refined. If the conversation is interrupted (terminal closed, Ctrl+C, machine sleep), all context is lost. The user must restart from scratch, re-explaining their vision.

Canvas sessions need persistence: save the full conversation state (turns, draft phases, metadata) to disk so that `quasar canvas --resume <id>` can pick up exactly where it left off. Sessions should also be listable so users can browse past conversations and resume or delete them.

## Solution

### Storage layout

Sessions are stored as JSON files in `.quasar/canvas/`:

```
.quasar/canvas/
  sessions.json           # Index of all sessions (ID, name, created, updated, phase count)
  <session-id>/
    session.json           # Full session state
```

### SessionStore

```go
// internal/canvas/session.go

package canvas

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "time"
)

// SessionStore manages canvas session persistence in the given base directory.
type SessionStore struct {
    baseDir string // Typically ".quasar/canvas"
}

// NewSessionStore creates a store rooted at the given directory.
// Creates the directory if it does not exist.
func NewSessionStore(baseDir string) (*SessionStore, error) {
    if err := os.MkdirAll(baseDir, 0o755); err != nil {
        return nil, fmt.Errorf("create session dir: %w", err)
    }
    return &SessionStore{baseDir: baseDir}, nil
}
```

### Save

```go
// Save persists a session to disk. Creates the session directory if
// needed. Updates the session index.
func (s *SessionStore) Save(session *Session) error {
    session.UpdatedAt = time.Now()

    dir := filepath.Join(s.baseDir, session.ID)
    if err := os.MkdirAll(dir, 0o755); err != nil {
        return fmt.Errorf("create session dir: %w", err)
    }

    data, err := json.MarshalIndent(session, "", "  ")
    if err != nil {
        return fmt.Errorf("marshal session: %w", err)
    }

    path := filepath.Join(dir, "session.json")
    if err := os.WriteFile(path, data, 0o644); err != nil {
        return fmt.Errorf("write session: %w", err)
    }

    return s.updateIndex(session)
}
```

### Load

```go
// Load reads a session from disk by ID.
func (s *SessionStore) Load(id string) (*Session, error) {
    path := filepath.Join(s.baseDir, id, "session.json")
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("read session %s: %w", id, err)
    }
    var session Session
    if err := json.Unmarshal(data, &session); err != nil {
        return nil, fmt.Errorf("unmarshal session %s: %w", id, err)
    }
    return &session, nil
}
```

### List

```go
// SessionSummary is a lightweight entry for listing sessions without
// loading full conversation history.
type SessionSummary struct {
    ID         string    `json:"id"`
    Name       string    `json:"name"`
    CreatedAt  time.Time `json:"created_at"`
    UpdatedAt  time.Time `json:"updated_at"`
    TurnCount  int       `json:"turn_count"`
    PhaseCount int       `json:"phase_count"`
    Generated  bool      `json:"generated"` // True if nebula files were written.
}

// List returns summaries of all saved sessions, sorted by most recently
// updated first.
func (s *SessionStore) List() ([]SessionSummary, error) {
    path := filepath.Join(s.baseDir, "sessions.json")
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return nil, nil
        }
        return nil, fmt.Errorf("read index: %w", err)
    }
    var summaries []SessionSummary
    if err := json.Unmarshal(data, &summaries); err != nil {
        return nil, fmt.Errorf("unmarshal index: %w", err)
    }
    sort.Slice(summaries, func(i, j int) bool {
        return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
    })
    return summaries, nil
}
```

### Delete

```go
// Delete removes a session and its directory from disk.
func (s *SessionStore) Delete(id string) error {
    dir := filepath.Join(s.baseDir, id)
    if err := os.RemoveAll(dir); err != nil {
        return fmt.Errorf("delete session %s: %w", id, err)
    }
    return s.removeFromIndex(id)
}
```

### Index management

The `sessions.json` index provides fast listing without reading every session file:

```go
func (s *SessionStore) updateIndex(session *Session) error {
    summaries, _ := s.List() // Ignore error (may not exist yet).
    updated := false
    for i, sum := range summaries {
        if sum.ID == session.ID {
            summaries[i] = session.Summary()
            updated = true
            break
        }
    }
    if !updated {
        summaries = append(summaries, session.Summary())
    }
    return s.writeIndex(summaries)
}

func (s *SessionStore) removeFromIndex(id string) error {
    summaries, _ := s.List()
    filtered := summaries[:0]
    for _, sum := range summaries {
        if sum.ID != id {
            filtered = append(filtered, sum)
        }
    }
    return s.writeIndex(filtered)
}

func (s *SessionStore) writeIndex(summaries []SessionSummary) error {
    data, err := json.MarshalIndent(summaries, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(filepath.Join(s.baseDir, "sessions.json"), data, 0o644)
}
```

### Auto-save integration

The canvas REPL (phase 3) calls `store.Save(session)` after every turn so that progress is never lost. The save is fast (~1ms for typical sessions) and does not interrupt the conversation flow.

## Files

- `internal/canvas/session.go` — `SessionStore`, `SessionSummary`, `NewSessionStore`, `Save`, `Load`, `List`, `Delete`, index management
- `internal/canvas/session_test.go` — tests for: save and load round-trip, list ordering (most recent first), delete removes directory and index entry, concurrent save safety, nonexistent session returns error, empty store returns nil list, index survives partial corruption (missing session dir)

## Acceptance Criteria

- [ ] `Save` writes `session.json` to `.quasar/canvas/<id>/` with full conversation state
- [ ] `Load` reads a session by ID and returns the full `Session` struct
- [ ] `List` returns `SessionSummary` entries sorted by most recently updated
- [ ] `Delete` removes the session directory and index entry
- [ ] Session index (`sessions.json`) is updated on every `Save` and `Delete`
- [ ] `Save` updates `UpdatedAt` timestamp automatically
- [ ] `Load` returns a descriptive error for nonexistent session IDs
- [ ] Empty store returns nil from `List` (not an error)
- [ ] `NewSessionStore` creates the base directory if it does not exist
- [ ] JSON serialization is human-readable (indented)
- [ ] `go test ./internal/canvas/...` passes
- [ ] `go vet ./internal/canvas/...` passes
