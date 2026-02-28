package arch_test

import (
	"path/filepath"
	"testing"
)

// layers assigns each internal package to a numeric layer. Lower layers are
// more foundational; higher layers may depend on lower ones but not vice versa.
// A package at layer N may only import packages at layer N or below.
var layers = map[string]int{
	"agent":     0,
	"ansi":      0,
	"beads":     0,
	"config":    0,
	"dag":       0,
	"filter":    0,
	"snapshot":  0,
	"telemetry": 0,

	"claude": 1,
	"fabric": 1,

	"neutron": 2,
	"tycho":   2,

	"loop": 3,

	"nebula": 4,

	"ui": 5,

	"tui": 6,
}

// allowedExceptions documents known layering violations that have been
// accepted as technical debt. Each entry maps importer → imported → reason.
// The test logs these as warnings but does not fail.
var allowedExceptions = map[string]map[string]string{
	"ui": {
		"nebula": "TODO: ui imports nebula for plan rendering types; extract to break this dependency",
	},
	"loop": {
		"ui": "TODO: loop imports ui for Printer; inject a minimal logging interface instead",
	},
}

// TestDependencyLayering verifies that no internal package imports a package
// from a higher layer, enforcing the project's dependency DAG.
func TestDependencyLayering(t *testing.T) {
	t.Parallel()

	dir := internalDirPath(t)

	for _, pkg := range internalPackages(t) {
		importerLayer, ok := layers[pkg]
		if !ok {
			// Unknown packages are caught by TestNoUnknownPackages.
			continue
		}

		imports := importsOf(t, filepath.Join(dir, pkg))
		for _, imp := range imports {
			importedLayer, ok := layers[imp]
			if !ok {
				// Imported package not in layer map; skip — caught by
				// TestNoUnknownPackages if it's an internal package.
				continue
			}

			if importerLayer >= importedLayer {
				// Legal: same layer or importing from below.
				continue
			}

			// Upward import detected.
			if exceptions, hasExceptions := allowedExceptions[pkg]; hasExceptions {
				if reason, allowed := exceptions[imp]; allowed {
					t.Logf("known exception: %s (layer %d) imports %s (layer %d): %s",
						pkg, importerLayer, imp, importedLayer, reason)
					continue
				}
			}

			t.Errorf("layer violation: %s (layer %d) imports %s (layer %d)",
				pkg, importerLayer, imp, importedLayer)
		}
	}
}

// TestNoUnknownPackages verifies that every internal package (excluding board
// and arch_test) has an assigned layer. This forces developers to place new
// packages in the dependency DAG.
func TestNoUnknownPackages(t *testing.T) {
	t.Parallel()

	for _, pkg := range internalPackages(t) {
		if _, ok := layers[pkg]; !ok {
			t.Errorf("package %s has no layer assignment; add it to the layers map", pkg)
		}
	}
}
