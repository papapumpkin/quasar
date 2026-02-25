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
	"tui":    35, // TODO: split into tui/views, tui/bridge, tui/overlay sub-packages
	"nebula": 30, // TODO: split into nebula/worker, nebula/plan, nebula/metrics sub-packages
}

// lineCountExceptions lists files that currently exceed maxLinesPerFile.
// Each entry maps a file path (relative to repo root) to its current line count.
// TODO: Decompose each file into smaller, focused units.
var lineCountExceptions = map[string]int{
	// Production files
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
	"internal/ui/dagrender.go":       614,  // TODO: split rendering helpers

	// Test files
	"internal/beads/beads_test.go":           955,  // TODO: split beads test cases
	"internal/claude/claude_test.go":         431,  // TODO: split claude tests
	"internal/dag/analyzer_test.go":          407,  // TODO: split analyzer tests
	"internal/dag/dag_test.go":               1339, // TODO: split by operation
	"internal/dag/scoring_test.go":           526,  // TODO: split scoring tests
	"internal/dag/tracks_test.go":            463,  // TODO: split tracks tests
	"internal/fabric/integration_test.go":    705,  // TODO: split integration tests
	"internal/fabric/publisher_test.go":      542,  // TODO: split publisher tests
	"internal/fabric/pushback_test.go":       476,  // TODO: split pushback tests
	"internal/fabric/snapshot_test.go":       614,  // TODO: split snapshot tests
	"internal/fabric/sqlite_test.go":         757,  // TODO: split SQLite tests
	"internal/fabric/static_test.go":         658,  // TODO: split static fabric tests
	"internal/filter/chain_test.go":          476,  // TODO: split filter chain tests
	"internal/loop/lint_test.go":             455,  // TODO: split lint tests
	"internal/loop/loop_test.go":             2072, // TODO: split into focused test files
	"internal/nebula/architect_test.go":      719,  // TODO: split architect tests
	"internal/nebula/checkpoint_test.go":     469,  // TODO: split checkpoint tests
	"internal/nebula/dashboard_test.go":      405,  // TODO: split dashboard tests
	"internal/nebula/git_test.go":            767,  // TODO: split git operation tests
	"internal/nebula/nebula_test.go":         2002, // TODO: split by test category
	"internal/nebula/plan_engine_test.go":    586,  // TODO: split plan engine tests
	"internal/nebula/scheduler_test.go":      452,  // TODO: split scheduler tests
	"internal/nebula/worker_changes_test.go": 646,  // TODO: split worker changes tests
	"internal/nebula/worker_fabric_test.go":  881,  // TODO: split fabric worker tests
	"internal/snapshot/scanner_test.go":      583,  // TODO: split scanner tests
	"internal/tui/boardview_test.go":         467,  // TODO: split board view tests
	"internal/tui/bridge_test.go":            1226, // TODO: split bridge test cases
	"internal/tui/detailpanel_test.go":       657,  // TODO: split detail panel tests
	"internal/tui/entanglementview_test.go":  424,  // TODO: split entanglement view tests
	"internal/tui/hailoverlay_test.go":       556,  // TODO: split hail overlay tests
	"internal/tui/homeview_test.go":          561,  // TODO: split home view tests
	"internal/tui/layout_test.go":            457,  // TODO: split layout tests
	"internal/tui/model_cockpit_test.go":     587,  // TODO: split cockpit tests
	"internal/tui/model_controls_test.go":    1123, // TODO: split model control tests
	"internal/tui/nebula_discover_test.go":   463,  // TODO: split nebula discover tests
	"internal/tui/overlay_test.go":           1117, // TODO: split overlay test cases
	"internal/tui/statusbar_test.go":         664,  // TODO: split status bar tests
	"internal/tui/styles_test.go":            710,  // TODO: split style tests
	"internal/tycho/tycho_test.go":           985,  // TODO: decompose tycho tests
	"internal/tycho/wave_scan_test.go":       609,  // TODO: split wave scan tests
	"internal/ui/dagrender_test.go":          536,  // TODO: split dagrender tests
	"internal/ui/ui_test.go":                 1399, // TODO: split by component
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

// TestFileLineCount verifies that no .go file (including test files) in internal
// packages exceeds maxLinesPerFile lines.
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
