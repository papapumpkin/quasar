package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

const (
	modulePath  = "github.com/papapumpkin/quasar"
	internalPfx = modulePath + "/internal/"
)

var excludedPkgs = map[string]bool{
	"board":     true,
	"arch_test": true,
}

// repoRoot caches the resolved repository root directory.
var (
	repoRootOnce sync.Once
	repoRootPath string
)

// repoRoot returns the absolute path to the repository root by walking up
// from this test file's directory until go.mod is found.
func repoRoot(t *testing.T) string {
	t.Helper()
	repoRootOnce.Do(func() {
		// Start from the directory containing this source file.
		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			t.Fatal("runtime.Caller failed")
		}
		dir := filepath.Dir(thisFile)
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				repoRootPath = dir
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				t.Fatal("could not find go.mod in any parent directory")
			}
			dir = parent
		}
	})
	if repoRootPath == "" {
		t.Fatal("repoRoot not resolved")
	}
	return repoRootPath
}

// internalDir returns the absolute path to the internal/ directory.
func internalDirPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "internal")
}

// exportedSymbol describes an exported declaration in a Go file.
type exportedSymbol struct {
	Name    string
	Kind    string // "type", "func", "method", "var", "const"
	DocText string // raw doc comment text, empty if missing
}

// interfaceDecl describes an interface type declaration.
type interfaceDecl struct {
	Name    string
	Pkg     string
	File    string
	Methods []string
}

// internalPackages returns the list of Go package names under internal/,
// excluding dead code (board) and the arch_test package itself.
func internalPackages(t *testing.T) []string {
	t.Helper()

	dir := internalDirPath(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	var pkgs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if excludedPkgs[name] {
			continue
		}
		// Only include directories that contain at least one .go file.
		goFiles := goFilesIn(t, filepath.Join(dir, name))
		if len(goFiles) > 0 {
			pkgs = append(pkgs, name)
		}
	}
	sort.Strings(pkgs)
	return pkgs
}

// goFilesIn returns all non-test .go files in the given directory.
func goFilesIn(t *testing.T, dir string) []string {
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
		name := e.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	sort.Strings(files)
	return files
}

// importsOf parses all non-test Go files in pkgDir and returns deduplicated
// internal import names (e.g. "agent", "fabric"). Only imports matching the
// module's internal/ prefix are included.
func importsOf(t *testing.T, pkgDir string) []string {
	t.Helper()

	files := goFilesIn(t, pkgDir)
	seen := make(map[string]bool)

	fset := token.NewFileSet()
	for _, f := range files {
		node, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing imports in %s: %v", f, err)
		}
		for _, imp := range node.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(path, internalPfx) {
				// Extract the first path component after internal/.
				rel := strings.TrimPrefix(path, internalPfx)
				if idx := strings.Index(rel, "/"); idx != -1 {
					rel = rel[:idx]
				}
				seen[rel] = true
			}
		}
	}

	var result []string
	for pkg := range seen {
		result = append(result, pkg)
	}
	sort.Strings(result)
	return result
}

// lineCount returns the number of lines in the file at filePath.
func lineCount(t *testing.T, filePath string) int {
	t.Helper()

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading %s: %v", filePath, err)
	}
	if len(data) == 0 {
		return 0
	}
	// Count newlines. A file that doesn't end with a newline still counts
	// its last line.
	count := strings.Count(string(data), "\n")
	if data[len(data)-1] != '\n' {
		count++
	}
	return count
}

// exportedSymbols parses a Go file and returns all exported declarations
// (types, functions, methods, vars, consts) with their doc comments.
func exportedSymbols(t *testing.T, filePath string) []exportedSymbol {
	t.Helper()

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing %s: %v", filePath, err)
	}

	var syms []exportedSymbol

	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if !s.Name.IsExported() {
						continue
					}
					doc := docText(d.Doc, s.Doc)
					syms = append(syms, exportedSymbol{
						Name:    s.Name.Name,
						Kind:    "type",
						DocText: doc,
					})
				case *ast.ValueSpec:
					kind := "var"
					if d.Tok == token.CONST {
						kind = "const"
					}
					for _, name := range s.Names {
						if !name.IsExported() {
							continue
						}
						doc := docText(d.Doc, s.Doc)
						syms = append(syms, exportedSymbol{
							Name:    name.Name,
							Kind:    kind,
							DocText: doc,
						})
					}
				}
			}
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			kind := "func"
			name := d.Name.Name
			if d.Recv != nil {
				kind = "method"
			}
			doc := ""
			if d.Doc != nil {
				doc = d.Doc.Text()
			}
			syms = append(syms, exportedSymbol{
				Name:    name,
				Kind:    kind,
				DocText: doc,
			})
		}
	}
	return syms
}

// docText returns the text of the first non-nil doc comment group.
func docText(groups ...*ast.CommentGroup) string {
	for _, g := range groups {
		if g != nil {
			return g.Text()
		}
	}
	return ""
}

// interfaceDecls parses a Go file and returns all interface type declarations.
func interfaceDecls(t *testing.T, filePath string) []interfaceDecl {
	t.Helper()

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing %s: %v", filePath, err)
	}

	pkgName := node.Name.Name

	var decls []interfaceDecl

	for _, d := range node.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			iface, ok := ts.Type.(*ast.InterfaceType)
			if !ok {
				continue
			}

			var methods []string
			if iface.Methods != nil {
				for _, m := range iface.Methods.List {
					for _, name := range m.Names {
						methods = append(methods, name.Name)
					}
				}
			}

			decls = append(decls, interfaceDecl{
				Name:    ts.Name.Name,
				Pkg:     pkgName,
				File:    filePath,
				Methods: methods,
			})
		}
	}
	return decls
}

// --- Sanity tests for the helpers themselves ---

func TestInternalPackages(t *testing.T) {
	t.Parallel()

	pkgs := internalPackages(t)

	if len(pkgs) < 10 {
		t.Errorf("expected at least 10 internal packages, got %d: %v", len(pkgs), pkgs)
	}

	for _, excluded := range []string{"board", "arch_test"} {
		for _, p := range pkgs {
			if p == excluded {
				t.Errorf("internalPackages should exclude %q, but it was present", excluded)
			}
		}
	}

	// Spot-check that well-known packages are present.
	known := map[string]bool{"agent": false, "claude": false, "config": false, "fabric": false, "loop": false}
	for _, p := range pkgs {
		if _, ok := known[p]; ok {
			known[p] = true
		}
	}
	for pkg, found := range known {
		if !found {
			t.Errorf("expected package %q in internalPackages result", pkg)
		}
	}
}

func TestGoFilesIn(t *testing.T) {
	t.Parallel()

	dir := internalDirPath(t)
	files := goFilesIn(t, filepath.Join(dir, "agent"))
	if len(files) == 0 {
		t.Fatal("expected at least one .go file in internal/agent")
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			t.Errorf("goFilesIn should not include test files, got %s", f)
		}
	}
}

func TestImportsOf(t *testing.T) {
	t.Parallel()

	dir := internalDirPath(t)
	imports := importsOf(t, filepath.Join(dir, "claude"))

	found := false
	for _, imp := range imports {
		if imp == "agent" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected internal/claude to import 'agent', got %v", imports)
	}
}

func TestLineCount(t *testing.T) {
	t.Parallel()

	dir := internalDirPath(t)
	// Count lines of the helpers file itself — must be > 0.
	count := lineCount(t, filepath.Join(dir, "arch_test", "helpers_test.go"))
	if count < 50 {
		t.Errorf("expected helpers_test.go to have at least 50 lines, got %d", count)
	}
}

func TestExportedSymbols(t *testing.T) {
	t.Parallel()

	dir := internalDirPath(t)
	// internal/agent should have exported types.
	files := goFilesIn(t, filepath.Join(dir, "agent"))
	if len(files) == 0 {
		t.Fatal("no .go files in internal/agent")
	}

	var total int
	for _, f := range files {
		syms := exportedSymbols(t, f)
		total += len(syms)
	}
	if total == 0 {
		t.Error("expected at least one exported symbol in internal/agent")
	}
}

func TestInterfaceDecls(t *testing.T) {
	t.Parallel()

	dir := internalDirPath(t)
	// internal/fabric/fabric.go should declare the Fabric interface.
	decls := interfaceDecls(t, filepath.Join(dir, "fabric", "fabric.go"))

	found := false
	for _, d := range decls {
		if d.Name == "Fabric" {
			found = true
			if len(d.Methods) == 0 {
				t.Error("Fabric interface should have methods")
			}
			break
		}
	}
	if !found {
		t.Error("expected to find Fabric interface in internal/fabric/fabric.go")
	}
}
