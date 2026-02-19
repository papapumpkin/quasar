package tui

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/papapumpkin/quasar/internal/nebula"
)

// NebulaChoice describes an available nebula for the post-completion picker or home screen.
type NebulaChoice struct {
	Name        string // from nebula.toml [nebula] name
	Description string // from nebula.toml [nebula] description
	Path        string // directory path
	Status      string // "ready", "in_progress", "done", "partial"
	Phases      int    // total phase count
	Done        int    // completed phases
}

// DiscoverNebulae scans the parent of currentDir for sibling nebula directories.
// It returns a list of valid nebulae, excluding the one at currentDir.
func DiscoverNebulae(currentDir string) ([]NebulaChoice, error) {
	parentDir := filepath.Dir(currentDir)
	absCurrentDir, err := filepath.Abs(currentDir)
	if err != nil {
		return nil, fmt.Errorf("resolving current dir: %w", err)
	}

	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return nil, fmt.Errorf("reading parent directory: %w", err)
	}

	var choices []NebulaChoice
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(parentDir, entry.Name())
		absDirPath, err := filepath.Abs(dirPath)
		if err != nil {
			continue
		}

		// Skip the currently-running nebula.
		if absDirPath == absCurrentDir {
			continue
		}

		// Try loading as a nebula — skip if not valid.
		n, err := nebula.Load(dirPath)
		if err != nil {
			continue
		}

		choice := NebulaChoice{
			Name:        n.Manifest.Nebula.Name,
			Description: n.Manifest.Nebula.Description,
			Path:        dirPath,
			Phases:      len(n.Phases),
		}

		// If name is empty, fall back to directory name.
		if choice.Name == "" {
			choice.Name = entry.Name()
		}

		// Determine status from state file.
		state, err := nebula.LoadState(dirPath)
		if err != nil {
			choice.Status = "ready"
		} else {
			choice.Status, choice.Done = classifyNebulaStatus(n, state)
		}

		choices = append(choices, choice)
	}

	return choices, nil
}

// DiscoverAllNebulae scans the given directory for valid nebula subdirectories.
// Unlike DiscoverNebulae, it does not exclude any directory and is intended for
// the home screen where no nebula is currently running.
func DiscoverAllNebulae(nebulaeDir string) ([]NebulaChoice, error) {
	entries, err := os.ReadDir(nebulaeDir)
	if err != nil {
		return nil, fmt.Errorf("reading nebulae directory: %w", err)
	}

	var choices []NebulaChoice
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(nebulaeDir, entry.Name())

		n, err := nebula.Load(dirPath)
		if err != nil {
			continue
		}

		choice := NebulaChoice{
			Name:        n.Manifest.Nebula.Name,
			Description: n.Manifest.Nebula.Description,
			Path:        dirPath,
			Phases:      len(n.Phases),
		}

		if choice.Name == "" {
			choice.Name = entry.Name()
		}

		state, err := nebula.LoadState(dirPath)
		if err != nil {
			choice.Status = "ready"
		} else {
			choice.Status, choice.Done = classifyNebulaStatus(n, state)
		}

		choices = append(choices, choice)
	}

	return choices, nil
}

// classifyNebulaStatus determines the status of a nebula based on its state.
func classifyNebulaStatus(n *nebula.Nebula, state *nebula.State) (status string, doneCount int) {
	if len(state.Phases) == 0 {
		return "ready", 0
	}

	totalPhases := len(n.Phases)
	var resolved int

	for _, ps := range state.Phases {
		switch ps.Status {
		case nebula.PhaseStatusDone:
			doneCount++
			resolved++
		case nebula.PhaseStatusFailed, nebula.PhaseStatusSkipped:
			resolved++
		}
	}

	switch {
	case resolved >= totalPhases:
		return "done", doneCount
	case doneCount > 0 || resolved > 0:
		return "in_progress", doneCount
	default:
		return "ready", 0
	}
}
