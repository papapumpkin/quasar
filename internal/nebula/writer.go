package nebula

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	toml "github.com/pelletier/go-toml/v2"
)

// ErrDirExists indicates the output directory already exists and Overwrite was not set.
var ErrDirExists = errors.New("output directory already exists")

// WriteOptions controls how a nebula is written to disk.
type WriteOptions struct {
	Overwrite bool // If true, overwrite an existing nebula directory.
}

// WriteNebula writes a complete nebula (manifest + phase files) to the given
// output directory. Phase files are numbered sequentially in topological order
// of their dependencies (01-phase-id.md, 02-phase-id.md, etc.).
//
// If the directory already exists and opts.Overwrite is false, WriteNebula
// returns an error. On failure, any partially written directory is removed.
func WriteNebula(result *GenerateResult, outputDir string, opts WriteOptions) error {
	// Pre-flight: check if directory already exists.
	if info, err := os.Stat(outputDir); err == nil && info.IsDir() {
		if !opts.Overwrite {
			return fmt.Errorf("%w: %s; use --force to overwrite", ErrDirExists, outputDir)
		}
	}

	// Sort phases topologically for deterministic numbering.
	sorted, err := topoSortPhases(result.Phases)
	if err != nil {
		return fmt.Errorf("sorting phases: %w", err)
	}

	// Write to a temp directory first; rename atomically on success.
	tmpDir := outputDir + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("cleaning temp directory: %w", err)
	}

	// Ensure cleanup on failure.
	success := false
	defer func() {
		if !success {
			os.RemoveAll(tmpDir)
		}
	}()

	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}

	// Write manifest.
	manifestBytes, err := marshalManifest(result.Manifest)
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "nebula.toml"), manifestBytes, 0o644); err != nil {
		return fmt.Errorf("writing nebula.toml: %w", err)
	}

	// Write phase files.
	for i, phase := range sorted {
		filename := fmt.Sprintf("%02d-%s.md", i+1, phase.ID)
		data, err := MarshalPhaseFile(phase)
		if err != nil {
			return fmt.Errorf("marshaling phase %q: %w", phase.ID, err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, filename), data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", filename, err)
		}
	}

	// Atomic swap: remove existing dir if overwrite, then rename.
	if opts.Overwrite {
		if err := os.RemoveAll(outputDir); err != nil {
			return fmt.Errorf("removing existing directory: %w", err)
		}
	}
	if err := os.Rename(tmpDir, outputDir); err != nil {
		return fmt.Errorf("renaming temp to output directory: %w", err)
	}

	success = true
	return nil
}

// marshalManifest serializes a Manifest to TOML bytes suitable for writing
// as nebula.toml.
func marshalManifest(m Manifest) ([]byte, error) {
	data, err := toml.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshaling manifest to TOML: %w", err)
	}
	// Ensure trailing newline for POSIX compliance.
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	return data, nil
}

// topoSortPhases returns phases sorted in topological order by their
// DependsOn relationships. Within a topological level, phases are sorted
// by priority (ascending) then by ID (alphabetical) for determinism.
// Returns an error if the dependency graph contains a cycle.
func topoSortPhases(phases []PhaseSpec) ([]PhaseSpec, error) {
	if len(phases) == 0 {
		return nil, nil
	}

	// Build index and in-degree map.
	byID := make(map[string]*PhaseSpec, len(phases))
	inDegree := make(map[string]int, len(phases))
	children := make(map[string][]string, len(phases))

	for i := range phases {
		p := &phases[i]
		byID[p.ID] = p
		inDegree[p.ID] = 0
	}

	for i := range phases {
		p := &phases[i]
		for _, dep := range p.DependsOn {
			if _, ok := byID[dep]; !ok {
				// Skip unknown deps — validation catches this separately.
				continue
			}
			inDegree[p.ID]++
			children[dep] = append(children[dep], p.ID)
		}
	}

	// Kahn's algorithm: BFS with in-degree tracking.
	var queue []string
	for id := range byID {
		if inDegree[id] == 0 {
			queue = append(queue, id)
		}
	}
	// Sort initial queue for determinism within the first level.
	sortByPriorityThenID(queue, byID)

	var sorted []PhaseSpec
	for len(queue) > 0 {
		// Sort the current frontier for deterministic ordering within each level.
		sortByPriorityThenID(queue, byID)

		// Process the entire current level.
		levelSize := len(queue)
		for i := 0; i < levelSize; i++ {
			id := queue[i]
			sorted = append(sorted, *byID[id])
			for _, child := range children[id] {
				inDegree[child]--
				if inDegree[child] == 0 {
					queue = append(queue, child)
				}
			}
		}
		queue = queue[levelSize:]
	}

	if len(sorted) != len(phases) {
		return nil, fmt.Errorf("%w: could not topologically sort all phases", ErrDependencyCycle)
	}

	return sorted, nil
}

// sortByPriorityThenID sorts a slice of phase IDs by priority (ascending),
// then alphabetically by ID for determinism.
func sortByPriorityThenID(ids []string, byID map[string]*PhaseSpec) {
	sort.Slice(ids, func(i, j int) bool {
		pi, pj := byID[ids[i]], byID[ids[j]]
		if pi.Priority != pj.Priority {
			return pi.Priority < pj.Priority
		}
		return pi.ID < pj.ID
	})
}
