package fleet

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// UIState is the persisted, cross-launch fleet view state stored as
// tui-state.json next to the fabric database. It is written on quit only — no
// live writes — to keep disk noise minimal.
type UIState struct {
	FoldedRepos []string `json:"folded_repos"`
	ActiveLane  string   `json:"active_lane"`
	Filter      string   `json:"filter"`
}

// LoadState reads UI state from path. A missing file is not an error: it yields
// the zero UIState so first launch starts with everything unfolded.
func LoadState(path string) (UIState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return UIState{}, nil
	}
	if err != nil {
		return UIState{}, fmt.Errorf("fleet: read ui state %q: %w", path, err)
	}
	var st UIState
	if err := json.Unmarshal(data, &st); err != nil {
		return UIState{}, fmt.Errorf("fleet: parse ui state %q: %w", path, err)
	}
	return st, nil
}

// SaveState writes UI state to path as indented JSON, creating parent
// directories as needed.
func SaveState(path string, st UIState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("fleet: create ui state dir: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("fleet: marshal ui state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("fleet: write ui state %q: %w", path, err)
	}
	return nil
}

// IsFolded reports whether the named repo is recorded as folded.
func (st UIState) IsFolded(displayName string) bool {
	for _, r := range st.FoldedRepos {
		if r == displayName {
			return true
		}
	}
	return false
}

// ToggleFold flips the folded state of the named repo and returns the result.
func (st UIState) ToggleFold(displayName string) UIState {
	for i, r := range st.FoldedRepos {
		if r == displayName {
			st.FoldedRepos = append(st.FoldedRepos[:i], st.FoldedRepos[i+1:]...)
			return st
		}
	}
	st.FoldedRepos = append(st.FoldedRepos, displayName)
	return st
}
