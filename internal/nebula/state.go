package nebula

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

const stateFileName = "nebula.state.toml"

// LoadState reads the state file from the nebula directory.
// Returns an empty state if the file does not exist.
func LoadState(dir string) (*State, error) {
	path := filepath.Join(dir, stateFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{
				Version: 1,
				Tasks:   make(map[string]*TaskState),
			}, nil
		}
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var state State
	if err := toml.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}

	if state.Tasks == nil {
		state.Tasks = make(map[string]*TaskState)
	}

	return &state, nil
}

// SaveState writes the state file atomically (write temp + rename).
func SaveState(dir string, state *State) error {
	data, err := toml.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	path := filepath.Join(dir, stateFileName)
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("writing temp state file: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming state file: %w", err)
	}

	return nil
}

// SetTaskState updates or creates a task's state entry.
func (s *State) SetTaskState(taskID, beadID string, status TaskStatus) {
	now := time.Now()
	ts, ok := s.Tasks[taskID]
	if !ok {
		ts = &TaskState{
			CreatedAt: now,
		}
		s.Tasks[taskID] = ts
	}
	ts.BeadID = beadID
	ts.Status = status
	ts.UpdatedAt = now
}
