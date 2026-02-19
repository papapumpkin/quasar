// lock_staleness.go provides staleness detection for the lock file by
// comparing stored snapshots against current state.
package relativity

import (
	"crypto/sha256"
	"fmt"
	"os"
)

// IsStale reports whether the lock file is out of date relative to the
// current state. A lock is considered stale if any of:
//   - source_hash doesn't match the current spacetime.toml hash
//   - nebula_count differs from the current catalog
//   - last_git_commit doesn't match HEAD
//   - any tracked branch tips have advanced
func IsStale(lf *LockFile, currentHash string, currentNebulaCount int, currentHead string, currentBranchTips map[string]string) bool {
	if lf == nil {
		return true
	}

	if lf.SourceHash != currentHash {
		return true
	}

	if lf.Staleness.NebulaCount != currentNebulaCount {
		return true
	}

	if lf.Staleness.LastGitCommit != currentHead {
		return true
	}

	if branchTipsChanged(lf.Staleness.BranchTips, currentBranchTips) {
		return true
	}

	return false
}

// branchTipsChanged reports whether any branch tips have changed between the
// stored and current values.
func branchTipsChanged(stored, current map[string]string) bool {
	// Different number of tracked branches.
	if len(stored) != len(current) {
		return true
	}

	for branch, storedTip := range stored {
		currentTip, ok := current[branch]
		if !ok {
			return true
		}
		if storedTip != currentTip {
			return true
		}
	}

	return false
}

// HashFile computes the SHA-256 hash of the file at the given path, returning
// the hash as a "sha256:<hex>" string. This is used to detect when the
// spacetime.toml has changed since the lock was generated.
func HashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading file for hash: %w", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum), nil
}
