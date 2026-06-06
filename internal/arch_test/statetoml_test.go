package arch_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoStateTomlWrites verifies that no production code references a file named
// state.toml. SQLite is the canonical nebula-state store; an earlier phase
// removed the on-disk state.toml dual-write, and reintroducing it would split
// the source of truth and desync the two. The on-disk authoring surface
// (nebula.state.toml) is intentionally a different filename and is unaffected.
func TestNoStateTomlWrites(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	for _, sub := range []string{"cmd", "internal"} {
		base := filepath.Join(root, sub)
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			for _, lit := range stringLiterals(t, path) {
				if isStateTomlPath(lit.value) {
					t.Errorf("%s:%d references %q; nebula state lives in SQLite — do not reintroduce the state.toml dual-write",
						relForSafety(t, path), lit.line, lit.value)
				}
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("walking %s: %v", base, err)
		}
	}
}

// isStateTomlPath reports whether a string literal names a file called
// state.toml. It matches the bare name and any path ending in /state.toml, but
// deliberately not nebula.state.toml (the file-based authoring surface).
func isStateTomlPath(v string) bool {
	return v == "state.toml" || strings.HasSuffix(v, "/state.toml")
}
