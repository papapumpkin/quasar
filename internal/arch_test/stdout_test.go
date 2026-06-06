package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// stdoutMarker, when present on a statement's line or the line directly above,
// declares that an os.Stdout / fmt.Print* write is intentional machine-readable
// output (e.g. `version`, `--json`). Human-readable output must go to stderr via
// ui.Printer; this keeps stdout a clean structured-data channel.
const stdoutMarker = "arch-test: stdout-allowed"

// stdoutWrite is a detected write to stdout with its source line.
type stdoutWrite struct {
	relPath string
	line    int
	expr    string
}

// TestCmdStdoutRequiresMarker verifies that every os.Stdout reference and
// fmt.Print/Printf/Println call in a cmd/ package is explicitly tagged with the
// stdout-allowed marker. Untagged stdout writes are almost always human messages
// that belong on stderr; the marker forces a deliberate choice at the call site.
func TestCmdStdoutRequiresMarker(t *testing.T) {
	t.Parallel()

	cmdDir := filepath.Join(repoRoot(t), "cmd")
	for _, file := range goFilesUnder(t, cmdDir) {
		markers := markerLines(t, file)
		for _, w := range stdoutWritesInFile(t, file) {
			if markers[w.line] || markers[w.line-1] {
				continue
			}
			t.Errorf("%s:%d writes to stdout via %s without a %q marker; "+
				"human output must use ui.Printer (stderr)", w.relPath, w.line, w.expr, stdoutMarker)
		}
	}
}

// markerLines returns the set of line numbers in file whose comments contain the
// stdout-allowed marker.
func markerLines(t *testing.T, file string) map[int]bool {
	t.Helper()

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing %s: %v", file, err)
	}

	lines := make(map[int]bool)
	for _, cg := range node.Comments {
		for _, c := range cg.List {
			if strings.Contains(c.Text, stdoutMarker) {
				lines[fset.Position(c.Pos()).Line] = true
			}
		}
	}
	return lines
}

// stdoutWritesInFile returns every os.Stdout selector and fmt.Print* call in the
// file. fmt.Fprint* targeting os.Stdout is caught via the os.Stdout selector.
func stdoutWritesInFile(t *testing.T, file string) []stdoutWrite {
	t.Helper()

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", file, err)
	}
	rel := relForSafety(t, file)

	var writes []stdoutWrite
	ast.Inspect(node, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.SelectorExpr:
			if pkg, ok := e.X.(*ast.Ident); ok && pkg.Name == "os" && e.Sel.Name == "Stdout" {
				writes = append(writes, stdoutWrite{rel, fset.Position(e.Pos()).Line, "os.Stdout"})
			}
		case *ast.CallExpr:
			if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "fmt" {
					switch sel.Sel.Name {
					case "Print", "Printf", "Println":
						writes = append(writes, stdoutWrite{rel, fset.Position(e.Pos()).Line, "fmt." + sel.Sel.Name})
					}
				}
			}
		}
		return true
	})
	return writes
}
