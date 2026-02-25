package arch_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// allowedColocations maps package names to interface names that are
// legitimately defined in the same package as their implementation.
// Each entry should include a comment explaining why co-location is acceptable.
var allowedColocations = map[string]map[string]bool{
	// Beads defines Client alongside CLI, the canonical beads CLI wrapper.
	// Consumers (loop, nebula, cmd) import the interface type.
	"beads": {
		"Client": true,
	},
	// Strategy pattern: multiple strategy implementations live alongside the interface.
	"dag": {
		"ReportStrategy": true,
	},
	// Fabric defines the core storage interface alongside its SQLite implementation.
	// Consumers (loop, nebula, cmd) import the interface type; the concrete SQLite
	// backend is the canonical implementation. Poller follows the same pattern with
	// LLMPoller and ContractPoller.
	"fabric": {
		"Fabric": true,
		"Poller": true,
	},
	// Filter defines the Filter interface alongside Chain, which composes filters.
	// ClaimChecker is consumed here but implemented externally (fabric).
	"filter": {
		"Filter": true,
	},
	// Loop defines several small internal-use interfaces with their default
	// implementations: Linter/CommandLinter, CycleCommitter/gitCycleCommitter,
	// Hook/HookFunc. TaskCreator and FindingCreator are consumed here and
	// implemented by BeadHook, the default hook wiring beads integration.
	// HailQueue is an internal-use interface with its in-memory default
	// implementation (MemoryHailQueue); consumers don't exist yet.
	"loop": {
		"Linter":         true,
		"CycleCommitter": true,
		"Hook":           true,
		"TaskCreator":    true,
		"FindingCreator": true,
		"HailQueue":      true,
	},
	// Nebula defines gate/committer interfaces alongside their implementations.
	// GitCommitter wraps git operations; Gater/GatePrompter implement the
	// strategy pattern with multiple gate modes.
	"nebula": {
		"GitCommitter": true,
		"Gater":        true,
		"GatePrompter": true,
	},
	// UI defines the UI interface alongside Printer, the sole stderr-based
	// implementation. Consumers import ui.UI for testability.
	"ui": {
		"UI": true,
	},
}

// structMethodsInPkg collects all method names for each receiver type across
// all non-test Go files in pkgDir. Returns a map from type name to method names.
func structMethodsInPkg(t *testing.T, pkgDir string) map[string][]string {
	t.Helper()

	files := goFilesIn(t, pkgDir)
	result := make(map[string][]string)
	fset := token.NewFileSet()

	for _, f := range files {
		node, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", f, err)
		}
		for _, decl := range node.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil {
				continue
			}
			recvType := receiverTypeName(fd.Recv)
			if recvType == "" {
				continue
			}
			result[recvType] = append(result[recvType], fd.Name.Name)
		}
	}
	return result
}

// receiverTypeName extracts the type name from a method receiver field list,
// unwrapping pointer receivers.
func receiverTypeName(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	expr := fl.List[0].Type
	// Unwrap pointer receiver.
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

// implementsAll reports whether structMethods contains all method names
// from ifaceMethods.
func implementsAll(ifaceMethods, structMethods []string) bool {
	set := make(map[string]bool, len(structMethods))
	for _, m := range structMethods {
		set[m] = true
	}
	for _, m := range ifaceMethods {
		if !set[m] {
			return false
		}
	}
	return true
}

// TestInterfacePlacement verifies that interfaces are defined where they are
// consumed, not where they are implemented. It flags any interface that is
// defined in the same package as a struct whose methods satisfy the interface,
// unless the co-location is explicitly allowlisted.
func TestInterfacePlacement(t *testing.T) {
	t.Parallel()

	dir := internalDirPath(t)
	pkgs := internalPackages(t)

	for _, pkg := range pkgs {
		t.Run(pkg, func(t *testing.T) {
			t.Parallel()

			pkgDir := filepath.Join(dir, pkg)
			files := goFilesIn(t, pkgDir)

			// Collect all interface declarations in this package.
			var ifaces []interfaceDecl
			for _, f := range files {
				ifaces = append(ifaces, interfaceDecls(t, f)...)
			}
			if len(ifaces) == 0 {
				return
			}

			// Collect all struct/type methods in this package.
			methods := structMethodsInPkg(t, pkgDir)

			for _, iface := range ifaces {
				// Skip empty/marker interfaces — they have no methods to check.
				if len(iface.Methods) == 0 {
					continue
				}

				// Check allowlist.
				if allowed, ok := allowedColocations[pkg]; ok && allowed[iface.Name] {
					continue
				}

				// Check if any type in the same package implements all
				// interface methods (name match heuristic).
				for typeName, typeMethods := range methods {
					if implementsAll(iface.Methods, typeMethods) {
						t.Errorf(
							"interface %s defined in %s but struct %s in same package implements it; move interface to consumer",
							iface.Name, pkg, typeName,
						)
					}
				}
			}
		})
	}
}
