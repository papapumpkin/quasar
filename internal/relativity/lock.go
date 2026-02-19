// lock.go defines the LockFile types and persistence for the computed graph
// state derived from the spacetime catalog. The lock file is analogous to
// go.sum — it captures resolved dependency order, wave assignments, impact
// metrics, and staleness detection data.
package relativity

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

// DefaultLockPath is the conventional location for the computed lock file.
const DefaultLockPath = ".relativity/spacetime.lock"

// LockFile is the computed graph state derived from the catalog.
type LockFile struct {
	Version     int       `toml:"version"`
	GeneratedAt time.Time `toml:"generated_at"`
	SourceHash  string    `toml:"source_hash"`
	Graph       Graph     `toml:"graph"`
	Metrics     []Metric  `toml:"metrics"`
	Staleness   Staleness `toml:"staleness"`
}

// Graph captures the resolved dependency order and wave assignments.
type Graph struct {
	Order []string   `toml:"order"`
	Waves [][]string `toml:"waves"`
}

// Metric holds computed scores for a single nebula.
type Metric struct {
	Name            string   `toml:"name"`
	Wave            int      `toml:"wave"`
	ImpactScore     float64  `toml:"impact_score"`
	Centrality      float64  `toml:"centrality"`
	DownstreamCount int      `toml:"downstream_count"`
	AreaOverlap     []string `toml:"area_overlap"`
}

// Staleness holds data used to detect when the lock is out of date.
type Staleness struct {
	NebulaCount   int               `toml:"nebula_count"`
	LastGitCommit string            `toml:"last_git_commit"`
	BranchTips    map[string]string `toml:"branch_tips"`
}

// LoadLock reads a lock file from the given path. If the file does not exist,
// it returns nil and no error, allowing callers to detect that no lock has
// been generated yet.
func LoadLock(path string) (*LockFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading spacetime.lock: %w", err)
	}

	var lf LockFile
	if err := toml.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parsing spacetime.lock: %w", err)
	}
	return &lf, nil
}

// SaveLock writes the lock file to the given path, creating parent directories
// as needed.
func SaveLock(path string, lf *LockFile) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	data, err := toml.Marshal(lf)
	if err != nil {
		return fmt.Errorf("marshaling spacetime.lock: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing spacetime.lock: %w", err)
	}
	return nil
}
