package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestStarsDoNotImportSensors verifies that nothing under internal/stars imports
// anything under internal/sensors. Stars are Markdown-defined adapters that
// receive nebulas, not sensor events; letting a star reach into a sensor package
// collapses the layering that keeps external-integration code (sensors) separate
// from execution code (stars). The package may not exist yet — the guard is
// inert until it does, then enforces from the first file.
func TestStarsDoNotImportSensors(t *testing.T) {
	t.Parallel()

	starsDir := filepath.Join(internalDirPath(t), "stars")
	const forbidden = internalPfx + "sensors"

	for _, imp := range fullImportsUnder(t, starsDir) {
		if imp.path == forbidden || strings.HasPrefix(imp.path, forbidden+"/") {
			t.Errorf("%s imports %q; stars must not import sensors (they receive nebulas, not sensor events)",
				imp.relFile, imp.path)
		}
	}
}

// TestTUIIsDBOnly verifies that the TUI reads state only through the fabric
// (SQLite) layer and never reaches into the runtime, the GC engine, or a
// concrete sensor adapter. A TUI that imports the runtime grows side-channel
// knobs that bypass the constellation engine; one that imports a sensor couples
// the dashboard to a specific forge. The DB is the contract.
func TestTUIIsDBOnly(t *testing.T) {
	t.Parallel()

	tuiDir := filepath.Join(internalDirPath(t), "tui")
	forbidden := []string{
		internalPfx + "sensors/github",
		internalPfx + "runtime",
		internalPfx + "gc",
	}

	for _, imp := range fullImportsUnder(t, tuiDir) {
		for _, f := range forbidden {
			if imp.path == f || strings.HasPrefix(imp.path, f+"/") {
				t.Errorf("%s imports %q; the TUI must read state via internal/fabric only", imp.relFile, imp.path)
			}
		}
	}
}

// TestGCUsesInjectedClock verifies that nothing under internal/gc calls
// time.Now() directly. GC decisions (TTL expiry, grace windows) must read time
// through the engine's injected clock so tests can drive deletion
// deterministically; a stray time.Now() makes those tests flaky and the sweep
// non-reproducible. Inert until internal/gc exists.
func TestGCUsesInjectedClock(t *testing.T) {
	t.Parallel()

	gcDir := filepath.Join(internalDirPath(t), "gc")
	for _, hit := range timeNowCalls(t, gcDir) {
		t.Errorf("%s:%d calls time.Now() directly; GC time must come from the injected clock", hit.relPath, hit.line)
	}
}

// timeNowCalls returns every time.Now() call site in non-test .go files under
// absDir. Returns nil if the directory does not exist.
func timeNowCalls(t *testing.T, absDir string) []gitExec {
	t.Helper()

	var hits []gitExec
	fset := token.NewFileSet()
	for _, file := range goFilesUnder(t, absDir) {
		node, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", file, err)
		}
		rel := relForSafety(t, file)
		ast.Inspect(node, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Now" {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "time" {
					hits = append(hits, gitExec{relPath: rel, line: fset.Position(call.Pos()).Line})
				}
			}
			return true
		})
	}
	return hits
}
