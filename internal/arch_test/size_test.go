package arch_test

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	maxFilesPerPackage = 20
	maxLinesPerFile    = 400
)

// packageFileCountExceptions lists packages that currently exceed maxFilesPerPackage.
// Each entry maps a package name to its current non-test .go file count.
// TODO: Split these packages into smaller, focused sub-packages.
var packageFileCountExceptions = map[string]int{
	"tui":    34, // TODO: split into tui/views, tui/bridge, tui/overlay sub-packages
	"nebula": 30, // TODO: split into nebula/worker, nebula/plan, nebula/metrics sub-packages
}

// lineCountExceptions lists files that currently exceed maxLinesPerFile.
// Each entry maps a file path (relative to repo root) to its current line count.
// TODO: Decompose each file into smaller, focused units.
var lineCountExceptions = map[string]int{
	"internal/dag/dag.go":            462,  // TODO: split DAG operations
	"internal/fabric/sqlite.go":      565,  // TODO: split query methods into separate files
	"internal/fabric/static.go":      486,  // TODO: decompose static fabric impl
	"internal/loop/loop.go":          632,  // TODO: extract cycle logic into separate file
	"internal/nebula/plan_engine.go": 408,  // TODO: extract plan engine steps
	"internal/nebula/worker.go":      471,  // TODO: extract worker lifecycle methods
	"internal/tui/diffview.go":       495,  // TODO: extract diff rendering
	"internal/tui/graphview.go":      453,  // TODO: extract graph rendering helpers
	"internal/tui/model.go":          2249, // TODO: split into model_init.go and model_update.go
	"internal/tui/overlay.go":        417,  // TODO: decompose overlay components
	"internal/tui/planview.go":       510,  // TODO: extract plan view helpers
	"internal/tui/statusbar.go":      590,  // TODO: decompose status bar components
	"internal/tui/bridge.go":         428,  // TODO: decompose bridge
	"internal/tui/msg.go":            402,  // TODO: decompose message types
	"internal/ui/dagrender.go":       614,  // TODO: split rendering helpers
}

// allGoFilesIn returns all .go files (including test files) in the given directory,
// sorted by path. Unlike goFilesIn, this includes _test.go files.
func allGoFilesIn(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading directory %s: %v", dir, err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".go") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files
}

// isGenerated reports whether the file begins with a "// Code generated" comment,
// indicating it was produced by a code generator and should be excluded from size checks.
func isGenerated(t *testing.T, filePath string) bool {
	t.Helper()

	f, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("opening %s: %v", filePath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		return strings.HasPrefix(scanner.Text(), "// Code generated")
	}
	return false
}

// TestPackageFileCount verifies that no internal package has more than
// maxFilesPerPackage non-test .go files.
func TestPackageFileCount(t *testing.T) {
	t.Parallel()

	dir := internalDirPath(t)

	for _, pkg := range internalPackages(t) {
		t.Run(pkg, func(t *testing.T) {
			t.Parallel()

			files := goFilesIn(t, filepath.Join(dir, pkg))
			count := len(files)

			if count <= maxFilesPerPackage {
				return
			}

			// Check if this package is a known exception.
			if _, ok := packageFileCountExceptions[pkg]; ok {
				t.Logf("known exception: package %s has %d .go files (limit: %d)", pkg, count, maxFilesPerPackage)
				return
			}

			t.Errorf("package %s has %d .go files (limit: %d); consider splitting", pkg, count, maxFilesPerPackage)
		})
	}
}

// TestFileLineCount verifies that no non-test .go file in internal packages
// exceeds maxLinesPerFile lines.
func TestFileLineCount(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	dir := internalDirPath(t)

	for _, pkg := range internalPackages(t) {
		pkgDir := filepath.Join(dir, pkg)

		for _, filePath := range allGoFilesIn(t, pkgDir) {
			rel, err := filepath.Rel(root, filePath)
			if err != nil {
				t.Fatalf("computing relative path for %s: %v", filePath, err)
			}

			// Skip test files — only enforce line limits on production code.
			if strings.HasSuffix(rel, "_test.go") {
				continue
			}

			t.Run(rel, func(t *testing.T) {
				t.Parallel()

				// Skip generated files.
				if isGenerated(t, filePath) {
					t.Skipf("skipping generated file %s", rel)
					return
				}

				count := lineCount(t, filePath)
				if count <= maxLinesPerFile {
					return
				}

				// Check if this file is a known exception.
				if _, ok := lineCountExceptions[rel]; ok {
					t.Logf("known exception: %s has %d lines (limit: %d)", rel, count, maxLinesPerFile)
					return
				}

				t.Errorf("%s has %d lines (limit: %d); consider decomposing", rel, count, maxLinesPerFile)
			})
		}
	}
}
