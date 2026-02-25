package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docExemptions lists exported symbols that intentionally lack GoDoc comments.
// Each key is a package name under internal/, each value is a list of symbol
// names that are exempt from the GoDoc requirement. Keep this list as small as
// possible — every entry should have a justifying comment.
var docExemptions = map[string][]string{
	// Long multi-line string constants are self-documenting by name.
	"agent": {"DefaultCoderSystemPrompt", "DefaultReviewerSystemPrompt"},
	// CLIResponse is a simple JSON-mapping struct; its fields are tagged.
	// Invoke and Validate implement the agent.Invoker interface.
	"claude": {"CLIResponse", "Invoke", "Validate"},
	// Typed enum constants whose parent type is documented; values are
	// self-documenting by name.
	"nebula": {
		"NewBranchManager",
		"PhaseStatusPending", "PhaseStatusCreated", "PhaseStatusInProgress",
		"PhaseStatusDone", "PhaseStatusFailed", "PhaseStatusSkipped",
		"ActionCreate", "ActionUpdate", "ActionSkip", "ActionClose", "ActionRetry",
	},
	// Typed iota constants whose parent type is documented; values are
	// self-documenting by name.
	"tui": {
		"ModeLoop", "ModeNebula",
		"PhaseWaiting", "PhaseWorking", "PhaseDone",
		"PhaseFailed", "PhaseGate", "PhaseSkipped",
	},
}

// TestExportedSymbolsHaveGoDoc verifies that every exported type, function,
// method, var, and const in internal packages has a GoDoc comment starting
// with the symbol name, following Go conventions.
func TestExportedSymbolsHaveGoDoc(t *testing.T) {
	t.Parallel()

	pkgs := internalPackages(t)
	for _, pkg := range pkgs {
		t.Run(pkg, func(t *testing.T) {
			t.Parallel()

			pkgDir := filepath.Join(internalDirPath(t), pkg)
			files := goFilesIn(t, pkgDir)

			exemptions := make(map[string]bool)
			for _, sym := range docExemptions[pkg] {
				exemptions[sym] = true
			}

			for _, file := range files {
				if isGeneratedFile(t, file) {
					continue
				}
				checkFileGoDoc(t, file, exemptions)
			}
		})
	}
}

// isGeneratedFile reports whether the file starts with a "Code generated"
// header, indicating it should be excluded from GoDoc checks.
func isGeneratedFile(t *testing.T, filePath string) bool {
	t.Helper()

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading %s: %v", filePath, err)
	}
	// Go generated-file convention: first or second line contains
	// "Code generated" and "DO NOT EDIT".
	head := string(data)
	if len(head) > 500 {
		head = head[:500]
	}
	return strings.Contains(head, "Code generated")
}

// checkFileGoDoc parses a single Go file and reports exported symbols that
// lack proper GoDoc comments. The exemptions map contains symbol names to skip.
func checkFileGoDoc(t *testing.T, filePath string, exemptions map[string]bool) {
	t.Helper()

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parsing %s: %v", filePath, err)
	}

	relPath := relativeFilePath(filePath)

	for _, decl := range node.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			checkGenDecl(t, fset, d, relPath, exemptions)
		case *ast.FuncDecl:
			checkFuncDecl(t, fset, d, relPath, exemptions)
		}
	}
}

// checkGenDecl checks type, var, and const declarations for GoDoc comments.
func checkGenDecl(t *testing.T, fset *token.FileSet, d *ast.GenDecl, relPath string, exemptions map[string]bool) {
	t.Helper()

	isGrouped := len(d.Specs) > 1
	hasBlockDoc := d.Doc != nil && strings.TrimSpace(d.Doc.Text()) != ""

	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			if !s.Name.IsExported() {
				continue
			}
			if exemptions[s.Name.Name] {
				continue
			}
			doc := docText(s.Doc, d.Doc)
			if !hasValidGoDoc(doc, s.Name.Name) {
				pos := fset.Position(s.Pos())
				t.Errorf("%s:%d: exported type %s has no GoDoc comment",
					relPath, pos.Line, s.Name.Name)
			}

		case *ast.ValueSpec:
			for _, name := range s.Names {
				if !name.IsExported() {
					continue
				}
				if exemptions[name.Name] {
					continue
				}

				// For grouped const/var blocks, accept any of:
				// 1. Individual doc comment starting with the name
				// 2. Block-level doc comment on the group
				// 3. Individual inline comment (common for iota enums)
				if isGrouped {
					hasIndividualDoc := hasValidGoDoc(docText(s.Doc), name.Name)
					hasInlineComment := s.Comment != nil && strings.TrimSpace(s.Comment.Text()) != ""
					if hasIndividualDoc || hasBlockDoc || hasInlineComment {
						continue
					}
				} else {
					// Standalone declaration: need proper GoDoc.
					doc := docText(s.Doc, d.Doc)
					if hasValidGoDoc(doc, name.Name) {
						continue
					}
				}

				kind := "var"
				if d.Tok == token.CONST {
					kind = "const"
				}
				pos := fset.Position(name.Pos())
				t.Errorf("%s:%d: exported %s %s has no GoDoc comment",
					relPath, pos.Line, kind, name.Name)
			}
		}
	}
}

// checkFuncDecl checks function and method declarations for GoDoc comments.
func checkFuncDecl(t *testing.T, fset *token.FileSet, d *ast.FuncDecl, relPath string, exemptions map[string]bool) {
	t.Helper()

	if !d.Name.IsExported() {
		return
	}

	// Skip methods on unexported receiver types — they are not part of the
	// public API even though the method name is exported.
	if d.Recv != nil && !isExportedReceiver(d.Recv) {
		return
	}

	if exemptions[d.Name.Name] {
		return
	}

	doc := ""
	if d.Doc != nil {
		doc = d.Doc.Text()
	}

	kind := "func"
	if d.Recv != nil {
		kind = "method"
	}

	if !hasValidGoDoc(doc, d.Name.Name) {
		pos := fset.Position(d.Pos())
		t.Errorf("%s:%d: exported %s %s has no GoDoc comment",
			relPath, pos.Line, kind, d.Name.Name)
	}
}

// hasValidGoDoc returns true if doc is non-empty and starts with the symbol
// name, following Go convention (e.g. "// FooBar does X.").
func hasValidGoDoc(doc, symbolName string) bool {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return false
	}
	return strings.HasPrefix(doc, symbolName)
}

// isExportedReceiver reports whether the method's receiver type is exported.
// Handles both pointer and value receivers.
func isExportedReceiver(recv *ast.FieldList) bool {
	if recv == nil || len(recv.List) == 0 {
		return false
	}
	return isExportedType(recv.List[0].Type)
}

// isExportedType extracts the base type name from an expression (handling
// pointer indirection) and reports whether it is exported.
func isExportedType(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.IsExported()
	case *ast.StarExpr:
		return isExportedType(t.X)
	case *ast.IndexExpr:
		// Generic type: T[U] — check T.
		return isExportedType(t.X)
	case *ast.IndexListExpr:
		// Generic type: T[U, V] — check T.
		return isExportedType(t.X)
	default:
		return false
	}
}

// relativeFilePath strips the repo root prefix to produce a cleaner path for
// error messages. Falls back to the full path if stripping fails.
func relativeFilePath(fullPath string) string {
	// Find "internal/" in the path and return from there.
	const marker = "internal/"
	if idx := strings.Index(fullPath, marker); idx >= 0 {
		return fullPath[idx:]
	}
	return filepath.Base(fullPath)
}
