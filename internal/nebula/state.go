package nebula

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

const stateFileName = "nebula.state.toml"

// legacyState mirrors State but with the old "tasks" TOML key for backward compatibility.
type legacyState struct {
	Version      int                    `toml:"version"`
	NebulaName   string                 `toml:"nebula_name"`
	TotalCostUSD float64                `toml:"total_cost_usd,omitempty"`
	Tasks        map[string]*PhaseState `toml:"tasks"`
}

// LoadState reads the state file from the nebula directory.
// Returns an empty state if the file does not exist.
// For backward compatibility, accepts both [phases] and legacy [tasks] sections,
// preferring [phases]. A deprecation warning is emitted via stderr when [tasks] is encountered.
func LoadState(dir string) (*State, error) {
	path := filepath.Join(dir, stateFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{
				Version: 1,
				Phases:  make(map[string]*PhaseState),
			}, nil
		}
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var state State
	if err := toml.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}

	// Backward compatibility: if Phases is empty, try loading legacy [tasks] section.
	if len(state.Phases) == 0 {
		var legacy legacyState
		if err := toml.Unmarshal(data, &legacy); err == nil && len(legacy.Tasks) > 0 {
			fmt.Fprintf(os.Stderr, "warning: state file uses deprecated [tasks] section; migrate to [phases]\n")
			state.Phases = legacy.Tasks
		}
	}

	if state.Phases == nil {
		state.Phases = make(map[string]*PhaseState)
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

// SetPhaseState updates or creates a phase's state entry.
func (s *State) SetPhaseState(phaseID, beadID string, status PhaseStatus) {
	now := time.Now()
	ps, ok := s.Phases[phaseID]
	if !ok {
		ps = &PhaseState{
			CreatedAt: now,
		}
		s.Phases[phaseID] = ps
	}
	ps.BeadID = beadID
	ps.Status = status
	ps.UpdatedAt = now
}
